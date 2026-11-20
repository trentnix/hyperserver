(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate);
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  try {
    if (location.pathname === '/post/s/component-only-native') {
      assert(document.querySelector('.file-downloads'), 'The native editor must save a component-only post.');
      await report('PASS');
      return;
    }
    const form = document.querySelector('#post-form');
    await wait(() => !form.querySelector('[data-component-picker]').hidden);
    const body = form.elements.content;
    const {getMarkdownEditor} = await import('/assets/js/markdown-editor.js');
    const editor = getMarkdownEditor(body);
    assert(Boolean(editor) === enhanced, 'Exercise the requested editor mode.');
    const check = required => {
      if (enhanced) {
        const input = form.querySelector('.CodeMirror').CodeMirror.getInputField();
        assert(!body.required && body.checkValidity(), 'The enhanced editor must leave its hidden textarea optional.');
        assert((input.getAttribute('aria-required') === 'true') === required, 'The visible editor must reflect the current body requirement.');
      } else {
        assert(body.required === required, 'The native textarea must retain browser validation.');
      }
    };
    check(true);
    form.elements.title.value = 'Requirement review';
    if (enhanced) {
      form.requestSubmit();
      await wait(() => document.querySelector('#editor-errors').textContent.includes('valid Markdown'));
      assert(getMarkdownEditor(body) === editor, 'A blank submission must show the server error without replacing the editor.');
    } else {
      form.requestSubmit();
      await wait(() => body.getAttribute('aria-invalid') === 'true');
      assert(document.activeElement === body, 'Native validation must focus the visible textarea.');
    }
    form.querySelector('[data-component-picker] summary').click();
    await addTestComponent(form.querySelector('[data-component-add="file-downloads"]'));
    const section = form.querySelector('[data-post-component="file-downloads"]');
    check(false);
    form.requestSubmit();
    await wait(() => section.querySelector('[data-component-error]:not([hidden])')?.textContent.includes('remove'));
    assert(!section.hidden, 'An empty added component must remain available for correction or removal.');
    form.elements.downloads.value = JSON.stringify(savedFile);
    form.elements.downloads.dispatchEvent(new Event('change', {bubbles: true}));
    check(false);
    await removeTestComponent(section.querySelector('[data-component-remove]'));
    check(true);
    await undoTestComponent(form.querySelector('[data-component-undo="file-downloads"]'));
    check(false);
    if (enhanced) {
      editor.destroy();
      assert(!body.required && !getMarkdownEditor(body), 'Destroy must restore the latest component-dependent requirement.');
      await removeTestComponent(section.querySelector('[data-component-remove]'));
      assert(body.required, 'Removing the last component after teardown must restore native validation.');
    } else {
      form.elements.slug.value = 'component-only-native';
      form.requestSubmit();
      await wait(() => document.querySelector('.file-downloads'));
      assert(location.pathname === '/post/s/component-only-native', 'The native editor must save a component-only post.');
    }
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + (error.stack || '')); }
})();
