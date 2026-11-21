(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const fill = (field, value) => {
    field.value = value;
    field.dispatchEvent(new Event('input', {bubbles: true}));
  };
  try {
    if (location.pathname === '/page/page-components-review') {
      const body = document.querySelector('.post-body');
      assert(body.querySelector('.post-callout strong')?.textContent === 'worksheet' &&
        body.querySelector('.expandable-details summary')?.textContent === 'Extra steps',
        'The published page must show both components.');
      const edit = document.querySelector('.action-edit');
      await htmx.ajax('GET', edit.getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
      const form = document.querySelector('#page-form');
      await waitForComponent(() => componentRowIn(form, 'callout'), 'saved page component');
      componentRowIn(form, 'callout').querySelector('[data-component-edit]').click();
      const panel = componentEditor(form, 'callout');
      assert(panel.querySelector('[data-markdown-value="body"]').value === 'Read the **worksheet**.' &&
        panel.querySelector('[data-component-unsaved]').hidden, 'Reopening must preserve the saved callout without showing unsaved changes.');
      await report('PASS');
      return;
    }
    const form = document.querySelector('#page-form');
    await waitForComponent(() => form.querySelector('.CodeMirror'), 'page editor');
    fill(form.elements.title, 'Page components review');
    fill(form.elements.slug, 'page-components-review');
    const cm = form.querySelector('.CodeMirror').CodeMirror;
    assert(cm.getInputField().getAttribute('aria-required') !== 'true', 'The page body must remain optional.');
    cm.setValue('Start here.\n\n## Main steps\n\nContinue here.');
    cm.focus();
    cm.setCursor({line: 2, ch: 0});
    const fetchOriginal = window.fetch;
    let resumeLayout;
    window.fetch = (url, options) => {
      if (url !== '/post/component-position') return fetchOriginal(url, options);
      return new Promise(resolve => { resumeLayout = () => resolve(fetchOriginal(url, options)); });
    };
    const adding = addTestComponent(form.querySelector('[data-component-add="callout"]'));
    await waitForComponent(() => resumeLayout, 'pending page component placement');
    let submitted = false;
    const interceptSubmit = event => { submitted = true; event.preventDefault(); };
    form.addEventListener('submit', interceptSubmit);
    form.requestSubmit();
    form.removeEventListener('submit', interceptSubmit);
    const saveStatus = form.querySelector('[data-component-save-status]');
    const showedWait = saveStatus?.textContent.includes('Wait for the component edit');
    window.fetch = fetchOriginal;
    resumeLayout();
    const callout = await adding;
    assert(!submitted && showedWait, 'Save must explain why it waits for a page component.');
    assert(!saveStatus.textContent, 'Save feedback must clear when placement finishes.');
    fill(callout.querySelector('[data-markdown-value="body"]'), 'Read the **worksheet**.');
    closeComponentSettings(form);
    await moveTestComponent(callout, 'down');
    assert(cm.getValue().indexOf('## Main steps') < cm.getValue().indexOf('hyperserver:component callout'),
      'A page component must move between Markdown blocks.');
    const details = await addTestComponent(form.querySelector('[data-component-add="expandable-details"]'));
    fill(details.querySelector('[data-markdown-value="title"]'), 'Extra steps');
    fill(details.querySelector('[data-markdown-value="body"]'), 'Check the **result**.');
    closeComponentSettings(form);
    form.querySelector('.tool-preview').click();
    await waitForComponent(() => form.querySelector('.editor-preview .expandable-details'), 'page component preview');
    assert(form.querySelector('.editor-preview .post-callout'), 'Preview must include both page components.');
    form.querySelector('.tool-preview').click();
    form.querySelector('[name="published"][value="on"]').checked = true;
    form.requestSubmit();
  } catch (error) { await report(error.message + '\n' + (error.stack || '')); }
})();
