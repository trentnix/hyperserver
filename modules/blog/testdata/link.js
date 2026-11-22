(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const wait = async predicate => {
    const end = Date.now() + 12000;
    while (!predicate()) { if (Date.now() > end) throw new Error('Timed out: ' + predicate); await new Promise(resolve => setTimeout(resolve, 25)); }
  };
  const set = (input, value) => { input.value = value; input.dispatchEvent(new Event('input', {bubbles: true})); };
  try {
    const stage = sessionStorage.getItem('link-stage');
    if (!stage) {
      await wait(() => document.querySelector('[data-link-editor] [name="link"]').hidden && document.querySelector('#post-form .CodeMirror'));
      const form = document.querySelector('#post-form');
      const section = form.querySelector('[data-link-editor]');
      assert(section.hidden && section.querySelector('fieldset').disabled, 'An unused link component must not block post validation.');
      form.elements.title.value = 'Link preview review'; form.elements.slug.value = 'link-preview-review';
      form.querySelector('[data-component-picker] summary').click();
      await addTestComponent(form.querySelector('[data-component-add="link-preview"]'));
      assert(!section.hidden && document.activeElement.id === 'link-url' && !section.querySelector('fieldset').disabled, 'Adding a link must focus its URL and enable its fields.');
      set(section.querySelector('#link-url'), 'http://example.test/missing');
      section.querySelector('[data-link-fetch]').click();
      await wait(() => !form.dataset.uploading && section.querySelector('[data-link-status]').textContent.includes('yourself'));
      assert(section.querySelector('#link-url').value === 'http://example.test/missing', 'Failed fetches must preserve manual editing.');
      set(section.querySelector('#link-url'), 'HTTP://example.test/article');
      section.querySelector('[data-link-fetch]').click();
      await wait(() => !form.dataset.uploading && section.querySelector('#link-title').value === 'An interesting article');
      assert(JSON.parse(form.elements.link.value).imageID && section.querySelector('.link-preview-card img'), 'Metadata must import a local thumbnail and show a card.');
      assert(!form.elements.content.required, 'Link-only posts must be allowed.');
      const title = 'A thoughtfully chosen title that wraps cleanly across several lines on a narrow phone screen';
      set(section.querySelector('#link-title'), title);
      section.querySelector('[data-link-fetch]').click();
      await wait(() => document.querySelector('[data-confirm-cancel]'));
      document.querySelector('[data-confirm-cancel]').click();
      assert(section.querySelector('#link-title').value === title, 'Canceling refresh must preserve the author title.');
      section.querySelector('[data-link-fetch]').click();
      document.querySelector('[data-confirm-accept]').click();
      await wait(() => !form.dataset.uploading && section.querySelector('#link-title').value === 'An interesting article');
      set(section.querySelector('#link-title'), title);
      set(section.querySelector('#link-description'), 'A manually edited description.\nA second line remains readable.');
      section.querySelector('input[type="checkbox"]').click();
      assert(JSON.parse(form.elements.link.value).hideImage && !section.querySelector('.link-preview-card img'), 'The image toggle must change the saved setting and preview.');
      const imageID = JSON.parse(form.elements.link.value).imageID;
      const fetchOriginal = window.fetch;
      window.fetch = (path, options) => {
        if (path !== '/post/link-preview') return fetchOriginal(path, options);
        assert(!options.body.has('image'), 'The editor should still request an image before it learns storage is full.');
        return Promise.resolve(new Response(JSON.stringify({
          link: {url: options.body.get('url'), title, description: 'A manually edited description.\nA second line remains readable.'},
          warning: 'Upload storage is full.'
        }), {headers: {'Content-Type': 'application/json', 'Upload-Storage-Full': 'true'}}));
      };
      section.querySelector('[data-link-fetch]').click();
      document.querySelector('[data-confirm-accept]').click();
      await wait(() => !form.dataset.uploading && section.querySelector('[data-link-status]').textContent.includes('existing image was kept'));
      window.fetch = fetchOriginal;
      const refreshed = JSON.parse(form.elements.link.value);
      assert(refreshed.imageID === imageID && refreshed.hideImage && section.querySelector('[data-link-upload]').disabled,
        'A storage warning from a refresh must keep the existing image and its visibility setting.');
      const saved = form.elements.link.value;
      await removeTestComponent(section.querySelector('[data-component-remove]'));
      assert(section.hidden && section.querySelector('fieldset').disabled && !form.elements.link.value, 'Removing a link must clear its source and disable hidden required fields.');
      await undoTestComponent(form.querySelector('[data-component-undo="link-preview"]'));
      assert(form.elements.link.value === saved && section.querySelector('#link-title').value === title && !section.querySelector('input[type="checkbox"]').checked, 'Undo must restore the edited card and image preference.');
      form.querySelector('[data-component-picker] summary').click();
      setTestComponentSource(await addTestComponent(form.querySelector('[data-component-add="pdf-preview"]')), [{id: 'a'.repeat(64) + '.pdf', label: 'Example PDF', size: 200}]);
      await moveTestComponent(section, 'down');
      assert(form.elements.componentOrder.value === '["pdf-preview","link-preview"]', 'Links must use the shared component order.');
      await removeTestComponent(form.querySelector('[data-post-component="pdf-preview"] [data-component-remove]'));
      form.querySelector('[name="published"][value="on"]').checked = true;
      form.dispatchEvent(new Event('input', {bubbles: true}));
      await wait(() => Object.keys(localStorage).some(key => key.startsWith('hyperserver.blog.draft.v1:') && localStorage.getItem(key).includes('thoughtfully')));
      sessionStorage.setItem('link-stage', 'recover'); location.reload(); return;
    }
    if (stage === 'recover') {
      await wait(() => document.querySelector('[data-draft-recovery]') && !document.querySelector('[data-draft-recovery]').hidden);
      document.querySelector('[data-draft-restore]').click();
      const form = document.querySelector('#post-form');
      await wait(() => form.querySelector('#link-title').value.includes('thoughtfully'));
      assert(!form.querySelector('[data-link-editor] input[type="checkbox"]').checked && JSON.parse(form.elements.link.value).imageID, 'Recovery must retain the hidden thumbnail and edited text.');
      sessionStorage.setItem('link-stage', 'view'); form.requestSubmit(); return;
    }
    if (stage === 'view') {
      await wait(() => document.querySelector('.post-body .link-preview-card'));
      const card = document.querySelector('.post-body .link-preview-card');
      assert(card.href === 'http://example.test/article' && card.getAttribute('hx-boost') === 'false', 'Reader links must use the saved destination outside HTMX.');
      assert(card.textContent.includes('thoughtfully') && card.textContent.includes('manually edited') && card.textContent.includes('example.test') && !card.querySelector('img'), 'The saved card must retain edits, show its real domain, and honor the image toggle.');
      assert(document.documentElement.scrollWidth <= innerWidth + 1, 'The card must fit narrow and wide viewports.');
      await htmx.ajax('GET', document.querySelector('a[href^="/post/update/"]').getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
      await wait(() => document.querySelector('#link-title'));
      const form = document.querySelector('#post-form');
      assert(form.querySelector('#link-title').value.includes('thoughtfully') && !form.querySelector('[data-link-editor] input[type="checkbox"]').checked, 'Reopening the editor must preserve the card.');
      form.querySelector('[data-link-editor] input[type="checkbox"]').click();
      assert(form.querySelector('.link-preview-card img').src.includes('/images/'), 'A retained thumbnail must be available when shown again.');
      await report('PASS');
    }
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
