(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate);
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  try {
    const stage = sessionStorage.getItem('empty-component-stage');
    if (stage === 'saved') {
      await wait(() => document.querySelector('.post-body'));
      assert(document.querySelector('.post-body').textContent.includes('Save after removing the empty component.'), 'Removing the empty component must allow the post to save.');
      assert(!document.querySelector('.post-body .audio-component'), 'An empty component must never be rendered or saved.');
      sessionStorage.removeItem('empty-component-stage');
      await report('PASS');
      return;
    }
    await wait(() => document.querySelector('#post-form .CodeMirror') && document.querySelector('[name="audio"]').hidden);
    const form = document.querySelector('#post-form');
    if (!stage) {
      form.elements.title.value = 'Empty component review';
      form.elements.slug.value = 'empty-component-review';
      form.noValidate = true; // Also exercise server validation when native checks are bypassed.
      for (const [kind, field] of [['photo-album', 'album'], ['file-downloads', 'downloads'], ['pdf-preview', 'pdf'], ['link-preview', 'link'], ['video', 'video'], ['audio', 'audio'], ['x-post', 'xPost'], ['image-comparison', 'comparison']]) {
        form.querySelector(`[data-component-add="${kind}"]`).click();
        const section = form.querySelector(`[data-post-component="${kind}"]`);
        const required = form.querySelector('.CodeMirror').CodeMirror.getInputField().getAttribute('aria-required');
        assert(required !== 'true', 'The empty component must get its own error rather than requiring unrelated Markdown.');
        form.requestSubmit();
        await wait(() => section.querySelector('[data-component-error]:not([hidden])')?.textContent.includes('remove'));
        assert(form.elements.namedItem(field).getAttribute('aria-invalid') === 'true' && section.contains(document.activeElement), 'The error must identify and focus the empty component: ' + kind);
        assert(!section.hidden && form.elements.componentOrder.value === JSON.stringify([kind]), 'A rejected save must retain the added component: ' + kind);
        assert(document.documentElement.scrollWidth <= innerWidth + 1, 'Component errors must fit the viewport.');
        section.querySelector('[data-component-remove]').click();
        assert(section.hidden && !section.querySelector('[data-component-error]:not([hidden])'), 'Removing the component must clear its inline error.');
        assert(!form.querySelector('#editor-errors').textContent.trim(), 'Removing the component must clear its summary error.');
      }
      const summary = form.querySelector('#editor-errors');
      summary.innerHTML = '<p data-error-fields="title">Keep the title error.</p><p data-error-fields="audio">Clear the audio error.</p>';
      form.elements.title.setAttribute('aria-invalid', 'true');
      form.elements.audio.dispatchEvent(new Event('input', {bubbles: true}));
      assert(summary.textContent === 'Keep the title error.', 'Correcting audio must preserve unrelated errors.');
      form.elements.title.dispatchEvent(new Event('input', {bubbles: true}));
      form.querySelector('[data-component-add="audio"]').click();
      sessionStorage.setItem('empty-component-stage', 'ordinary');
      form.removeAttribute('hx-post');
      form.setAttribute('hx-boost', 'false');
      htmx.process(form, true);
      form.requestSubmit();
      return;
    }
    const section = form.querySelector('[data-audio-editor]');
    await wait(() => section.querySelector('[data-component-error]:not([hidden])'));
    assert(!section.hidden && form.elements.componentOrder.value === '["audio"]' && !form.elements.audio.value, 'An ordinary rejected save must preserve the empty audio component.');
    assert(section.querySelector('[data-component-error]').textContent === 'Choose an MP3 or remove the audio component.', 'The inline error must be concise and omit unrelated help text.');
    section.querySelector('[data-component-remove]').click();
    form.querySelector('.CodeMirror').CodeMirror.setValue('Save after removing the empty component.');
    form.querySelector('[name="published"][value="on"]').checked = true;
    sessionStorage.setItem('empty-component-stage', 'saved');
    form.requestSubmit();
  } catch (error) { await report(error.stack || String(error)); }
})();
