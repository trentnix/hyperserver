// Only content with publication history needs confirmation when its URL changes.
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
  const save = async (path, fields) => {
    const parts = path.split('/');
    if (parts.length === 3) {
      const editor = new DOMParser().parseFromString(await (await fetch('/' + parts[1] + '/update/' + parts[2])).text(), 'text/html');
      fields.version = editor.querySelector('[name="version"]').value;
    }
    const response = await fetch(path, {method: 'POST', headers: {'HX-Request': 'true'}, body: new URLSearchParams(fields)});
    assert(response.ok, 'Unable to prepare content: ' + response.status);
    return response.headers.get('HX-Redirect');
  };
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    if (!sessionStorage.getItem('slug-warnings-login')) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('slug-warnings-login', 'true');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    let prompts = [];
    let consent = true;
    window.confirm = message => { prompts.push(message); return consent; };
    for (const kind of ['post', 'pages']) {
      for (const state of ['new', 'draft', 'published', 'unpublished']) {
        const original = kind + '-' + state;
        const fields = {title: 'Original title', slug: original, content: 'Original body', childrenDisplay: 'hidden'};
        let path = '/' + kind + '/create';
        if (state !== 'new') {
          if (state !== 'draft') fields.published = 'on';
          const url = await save('/' + kind, fields);
          const page = new DOMParser().parseFromString(await (await fetch(url)).text(), 'text/html');
          path = page.querySelector('a[href^="/' + kind + '/update/"]').getAttribute('href');
          if (state === 'unpublished') {
            delete fields.published;
            await save(path.replace('/update/', '/'), fields);
          }
        }
        await htmx.ajax('GET', path, '#dialog-host');
        await wait(() => document.querySelector('dialog[open] .CodeMirror') && document.querySelector('[data-draft-status]:not([hidden])'));
        await wait(() => !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
        const form = document.querySelector('dialog[open] form');
        form.elements.title.value = 'Changed title';
        form.querySelector('.CodeMirror').CodeMirror.setValue('Writing that must not be lost.');
        const slug = form.elements.slug;
        const expected = state === 'published' || state === 'unpublished';
        assert(slug.hasAttribute('data-published-slug') === expected, 'Warning metadata differs from publication history: ' + kind + '/' + state);

        // Stop after the document's warning handler, before HTMX or native saving.
        // Canceling the warning must prevent this listener from running at all.
        let submissions = 0;
        form.addEventListener('submit', event => {
          submissions++;
          event.preventDefault();
          event.stopImmediatePropagation();
        }, true);
        for (const value of [state === 'new' ? '' : original, 'renamed', '']) {
          slug.value = value;
          const shouldWarn = expected && value !== original;
          prompts = [];
          consent = false;
          const before = submissions;
          form.requestSubmit();
          assert(prompts.length === (shouldWarn ? 1 : 0), 'Incorrect confirmation count for ' + kind + '/' + state + '/' + value);
          assert(submissions === before + (shouldWarn ? 0 : 1), 'Canceled warning allowed submission.');
          assert(form.isConnected && slug.value === value && form.elements.content.value === 'Writing that must not be lost.', 'Confirmation discarded unsaved fields.');
          if (shouldWarn) {
            assert(prompts[0].includes('Changing this URL'), 'Confirmation does not explain the URL change.');
            consent = true;
            form.requestSubmit();
            assert(submissions === before + 1 && prompts.length === 2, 'Accepting the warning did not allow submission.');
          }
        }
        consent = true;
        form.querySelector('[data-close-dialog]').click();
      }
    }
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
