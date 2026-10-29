// Exercise real selection controls without uploading a second copy or losing edits.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate);
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  window.addEventListener('error', event => report(event.message));
  const library = () => document.querySelector('.media-browser[open]');
  async function open(button) {
    button.focus();
    button.click();
    await wait(() => library()?.querySelector('[data-media-select]'));
    assert(library().getBoundingClientRect().right <= innerWidth, 'Library overflows viewport.');
    assert(library().scrollWidth <= library().clientWidth, 'Library content overflows horizontally.');
    const bounds = library().getBoundingClientRect();
    assert(bounds.top >= innerHeight * .09 && bounds.bottom <= innerHeight * .91, 'Chooser must leave space above and below its distinct frame.');
    const heading = library().querySelector('header');
    const headingTop = heading.getBoundingClientRect().top;
    const results = library().querySelector('[data-media-results]');
    results.scrollTop = results.scrollHeight;
    assert(heading.getBoundingClientRect().top === headingTop && library().scrollTop === 0, 'Scrolling images must not move the heading or Cancel button.');
    results.scrollTop = 0;
  }
  async function choose(button) {
    await open(button);
    const select = library().querySelector('[data-media-select]');
    const id = select.dataset.mediaSelect;
    select.click();
    await wait(() => !document.querySelector('.media-browser'));
    return id;
  }
  try {
    const stage = sessionStorage.getItem('library-stage');
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('library-stage', 'logged-in');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'logged-in') {
      sessionStorage.setItem('library-stage', 'post');
      location.assign('/post/create');
      return;
    }
    if (stage === 'post' || stage === 'page') {
      const form = document.querySelector(stage === 'post' ? '#post-form' : '#page-form');
      await wait(() => form.querySelector('.CodeMirror'));
      for (const [selector, filename, label] of [['.tool-image', 'image-up.svg', 'Upload image'], ['.tool-library', 'images.svg', 'Choose existing image']]) {
        const button = form.querySelector(selector);
        const icon = getComputedStyle(button, '::before');
        assert(button.title === label && icon.maskImage.includes('/lib/lucide/' + filename), 'Image toolbar must use the matching SVG and descriptive tooltip.');
        assert(icon.width === '16px' && icon.height === '16px', 'Image icons must have consistent dimensions.');
        assert(new DOMMatrix(icon.transform).m42 === -1, 'SVG icons must align optically with the text-based toolbar icons.');
        const image = new Image();
        image.src = '/assets/lib/lucide/' + filename;
        await image.decode();
        assert(image.naturalWidth === 24, 'Vendored toolbar icon must load as an SVG.');
      }
      form.elements.title.value = 'Unsaved ' + stage;
      form.elements.title.dispatchEvent(new Event('input', {bubbles: true}));
      const editor = form.querySelector('.CodeMirror').CodeMirror;
      editor.setValue('Keep this writing.\n\n');
      editor.setCursor({line: 2, ch: 0});
      const browse = form.querySelector('[data-media-browse]');
      await open(browse);
      library().querySelector('[data-close-dialog]').click();
      await wait(() => !document.querySelector('.media-browser'));
      assert(form.elements.previewImage.value === '', 'Cancel changed the selection.');
      assert(document.activeElement === browse, 'Cancel did not restore focus.');
      await open(browse);
      library().querySelector('[data-media-page]').click();
      await wait(() => library()?.textContent.includes('Page 2 of 2') && library().querySelector('[data-media-select]'));
      assert(library().querySelectorAll('[data-media-select]').length === 1, 'Pagination repeated the first page.');
      const choice = library().querySelector('[data-media-select]');
      const id = choice.dataset.mediaSelect;
      choice.click();
      await wait(() => !library());
      assert(form.elements.previewImage.value === id, 'Preview selection stored the wrong ID.');
      assert(form.querySelector('[data-image-preview]').getAttribute('src') === '/images/' + id, 'Preview did not update.');
      assert(form.elements.title.value === 'Unsaved ' + stage && editor.getValue() === 'Keep this writing.\n\n', 'Selection lost unsaved writing.');
      const bodyImage = await choose(form.querySelector('.tool-library'));
      assert(editor.getValue().includes('![](/images/' + bodyImage + ')'), 'Existing image was not inserted into Markdown.');
      assert(form.elements.content.value === editor.getValue(), 'Markdown textarea was not synchronized.');
      if (stage === 'post') {
        // The same library must stack above an HTMX editor without closing it.
        await htmx.ajax('GET', '/pages/create', {target: '#dialog-host'});
        const parent = document.querySelector('#dialog-host dialog[open]');
        assert(parent, 'Editor dialog did not open.');
        await choose(parent.querySelector('[data-media-browse]'));
        assert(parent.open && parent.querySelector('[data-image-value]').value, 'Choosing closed or failed to update the editor dialog.');
        parent.close();
        parent.remove();
      }
      sessionStorage.setItem('library-stage', stage === 'post' ? 'page' : 'profile');
      location.assign(stage === 'post' ? '/pages/create' : '/profile');
      return;
    }
    if (stage === 'upload') {
      const form = document.querySelector('[data-media-upload]');
      assert(form && !form.hidden, 'Media page must offer direct uploads.');
      assert(document.querySelector('a[href="/media/unused"]').classList.contains('page-create'), 'Cleanup must use the management button style.');
      const input = form.elements.image;
      const chooseFile = file => { const transfer = new DataTransfer(); transfer.items.add(file); input.files = transfer.files; };
      chooseFile(new File(['not an image'], 'invalid.png', {type: 'image/png'}));
      form.requestSubmit();
      await wait(() => form.querySelector('[data-upload-status]').textContent.includes('valid PNG'));
      assert(!input.disabled && !form.querySelector('button').disabled, 'Upload failure must allow retry.');
      const canvas = document.createElement('canvas');
      canvas.width = canvas.height = 12;
      const context = canvas.getContext('2d');
      context.fillStyle = '#ce7835'; context.fillRect(0, 0, 12, 12);
      chooseFile(new File([await new Promise(resolve => canvas.toBlob(resolve))], 'library.png', {type: 'image/png'}));
      sessionStorage.setItem('library-stage', 'uploaded');
      form.requestSubmit();
      return;
    }
    if (stage === 'uploaded') {
      assert(location.pathname.startsWith('/media/') && document.querySelector('h1').textContent === 'Image details', 'Upload must open its details.');
      assert(document.body.textContent.includes('Not used by saved posts'), 'New upload should show no saved uses.');
      const image = document.querySelector('.revision-image img');
      await wait(() => image.complete);
      assert(image.naturalWidth === 12, 'Direct upload did not produce the expected image.');
      const listed = new DOMParser().parseFromString(await (await fetch('/media')).text(), 'text/html');
      assert(listed.querySelector('.media-grid a').getAttribute('href') === location.pathname, 'Newest upload should link to its details.');
      await report('PASS');
      return;
    }
    const form = document.querySelector('#profile-form');
    for (const field of ['imageURL', 'faviconURL', 'backgroundURL']) {
      const picker = form.elements[field].closest('[data-image-picker]');
      const id = await choose(picker.querySelector('[data-media-browse]'));
      assert(form.elements[field].value === '/images/' + id, 'Profile selection must store the URL.');
      assert(picker.querySelector('[data-image-preview]').getAttribute('src') === '/images/' + id, 'Profile preview did not update.');
    }
    // A selected file can disappear between listing and choosing. Do not replace the old selection.
    const picker = form.elements.imageURL.closest('[data-image-picker]');
    const previous = form.elements.imageURL.value;
    const originalFetch = window.fetch;
    window.fetch = (path, options) => path === '/media?pick=1' ? Promise.resolve(new Response('', {status: 500})) : originalFetch(path, options);
    picker.querySelector('[data-media-browse]').click();
    await wait(() => library()?.querySelector('[data-media-status]').textContent.includes('Unable to load'));
    assert(form.elements.imageURL.value === previous, 'Failed library load changed the selection.');
    library().querySelector('[data-close-dialog]').click();
    await wait(() => !document.querySelector('.media-browser'));
    window.fetch = originalFetch;

    await open(picker.querySelector('[data-media-browse]'));
    window.fetch = (path, options) => options?.method === 'HEAD' ? Promise.resolve(new Response('', {status: 500})) : originalFetch(path, options);
    library().querySelector('[data-media-select]').click();
    await wait(() => library()?.querySelector('[data-media-status]').textContent.includes('Unable to check'));
    assert(form.elements.imageURL.value === previous, 'Failed image check changed the selection.');
    library().querySelector('[data-close-dialog]').click();
    await wait(() => !document.querySelector('.media-browser'));
    window.fetch = originalFetch;

    await open(picker.querySelector('[data-media-browse]'));
    const choice = library().querySelector('[data-media-select]');
    const confirmation = await fetch('/media/delete/' + choice.dataset.mediaSelect);
    const doc = new DOMParser().parseFromString(await confirmation.text(), 'text/html');
    const version = doc.querySelector('[name=version]').value;
    const deletion = await fetch('/media/delete/' + choice.dataset.mediaSelect, {method: 'POST', body: new URLSearchParams({version})});
    assert(deletion.ok, 'Fixture deletion failed.');
    choice.click();
    await wait(() => library()?.querySelector('[data-media-status]').textContent.includes('no longer available'));
    assert(form.elements.imageURL.value === previous, 'Failed selection overwrote the previous value.');
    library().querySelector('[data-close-dialog]').click();
    await wait(() => !library());
    const listed = await fetch('/media?pick=1');
    const remaining = new DOMParser().parseFromString(await listed.text(), 'text/html');
    assert(remaining.querySelectorAll('[data-media-select]').length === 24 && remaining.body.textContent.includes('Page 1 of 1'), 'Selection uploaded additional copies.');
    sessionStorage.setItem('library-stage', 'upload');
    location.assign('/media');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
