(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const set = (field, value) => { field.value = value; field.dispatchEvent(new Event('input', {bubbles: true})); };
  const longLabel = 'Open the GitHub project for the full example, setup guide, and source code';
  try {
    if (location.pathname === '/page/action-link-review') {
      const link = document.querySelector('.post-body .post-action-link > a');
      const bounds = link.getBoundingClientRect();
      assert(link.textContent.trim() === longLabel && link.target === '_blank', 'The published button must use the saved label and open a full URL in a new tab.');
      assert(bounds.height >= 44 && bounds.left >= 0 && bounds.right <= innerWidth && link.scrollWidth <= link.clientWidth + 1,
        'The button must stay tappable and its long label must fit a narrow viewport.');
      assert(getComputedStyle(link).backgroundColor === 'rgb(52, 61, 75)', 'The button must match the blog’s primary action style.');
      link.focus();
      assert(document.activeElement === link, 'The button must accept keyboard focus as a link.');
      const edit = document.querySelector('.action-edit');
      await htmx.ajax('GET', edit.getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
      const form = document.querySelector('#page-form');
      await waitForComponent(() => componentRowIn(form, 'action-link'), 'saved Action link');
      componentRowIn(form, 'action-link').querySelector('[data-component-edit]').click();
      const panel = componentEditor(form, 'action-link');
      assert(panel.querySelector('[data-action-link-url]').value === 'https://github.com/trentnix/hyperserver' &&
        panel.querySelector('[data-action-link-label]').value === longLabel &&
        panel.querySelector('[data-component-unsaved]').hidden, 'Reopening must restore both saved fields.');
      await report('PASS');
      return;
    }

    const form = document.querySelector('#page-form');
    await waitForComponent(() => form.querySelector('.CodeMirror'), 'page editor');
    set(form.elements.title, 'Action link review');
    set(form.elements.slug, 'action-link-review');
    const picker = form.querySelector('[data-component-add="action-link"]');
    const panel = await addTestComponent(picker);
    const label = panel.querySelector('[data-action-link-label]');
    const url = panel.querySelector('[data-action-link-url]');
    assert(document.activeElement === label && !label.validity.valid && !url.validity.valid, 'Focus the label and require both fields.');
    set(label, '   ');
    set(url, '   ');
    await closeComponentSettings(form);
    await waitForComponent(() => panel.closest('dialog').open && document.activeElement === label, 'invalid Action link fields');
    assert(label.validationMessage === 'Enter a button label.' && url.validationMessage === 'Enter a destination URL.' &&
      label.getAttribute('aria-invalid') === 'true' && url.getAttribute('aria-invalid') === 'true',
      'Done must show field errors for whitespace-only values.');
    set(label, longLabel);
    assert(!label.hasAttribute('aria-invalid') && !document.getElementById(label.id + '-error') && url.hasAttribute('aria-invalid'),
      'Correcting the label must clear its error and retain the destination error.');
    set(url, 'example.test/demo');
    await closeComponentSettings(form);
    assert(panel.closest('dialog').open && document.activeElement === url && !componentRowIn(form, 'action-link') &&
      document.getElementById(url.id + '-error')?.textContent.includes('https://') &&
      url.value === 'example.test/demo' && label.value === longLabel,
      'Done must keep the dialog open, preserve both fields, and explain the missing URL scheme.');
    set(url, '/page/projects');
    assert(!url.hasAttribute('aria-invalid') && !document.getElementById(url.id + '-error'),
      'Correcting the destination with a site path must clear its error.');
    await closeComponentSettings(form);
    await waitForComponent(() => componentRowIn(form, 'action-link'), 'Action link row');
    componentRowIn(form, 'action-link').querySelector('[data-component-edit]').click();
    set(url, 'https://');
    await closeComponentSettings(form);
    assert(panel.closest('dialog').open && document.activeElement === url, 'Done must also validate an edited destination.');
    panel.closest('dialog').querySelector('[data-component-cancel]').click();
    componentRowIn(form, 'action-link').querySelector('[data-component-edit]').click();
    assert(url.value === '/page/projects' && !url.hasAttribute('aria-invalid'), 'Cancel must restore the valid destination and clear the error.');
    set(url, 'https://github.com/trentnix/hyperserver');
    await closeComponentSettings(form);
    form.querySelector('[name="published"][value="on"]').checked = true;
    form.requestSubmit();
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
