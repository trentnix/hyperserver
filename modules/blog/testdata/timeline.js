(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const set = (field, value) => { field.value = value; field.dispatchEvent(new Event('input', {bubbles: true})); };
  const field = (row, name) => row.querySelector(`[data-timeline-field="${name}"]`);
  const detail = 'Published the **first stable release**.\n\nSee the [projects page](/page/projects).';
  const palettes = {
    '': ['rgb(244, 246, 249)', 'rgb(82, 103, 125)', 'rgb(89, 120, 149)', 'rgb(171, 190, 209)'],
    white: ['rgb(255, 255, 255)', 'rgb(82, 103, 125)', 'rgb(89, 120, 149)', 'rgb(171, 190, 209)'],
    parchment: ['rgb(255, 251, 240)', 'rgb(116, 82, 46)', 'rgb(161, 108, 50)', 'rgb(206, 181, 141)'],
    forest: ['rgb(240, 245, 240)', 'rgb(63, 101, 79)', 'rgb(80, 118, 92)', 'rgb(166, 188, 174)'],
    brick: ['rgb(252, 240, 238)', 'rgb(139, 48, 44)', 'rgb(177, 73, 64)', 'rgb(214, 163, 158)'],
  };
  const usesPalette = (timeline, style) => {
    const item = timeline.querySelector('.timeline-milestones > li');
    const colors = [getComputedStyle(timeline).backgroundColor, getComputedStyle(item.querySelector('time')).color,
      getComputedStyle(item, '::before').backgroundColor, getComputedStyle(item, '::after').backgroundColor];
    return colors.every((color, index) => color === palettes[style][index]);
  };
  const isBorderless = element => {
    const style = getComputedStyle(element);
    return style.boxShadow === 'none' && ['Top', 'Right', 'Bottom', 'Left'].every(side => style[`border${side}Width`] === '0px');
  };
  try {
    if (location.pathname === '/page/timeline-review') {
      const timeline = document.querySelector('.post-body .post-timeline');
      const items = [...timeline.querySelectorAll('.timeline-milestones > li')];
      const dates = items.map(item => item.querySelector('time'));
      assert(timeline.querySelector('h3').textContent === 'Project milestones' &&
        dates.map(date => date.dateTime).join(',') === '2023,2024-05,2025-10-18' &&
        dates.map(date => date.textContent).join('|') === '2023|May 2024|Oct 18, 2025',
        'The reader must show chronological milestones with the authored date precision.');
      assert(items[2].querySelector('.timeline-detail strong').textContent === 'first stable release',
        'The detail must render as Markdown.');
      assert(timeline.dataset.timelineStyle === 'white' && usesPalette(timeline, 'white'),
        'The saved palette must color the published background, dates, dots, and rail.');
      assert(isBorderless(timeline), 'The published Plain timeline must be borderless.');
      const bounds = timeline.getBoundingClientRect();
      assert(bounds.left >= 0 && bounds.right <= innerWidth + 1 && getComputedStyle(items[0], '::after').width === '1px' &&
        getComputedStyle(items[2], '::after').content === 'none', 'The rail must fit the viewport and stop at the final milestone.');

      const edit = document.querySelector('.action-edit');
      await htmx.ajax('GET', edit.getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
      const form = document.querySelector('#page-form');
      await waitForComponent(() => componentRowIn(form, 'timeline'), 'saved Timeline');
      componentRowIn(form, 'timeline').querySelector('[data-component-edit]').click();
      const panel = componentEditor(form, 'timeline');
      const [first, second] = panel.querySelectorAll('.timeline-editor-list > li');
      assert(panel.querySelector('[data-timeline-style-picker]').value === 'white' &&
        usesPalette(panel.querySelector('.timeline-style-preview'), 'white'), 'Reopening must restore the saved palette and preview.');
      assert(field(first, 'date').value === '2025-10-18' && field(first, 'detail').value === detail &&
        first.querySelector('.CodeMirror')?.CodeMirror.getValue() === detail && field(second, 'date').value === '2023' &&
        !second.querySelector('[data-timeline-add-detail]').hidden && panel.querySelector('[data-component-unsaved]').hidden,
        'Reopening must preserve editor order, show saved detail immediately, and keep empty detail compact.');
      await report('PASS');
      return;
    }

    const form = document.querySelector('#page-form');
    await waitForComponent(() => form.querySelector('.CodeMirror'), 'page editor');
    assert(form.submit === HTMLFormElement.prototype.submit, 'Markdown editors must leave the native form submission method in place.');
    set(form.elements.title, 'Timeline review');
    set(form.elements.slug, 'timeline-review');
    const panel = await addTestComponent(form.querySelector('[data-component-add="timeline"]'));
    const dialog = panel.closest('dialog');
    const rows = () => [...panel.querySelectorAll('.timeline-editor-list > li')];
    const first = rows()[0];
    const date = field(first, 'date');
    assert(document.activeElement === date, 'A new Timeline must focus its first date.');
    const style = panel.querySelector('[data-timeline-style-picker]');
    const preview = panel.querySelector('.timeline-style-preview');
    assert(style.value === '' && usesPalette(preview, ''), 'Slate must be the default for new timelines.');
    const heading = panel.querySelector('[data-timeline-heading]');
    const headingBounds = heading.getBoundingClientRect();
    const styleBounds = style.getBoundingClientRect();
    const previewBounds = preview.getBoundingClientRect();
    assert(previewBounds.left >= styleBounds.right + 12 && previewBounds.right <= innerWidth &&
      previewBounds.height === styleBounds.height, 'The preview must fit beside the dropdown and match its height.');
    assert(innerWidth <= 640 ? styleBounds.top > headingBounds.bottom :
      styleBounds.left > headingBounds.right && Math.abs(styleBounds.top - headingBounds.top) <= 1,
      'Heading and Color must share a row on wide screens and stack on narrow screens.');
    for (const color of ['parchment', 'forest', 'brick', 'white']) {
      set(style, color);
      assert(usesPalette(preview, color) && preview.getBoundingClientRect().height === previewBounds.height,
        'Changing the palette must update the preview without resizing it.');
    }
    assert(style.selectedOptions[0].label === 'Plain' && isBorderless(preview), 'The Plain option must preview a borderless timeline.');
    set(heading, 'Project milestones');
    set(date, '2025-02-29');
    set(field(first, 'title'), 'Version 1.0');
    await closeComponentSettings(form);
    assert(dialog.open && date.getAttribute('aria-invalid') === 'true' && document.activeElement === date,
      'Done must keep the dialog open and focus an invalid calendar date.');
    set(date, '2025-10-18');
    const addDetail = first.querySelector('[data-timeline-add-detail]');
    addDetail.click();
    const cm = first.querySelector('.CodeMirror').CodeMirror;
    const input = cm.getInputField();
    const label = first.querySelector('[data-timeline-label="detail"]');
    assert(addDetail.hidden && document.activeElement === input && input.getAttribute('aria-labelledby') === label.id,
      'Add detail must open, label, and focus the compact Markdown editor.');
    assert(['Ctrl-P', 'Cmd-P', 'F9', 'F11'].every(key => !(key in cm.getOption('extraKeys'))),
      'The compact editor must disable preview and full-screen shortcuts.');
    const escape = new KeyboardEvent('keydown', {key: 'Escape', keyCode: 27, bubbles: true, cancelable: true});
    input.dispatchEvent(escape);
    assert(!escape.defaultPrevented, 'Escape must reach the settings dialog from the Markdown editor.');
    assert(!first.querySelector('[data-component-picker]') && form.querySelector('.editor-toolbar [data-component-picker]'),
      'The milestone editor must preserve the Content toolbar.');
    cm.setValue(detail.replaceAll('**', ''));
    cm.setSelection({line: 0, ch: 14}, {line: 0, ch: 34});
    first.querySelector('.tool-bold').click();
    assert(cm.getValue() === detail && JSON.parse(form.elements.timeline.value).milestones[0].detail === detail,
      'Formatting through the toolbar must sync the milestone Markdown.');
    cm.setValue('x'.repeat(2001));
    const limit = first.querySelector('.compact-markdown-editor [role="status"]');
    assert(cm.getValue() === detail && limit?.textContent.includes('2,000'),
      'The compact editor must explain rejected changes that exceed the milestone detail limit.');
    cm.setValue('An example line of Markdown.\n'.repeat(35));
    assert(!limit.textContent, 'A successful edit must clear the character limit feedback.');
    cm.scrollTo(null, 10000);
    assert(cm.getScrollInfo().top > 0, 'Longer details must scroll inside the compact editor.');
    cm.setValue(detail);
    const milestoneBounds = first.getBoundingClientRect();
    const topInset = date.labels[0].getBoundingClientRect().top - milestoneBounds.top;
    const bottomInset = milestoneBounds.bottom - first.querySelector('.EasyMDEContainer').getBoundingClientRect().bottom;
    assert(Math.abs(bottomInset - topInset - 5) < 1, 'The bottom inset must account for the space above the visible label text.');
    for (const [when, title] of [['2023', 'First prototype'], ['2024-05', 'Public beta']]) {
      panel.querySelector('[data-timeline-add]').click();
      const row = rows().at(-1);
      assert(document.activeElement === field(row, 'date'), 'Adding a milestone must focus its date.');
      const previous = row.previousElementSibling;
      const card = getComputedStyle(row);
      assert(row.getBoundingClientRect().top - previous.getBoundingClientRect().bottom >= 10 &&
        card.borderTopWidth === '1px' && card.borderBottomWidth === '1px' &&
        card.borderTopColor !== getComputedStyle(previous).borderTopColor,
        'Milestones must have their own separated cards, with a distinct border for the focused card.');
      set(field(row, 'date'), when);
      set(field(row, 'title'), title);
      const button = row.querySelector('[data-timeline-remove]');
      const box = button.getBoundingClientRect();
      assert(box.width >= 44 && box.height >= 44 && box.left >= 0 && box.right <= innerWidth + 1,
        'The remove control must remain tappable at every width.');
      if (when === '2024-05') {
        row.querySelector('[data-timeline-add-detail]').click();
        const secondEditor = row.querySelector('.CodeMirror').CodeMirror;
        assert(secondEditor.getInputField().id !== input.id, 'Each detail editor must have a distinct input ID.');
        secondEditor.setValue('Opened for feedback.');
        assert(cm.getValue() === detail, 'Each editor must keep its own Markdown.');
      }
    }
    assert(rows()[0] === first, 'Date entry must preserve row order and input focus.');
    await closeComponentSettings(form);
    await waitForComponent(() => componentRowIn(form, 'timeline') && !dialog.open, 'Timeline component row');
    assert(!panel.querySelector('.CodeMirror'), 'Closing settings must release the detail editors.');
    assert(form.submit === HTMLFormElement.prototype.submit, 'Closing several editors must preserve native form submission.');
    const source = form.elements.timeline;
    const saved = source.value;
    componentRowIn(form, 'timeline').querySelector('[data-component-edit]').click();
    set(style, 'forest');
    rows()[0].querySelector('[data-timeline-remove]').click();
    assert(JSON.parse(source.value).milestones.length === 2 && document.activeElement === field(rows()[0], 'date'),
      'Removing a milestone must focus the neighboring row.');
    dialog.querySelector('[data-component-cancel]').click();
    componentRowIn(form, 'timeline').querySelector('[data-component-edit]').click();
    assert(source.value === saved && rows().length === 3 && rows()[0].querySelector('.CodeMirror')?.CodeMirror.getValue() === detail,
      'Cancel must restore the saved rows and visible detail.');
    assert(style.value === 'white' && usesPalette(preview, 'white'), 'Cancel must restore the previous color and preview.');
    await closeComponentSettings(form);
    await waitForComponent(() => !dialog.open, 'settings closed');
    form.querySelector('[name="published"][value="on"]').checked = true;
    form.requestSubmit();
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
