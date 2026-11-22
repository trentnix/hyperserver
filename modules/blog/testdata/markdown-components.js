(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const titles = ['What changed between the measurements?', 'Notes on repeating these measurements on another machine'];
  const bodies = ['First **measurement**.\n\n- Keep the same inputs.', 'Second **measurement**.\n\n```text\n' + 'long-output-'.repeat(24) + '\n```'];
  const callouts = [
    {style: '', body: 'Use the **same browser profile** for both measurements.', background: 'rgb(255, 251, 240)'},
    {style: 'slate', body: 'Keep a copy of your baseline.', background: 'rgb(239, 244, 248)'},
    {style: 'caution', body: 'A refresh clears the current measurement.', background: 'rgb(255, 235, 227)'},
    {style: 'success', body: 'The latest run passed all checks.', background: 'rgb(237, 247, 239)'},
  ];
  const set = (input, value) => { input.value = value; input.dispatchEvent(new Event('input', {bubbles: true})); };
  try {
    if (sessionStorage.getItem('details-published')) {
      const disclosures = [...document.querySelectorAll('.post-body .expandable-details')];
      assert(disclosures.length === 2, 'Both details must render.');
      for (const title of titles) {
        const details = disclosures.find(disclosure => disclosure.querySelector('summary').textContent === title);
        assert(details, 'The saved title must appear in the disclosure: ' + title);
        const summary = details.querySelector('summary');
        const body = details.querySelector('.expandable-details-body');
        const collapsedHeight = details.getBoundingClientRect().height;
        assert(!details.open && collapsedHeight <= summary.getBoundingClientRect().height + 2, 'Details must start collapsed.');
        assert(summary.getBoundingClientRect().height >= 44, 'The disclosure must have room to tap.');
        summary.focus();
        assert(document.activeElement === summary, 'The summary must accept keyboard focus.');
        summary.click();
        assert(details.open && details.getBoundingClientRect().height > collapsedHeight && body.querySelector('strong').textContent === 'measurement', 'The summary must reveal rendered Markdown.');
        assert(disclosures.filter(disclosure => disclosure.open).length === 1, 'Each disclosure must open independently.');
        assert(document.documentElement.scrollWidth <= innerWidth + 1, 'The expanded body must fit the viewport.');
        summary.click();
        assert(!details.open && details.getBoundingClientRect().height === collapsedHeight, 'Selecting the title again must close the details.');
      }
      const note = document.querySelector('.post-body .post-callout[role=note]:not([data-callout-style])');
      assert(note?.querySelector('strong')?.textContent === 'same browser profile' &&
        !note.querySelector('.post-callout-heading') && note.getBoundingClientRect().height > 0,
        'A callout without a heading must show its rendered text inline.');
      const notes = [...document.querySelectorAll('.post-body .post-callout[role=note]')];
      assert(notes.length === callouts.length, 'All four callouts must render.');
      for (const {style, background} of callouts) {
        const styledNote = notes.find(note => (note.dataset.calloutStyle || '') === style);
        assert(styledNote && getComputedStyle(styledNote).backgroundColor === background,
          'The saved palette must render: ' + (style || 'parchment'));
      }
      const edit = document.querySelector('a[href^="/post/update/"]');
      await htmx.ajax('GET', edit.getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
      const form = document.querySelector('#post-form');
      await waitForComponent(() => form.querySelector('.CodeMirror'), 'saved post editor');
      const fields = [...form.querySelectorAll('[data-post-component="expandable-details"] textarea[name]')];
      assert(fields.length === 2 && fields.every(field => {
        const {title, body} = JSON.parse(field.value);
        return body === bodies[titles.indexOf(title)];
      }), 'Reopening must preserve both Markdown bodies.');
      const ids = JSON.parse(sessionStorage.getItem('callout-ids'));
      for (const [index, {style, body, background}] of callouts.entries()) {
        const id = ids[index];
        await waitForComponent(() => componentRowIn(form, id), 'saved callout row');
        componentRowIn(form, id).querySelector('[data-component-edit]').click();
        const panel = componentEditor(form, id);
        const markdown = panel.querySelector('.compact-markdown-editor .CodeMirror').CodeMirror;
        assert(panel.querySelector('select').value === style && markdown.getValue() === body &&
          !panel.querySelector('[data-component-unsaved]:not([hidden])'), 'Reopening must preserve the callout text and style and show it as saved.');
        assert(getComputedStyle(panel.querySelector('.callout-style-preview')).backgroundColor === background,
          'Reopening must show the saved palette in the preview.');
        const title = panel.querySelector('h2');
        const status = panel.querySelector('[data-component-unsaved]');
        const text = panel.querySelector('.compact-markdown-editor');
        const topInPanel = element => element.getBoundingClientRect().top - panel.getBoundingClientRect().top;
        const titleTop = topInPanel(title);
        const textTop = topInPanel(text);
        markdown.setValue(body + ' Edited.');
        assert(!status.hidden && topInPanel(title) === titleTop && topInPanel(text) === textTop,
          'Showing unsaved changes must keep the callout title and fields in place.');
        markdown.setValue(body);
        assert(status.hidden && topInPanel(title) === titleTop && topInPanel(text) === textTop,
          'Clearing unsaved changes must keep the callout title and fields in place.');
        const select = panel.querySelector('select');
        set(select, style === 'slate' ? 'success' : 'slate');
        const pending = body + ' Discard this edit.';
        markdown.focus();
        markdown.setValue(pending);
        // Some input methods can commit their final text when the field loses focus.
        markdown.getInputField().addEventListener('blur', () => markdown.setValue(pending + ' Final input.'), {once: true});
        panel.closest('dialog').querySelector('[data-component-cancel]').click();
        assert(!panel.closest('dialog').open && !panel.querySelector('.compact-markdown-editor') && status.hidden &&
          document.activeElement === componentRowIn(form, id).querySelector('[data-component-edit]'),
          'Cancel must clear dialog edits and return focus to the component.');
        componentRowIn(form, id).querySelector('[data-component-edit]').click();
        assert(panel.querySelector('.CodeMirror').CodeMirror.getValue() === body && select.value === style,
          'Reopening must show the original text and style after Cancel.');
        await closeComponentSettings(form);
      }
      sessionStorage.removeItem('callout-ids');
      sessionStorage.removeItem('details-published');
      await report('PASS');
      return;
    }

    const form = document.querySelector('#post-form');
    await waitForComponent(() => form.querySelector('.CodeMirror'), 'post editor');
    form.elements.title.value = 'Expandable details review';
    form.elements.slug.value = 'expandable-details-review';
    const cm = form.querySelector('.CodeMirror').CodeMirror;
    cm.replaceRange('Redo this paragraph.', {line: 0, ch: 0}, undefined, '+input');
    cm.undo();
    const canceled = await addTestComponent(form.querySelector('[data-component-add="callout"]'));
    canceled.querySelector('.CodeMirror').CodeMirror.setValue('Discard this callout.');
    canceled.closest('dialog').querySelector('[data-component-cancel]').click();
    cm.redo();
    assert(cm.getValue() === 'Redo this paragraph.' && form.elements.componentOrder.value === '[]' && !form.elements.callout.value,
      'Canceling an addition must preserve the previous writing redo and leave no callout.');
    cm.undo();
    const picker = form.querySelector('[data-component-add="expandable-details"]');
    for (const [index, title] of titles.entries()) {
      const panel = await addTestComponent(picker);
      const name = panel.querySelector('input');
      const body = panel.querySelector('fieldset textarea');
      const markdown = panel.querySelector('.compact-markdown-editor .CodeMirror').CodeMirror;
      const input = markdown.getInputField();
      const label = document.getElementById(input.getAttribute('aria-labelledby'));
      assert(document.activeElement === name && label.textContent === 'Details', 'The dialog must focus Title and label the Markdown field.');
      label.click();
      assert(document.activeElement === input, 'Selecting the label must focus the Markdown editor.');
      assert(!panel.querySelector('fieldset').disabled, 'Adding the component must enable its required fields.');
      set(name, title);
      await closeComponentSettings(form);
      const error = document.getElementById(body.id + '-error');
      assert(document.activeElement === input && input.getAttribute('aria-invalid') === 'true' && input.getAttribute('aria-required') === 'true' &&
        input.getAttribute('aria-describedby').split(' ').includes(error.id) && error.previousElementSibling.classList.contains('compact-markdown-editor'),
        'Done must focus the visible Markdown editor and show its validation error below it.');
      markdown.setValue(bodies[index].replaceAll('**', ''));
      markdown.setSelection({line: 0, ch: index === 0 ? 6 : 7}, {line: 0, ch: index === 0 ? 17 : 18});
      panel.querySelector('.tool-bold').click();
      assert(markdown.getValue() === bodies[index] && !input.hasAttribute('aria-invalid') && !error.isConnected,
        'Formatting must apply Markdown and editing must clear validation.');
      await closeComponentSettings(form);
    }
    const calloutPicker = form.querySelector('[data-component-add="callout"]');
    const ids = [];
    for (const {style, body, background} of callouts) {
      const panel = await addTestComponent(calloutPicker);
      const text = panel.querySelector('fieldset textarea');
      const select = panel.querySelector('select');
      assert(panel.querySelector('input').validity.valid && !text.validity.valid,
        'An empty callout must require text and allow an optional heading.');
      assert(select.value === '' && select.selectedOptions[0].textContent === 'Parchment' && select.options.length === callouts.length,
        'Each callout must default to Parchment and offer four choices.');
      set(select, style);
      const preview = panel.querySelector('.callout-style-preview');
      assert(getComputedStyle(preview).backgroundColor === background, 'The preview must update to the selected palette.');
      const selectBox = select.getBoundingClientRect();
      const previewBox = preview.getBoundingClientRect();
      assert(previewBox.left - selectBox.right >= 16 && selectBox.top === previewBox.top && previewBox.right <= innerWidth,
        'The callout preview must fit beside the style selector with room for the focus ring.');
      panel.querySelector('.CodeMirror').CodeMirror.setValue(body);
      ids.push(panel.dataset.componentId || panel.dataset.postComponent);
      await closeComponentSettings(form);
    }
    sessionStorage.setItem('callout-ids', JSON.stringify(ids));
    form.querySelector('[name=published][value=on]').checked = true;
    sessionStorage.setItem('details-published', 'true');
    form.requestSubmit();
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
