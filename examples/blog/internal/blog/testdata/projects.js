// Exercise project editing with the real HTMX, Markdown editor, and image upload.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate.toString());
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    const stage = sessionStorage.getItem('project-stage');
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('project-stage', 'logged-in');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'logged-in') {
      sessionStorage.setItem('project-stage', 'create');
      location.assign('/projects');
      return;
    }
    if (stage === 'create') {
      document.querySelector('a[href^="/pages/create?parent="]').click();
      await wait(() => document.querySelector('dialog[open] .CodeMirror'));
      await wait(() => !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
      const form = document.querySelector('#page-form');
      form.elements.title.value = 'A project';
      form.elements.slug.value = 'browser-project';
      form.elements.summary.value = 'A short card description';
      assert(form.elements.displayOrder.value === '', 'New projects should default to update-time ordering.');
      form.elements.displayOrder.value = '10';
      form.elements.published.checked = true;
      sessionStorage.setItem('project-parent', form.elements.parentID.value);
      const cm = document.querySelector('.CodeMirror').CodeMirror;
      form.elements.slug.value = 'INVALID';
      form.noValidate = true;
      form.requestSubmit();
      await wait(() => document.querySelector('#editor-errors').textContent.includes('slug'));
      form.elements.slug.value = 'browser-project';
      assert(document.querySelector('.CodeMirror').CodeMirror === cm, 'Validation replaced the project editor.');
      cm.setValue('# Project details\n\nThis is the full content.');
      document.querySelector('.tool-preview').click();
      await wait(() => document.querySelector('.editor-preview h1')?.textContent === 'Project details');
      document.querySelector('.tool-preview').click();

      const input = document.querySelector('#page-image');
      const selectFile = file => {
        const files = new DataTransfer();
        files.items.add(file);
        input.files = files.files;
        input.dispatchEvent(new Event('change', {bubbles: true}));
      };
      selectFile(new File(['not an image'], 'bad.png', {type: 'image/png'}));
      await wait(() => !form.dataset.uploading);
      assert(form.elements.previewImage.value === '' && document.querySelector('[data-image-status]').textContent.includes('unchanged'), 'Invalid upload replaced the preview.');

      const canvas = document.createElement('canvas');
      canvas.width = 100; canvas.height = 60;
      canvas.getContext('2d').fillRect(0, 0, 100, 60);
      const image = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
      selectFile(new File([image], 'preview.png', {type: 'image/png'}));
      assert(form.querySelector('[type="submit"]').disabled, 'Save was not disabled during upload.');
      await wait(() => !form.dataset.uploading);
      assert(form.elements.previewImage.value.endsWith('.png'), 'Upload did not select a stored image.');
      sessionStorage.setItem('page-image', form.elements.previewImage.value);
      sessionStorage.setItem('project-stage', 'saved');
      form.requestSubmit();
      return;
    }
    if (stage === 'saved') {
      assert(location.pathname === '/page/browser-project', 'Project save did not reach its detail page.');
      assert(!document.querySelector('#article-content img'), 'Preview image leaked into the detail page.');
      assert(document.querySelector('.article-column').getBoundingClientRect().width <= 960, 'Project detail lost its reading-width limit.');
      const edit = document.querySelector('a[href^="/pages/update/"]').getAttribute('href');
      // A second card exercises both desktop columns and the mobile stack.
      const response = await fetch('/pages', {method: 'POST', body: new URLSearchParams({
        parentID: sessionStorage.getItem('project-parent'), title: 'Second project', slug: 'second-project', summary: 'Default preview', content: 'Second details', displayOrder: '20', published: 'on'
      })});
      assert(response.ok, 'Second project could not be saved.');
      await htmx.ajax('GET', '/projects', '#article-content');
      const cards = [...document.querySelectorAll('.page-card')];
      assert(cards.length === 2, 'Projects listing did not show both cards.');
      const uploaded = cards[0].querySelector('img');
      const fallback = cards[1].querySelector('img');
      await wait(() => uploaded.complete && fallback.complete);
      assert(uploaded.naturalWidth > 0 && fallback.naturalWidth > 0, 'Card image failed to load.');
      assert(uploaded.src.includes(sessionStorage.getItem('page-image')) && fallback.src.includes('project-default.svg'), 'Wrong card images.');
      const [first, second] = cards.map(card => card.getBoundingClientRect());
      assert(innerWidth < 768 ? second.top > first.bottom : Math.abs(first.top - second.top) < 1, 'Wrong responsive card layout.');
      assert(document.documentElement.scrollWidth <= innerWidth, 'Project cards overflow the viewport.');
      assert(Math.abs(uploaded.getBoundingClientRect().width / uploaded.getBoundingClientRect().height - 1.6) < .02, 'Card image aspect ratio is not consistent.');
      if (innerWidth >= 1680) {
        const sidebar = document.querySelector('.site-sidebar').getBoundingClientRect();
        const content = document.querySelector('.article-column').getBoundingClientRect();
        assert(Math.abs(content.left - sidebar.right) < 1, 'Projects must begin beside the sidebar.');
        assert(Math.abs(content.right - document.documentElement.clientWidth) < 1, 'Projects must use the remaining ultrawide width.');
        const page = document.querySelector('.page-blocks').getBoundingClientRect();
        const grid = document.querySelector('.page-grid').getBoundingClientRect();
        assert(Math.abs(grid.width - page.width * .9) < 1, 'Ultrawide cards must use 90% of the available content width.');
        assert(Math.abs(grid.left - page.left) < 1, 'Ultrawide cards must align with the content left edge.');
      }

      const parentID = sessionStorage.getItem('project-parent');
      for (const mode of ['list', 'hidden', 'blocks']) {
        const saved = await fetch('/pages/' + parentID, {method: 'POST', body: new URLSearchParams({
          title: 'Projects', slug: 'projects', displayOrder: '1', published: 'on', childrenDisplay: mode
        })});
        assert(saved.ok, 'Parent preview mode could not be saved.');
        await htmx.ajax('GET', '/projects', '#article-content');
        const previews = [...document.querySelectorAll('.page-card')];
        assert(previews.length === (mode === 'hidden' ? 0 : 2), 'Wrong preview count for ' + mode);
        if (mode === 'list') {
          const [first, second] = previews.map(preview => preview.getBoundingClientRect());
          assert(second.top > first.bottom, 'Preview list must stack its rows at every width.');
          assert(document.querySelector('.article-column').getBoundingClientRect().width <= 960, 'List mode must retain reading width.');
        }
        assert(document.documentElement.scrollWidth <= innerWidth, 'Preview mode overflowed the viewport: ' + mode);
      }

      await htmx.ajax('GET', edit, '#dialog-host');
      await wait(() => document.querySelector('dialog[open] .CodeMirror'));
      await wait(() => !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
      document.querySelector('[data-image-reset]').click();
      const form = document.querySelector('#page-form');
      assert(form.elements.previewImage.value === '', 'Removing the preview retained its ID.');
      form.elements.published.checked = false;
      sessionStorage.setItem('project-stage', 'unpublished');
      form.requestSubmit();
      return;
    }
    assert(stage === 'unpublished', 'Unexpected project test stage.');
    assert(document.querySelector('.page-status')?.textContent.includes('Draft'), 'Unpublishing did not mark the project as a draft.');
    await fetch('/auth/logout', {method: 'POST'});
    assert((await fetch('/page/browser-project')).status === 404, 'Anonymous visitor could read a draft.');
    const listing = await (await fetch('/projects')).text();
    assert(!listing.includes('/page/browser-project'), 'Draft appeared in the public listing.');
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
