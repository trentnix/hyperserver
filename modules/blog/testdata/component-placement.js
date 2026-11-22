(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  window.addEventListener('error', event => report(event.message + '\n' + event.error?.stack));
  window.addEventListener('unhandledrejection', event => report(event.reason?.stack || String(event.reason)));
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const wait = async condition => {
    const timeout = new Error('Timed out: ' + condition);
    const deadline = Date.now() + 12000;
    while (!condition()) {
      if (Date.now() > deadline) throw timeout;
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  const set = (field, value) => {
    field.value = value;
    field.dispatchEvent(new Event('input', {bubbles: true}));
  };
  const marker = '<!-- hyperserver:component embed -->';
  const initial = 'Introduction.\n\n- First item\n- Second item\n\nConclusion.';
  const checkOrder = (root, selector) => {
    const heading = root.querySelector('p');
    const embed = root.querySelector(selector);
    const list = root.querySelector('ul');
    assert(heading.textContent === 'Introduction.' && embed && list &&
      heading.compareDocumentPosition(embed) & Node.DOCUMENT_POSITION_FOLLOWING, 'The embed must follow the introduction.');
    assert(embed.compareDocumentPosition(list) & Node.DOCUMENT_POSITION_FOLLOWING, 'The embed must precede the list.');
  };
  try {
    if (sessionStorage.getItem('placement-saved')) {
      checkOrder(document.querySelector('.post-body'), '.web-embed');
      const edit = document.querySelector('a[href^="/post/update/"]');
      await htmx.ajax('GET', edit.getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
      await wait(() => document.querySelector('.CodeMirror [data-component-edit=embed]'));
      assert(document.querySelector('#post-content').value.indexOf(marker) < document.querySelector('#post-content').value.indexOf('- First item'), 'Reopening the post must restore its inline component.');
      const settings = document.querySelector('[data-embed-editor]');
      assert(!settings.closest('dialog').open, 'A placed component must reopen with its settings closed.');
      document.querySelector('[data-component-edit=embed]').click();
      assert(document.querySelector('#embed-title').value === 'Interactive chart' && settings.closest('dialog').matches(':modal'), 'Edit must open the saved settings in a modal.');
      await report('PASS');
      return;
    }

    await wait(() => document.querySelector('#post-form .CodeMirror') && !document.querySelector('[data-component-picker]').hidden);
    const form = document.querySelector('#post-form');
    const pageWidth = form.closest('.article-column').getBoundingClientRect().width;
    const cm = form.querySelector('.CodeMirror').CodeMirror;
    const widget = () => form.querySelector('.CodeMirror .component-marker');
    const picker = form.querySelector('[data-component-picker]');
    const moveUp = () => widget()?.querySelector('[data-component-shift=up]:not(:disabled)');
    const moveDown = () => widget()?.querySelector('[data-component-shift=down]:not(:disabled)');
    assert(picker.closest('.editor-toolbar'), 'Add component must belong to the Content toolbar.');
    set(form.elements.title, 'Placement review');
    set(form.elements.slug, 'placement-review');
    cm.setValue(initial);
    const tool = form.querySelector('.tool-preview');
    tool.click();
    await wait(() => form.querySelector('.editor-preview-active')?.textContent.includes('Introduction.'));
    await addTestComponent(picker.querySelector('[data-component-add=embed]'));
    const settings = form.querySelector('[data-embed-editor]');
    const dialog = settings.closest('dialog');
    const done = dialog.querySelector('[data-component-done]');
    const cancel = dialog.querySelector('[data-component-cancel]');
    assert(!tool.classList.contains('active') && cm.getValue() === initial, 'Opening settings from Preview must return to Content without placing the component.');
    const bounds = dialog.getBoundingClientRect();
    const footer = done.getBoundingClientRect();
    const body = dialog.querySelector('.component-dialog-body');
    const cancelBounds = cancel.getBoundingClientRect();
    assert(dialog.matches(':modal') && bounds.left >= 0 && bounds.right <= document.documentElement.clientWidth &&
      cancelBounds.left >= bounds.left && cancelBounds.right < footer.left && footer.right <= bounds.right &&
      footer.bottom <= innerHeight, 'Cancel and Done must fit in the dialog.');
    if (innerWidth < 280) {
      assert(body.scrollWidth >= pageWidth - 26 && body.scrollWidth > body.clientWidth, 'The form must retain the editor minimum width inside the dialog.');
      body.scrollLeft = body.scrollWidth;
      assert(body.scrollLeft > 0 && done.getBoundingClientRect().right === footer.right, 'Scrolling across the form must keep Done in view.');
      await report('PASS');
      return;
    }
    assert(body.scrollWidth === body.clientWidth, 'The dialog must fit its fields at the supported page width.');
    set(form.querySelector('#embed-title'), 'Discard this addition');
    cancel.click();
    assert(!dialog.open && document.activeElement === picker.querySelector('summary') &&
      cm.getValue() === initial && form.elements.componentOrder.value === '[]' && !form.elements.embed.value,
      'Canceling a new component must restore the original Markdown, order, and focus.');
    await addTestComponent(picker.querySelector('[data-component-add=embed]'));
    set(form.querySelector('#embed-url'), 'https://widgets.example.test/demo');
    set(form.querySelector('#embed-title'), 'Interactive chart');
    await closeComponentSettings(form);
    assert(cm.getValue().trimEnd().endsWith(marker), 'Done must place a component at the end when no cursor was set.');
    await removeTestComponent(settings.querySelector('[data-component-remove]'));
    cm.undo();
    cm.undo();

    cm.setValue(initial);
    cm.focus();
    cm.setCursor({line: 3, ch: 4});
    await addTestComponent(picker.querySelector('[data-component-add=embed]'));
    assert(document.activeElement === form.querySelector('#embed-url'), 'Adding must open the component settings.');
    assert(settings.matches('.component-panel-active'), 'Adding after an undone addition must reuse the inactive settings panel.');
    await closeComponentSettings(form);
    assert(dialog.open && document.activeElement === form.querySelector('#embed-url'), 'Done must focus the invalid field in the open dialog.');
    set(form.querySelector('#embed-url'), 'https://widgets.example.test/demo');
    set(form.querySelector('#embed-title'), 'Interactive chart');
    await closeComponentSettings(form);
    await wait(widget);
    assert(cm.getValue() === initial.replace('Conclusion.', marker + '\nConclusion.'), 'Insertion inside a list must place the component after the full list.');
    assert(!dialog.open && document.activeElement === widget().querySelector('[data-component-edit]'), 'Done must return focus to the inline component.');
    widget().querySelector('[data-component-edit]').click();
    assert(document.activeElement === form.querySelector('#embed-url'), 'Edit must focus the existing settings.');
    set(form.querySelector('#embed-title'), 'Discard this edit');
    dialog.dispatchEvent(new Event('cancel', {cancelable: true}));
    assert(!dialog.open && form.querySelector('#embed-title').value === 'Interactive chart' &&
      document.activeElement === widget().querySelector('[data-component-edit]'),
      'Escape must discard edits from the current dialog and retain earlier edits in the post.');
    widget().querySelector('[data-component-place]').click();
    await wait(() => moveUp() && moveDown());
    assert(!form.querySelector('dialog[open]') && !widget().querySelector('.component-move-steps').hidden, 'Move must reveal controls on the component row.');
    widget().querySelector('[data-component-place]').click();
    assert(widget().querySelector('.component-move-steps').hidden && cm.getValue().indexOf(marker) > cm.getValue().indexOf('- Second item'), 'Closing the move controls must keep the component in place.');

    const original = cm.getValue();
    const pointerType = innerWidth <= 360 ? 'touch' : 'mouse';
    const pointer = (target, type, x, y) => target.dispatchEvent(new PointerEvent(type, {
      pointerType, pointerId: 21, button: 0, clientX: x, clientY: y, bubbles: true, cancelable: true
    }));
    const handle = () => widget().querySelector('[data-component-drag]');
    const startDrag = (line = 2) => {
      handle().scrollIntoView({block: 'center'});
      const box = handle().getBoundingClientRect();
      pointer(handle(), 'pointerdown', box.left + box.width / 2, box.top + box.height / 2);
      const x = cm.getWrapperElement().getBoundingClientRect().left + 16;
      const y = cm.heightAtLine(line, 'window');
      pointer(document, 'pointermove', x, y);
      return {x, y};
    };
    assert(getComputedStyle(handle()).touchAction === 'none' && handle().getBoundingClientRect().height >= 44, 'The move handle must work with touch without blocking scrolling elsewhere.');
    let point = startDrag();
    const cue = () => document.querySelector('.component-drop-cue:not([hidden])');
    pointer(handle(), 'lostpointercapture', point.x, point.y);
    await wait(cue);
    assert(cm.getValue() === original && Math.abs(cue().getBoundingClientRect().top - point.y) < 2, 'The drop cue must mark a block boundary before changing the document.');
    pointer(document, 'pointercancel', point.x, point.y);
    assert(!cue() && cm.getValue() === original, 'Canceling a drag must clear the cue and keep the original position.');

    point = startDrag();
    await wait(cue);
    pointer(document, 'pointerup', point.x, point.y);
    await wait(() => !document.querySelector('.component-drop-cue') && cm.getValue().indexOf(marker) < cm.getValue().indexOf('- First item'));
    await wait(() => widget() && document.activeElement === handle());
    cm.undo();
    assert(cm.getValue() === original && form.elements.componentOrder.value === '["embed"]',
      'Undo must move the component back without changing the writing or active component.');

    // The local example has a blank line on each side of its first component.
    const spaced = 'Introduction.\n\n' + marker + '\n\n## Next section\n\nConclusion.';
    cm.setValue(spaced);
    await wait(() => form.elements.content.value === spaced);
    await wait(widget);
    point = startDrag(4);
    await wait(cue);
    pointer(document, 'pointerup', point.x, point.y);
    await wait(() => cm.getValue() === spaced.replace(marker + '\n\n##', '\n' + marker + '\n##'));
    await wait(widget);
    point = startDrag(2);
    await wait(cue);
    assert(Math.abs(cue().getBoundingClientRect().top - point.y) < 2, 'The original gap must remain a drop target after a short downward move.');
    pointer(document, 'pointerup', point.x, point.y);
    await wait(() => cm.getValue() === spaced);

    const longPost = original + '\n\n' + Array.from({length: 70}, (_, index) => 'Paragraph ' + index + '.').join('\n\n');
    cm.setValue(longPost);
    await wait(widget);
    startDrag();
    const editorBox = cm.getWrapperElement().getBoundingClientRect();
    point = {x: editorBox.left + 16, y: Math.min(innerHeight, editorBox.bottom) - 4};
    pointer(document, 'pointermove', point.x, point.y);
    const beforeScroll = scrollY + cm.getScrollInfo().top;
    await wait(() => scrollY + cm.getScrollInfo().top > beforeScroll + 20);
    pointer(document, 'pointercancel', point.x, point.y);
    assert(cm.getValue() === longPost && !document.querySelector('.component-drop-cue'), 'Scrolling during a canceled drag must leave the writing unchanged.');
    cm.setValue(original);
    await wait(widget);
    form.dispatchEvent(new CustomEvent('blog:editor-baseline', {detail: Object.fromEntries(new FormData(form))}));
    await moveTestComponent(settings, 'up');
    assert(cm.getValue().indexOf(marker) < cm.getValue().indexOf('- First item'), 'Move up must place the component before the list.');
    await wait(() => !settings.querySelector('[data-component-unsaved]').hidden);
    await moveTestComponent(settings, 'down');
    assert(cm.getValue() === original, 'Move down must return the component to its original place.');
    await wait(() => settings.querySelector('[data-component-unsaved]').hidden);
    await moveTestComponent(settings, 'up');
    const moved = cm.getValue();
    assert(moved.replace(marker + '\n', '') === initial, 'Moving a component must preserve the writing and its spacing.');
    widget().querySelector('[data-component-edit]').click();
    await removeTestComponent(form.querySelector('[data-embed-editor] [data-component-remove]'));
    assert(!cm.getValue().includes(marker), 'Removal must clear its marker.');
    cm.undo();
    await wait(() => cm.getValue() === moved && form.querySelector('#embed-title').value === 'Interactive chart');
    cm.redo();
    await wait(() => !cm.getValue().includes(marker) && !form.elements.embed.value);
    await undoTestComponent(form.querySelector('[data-component-undo=embed]'));
    assert(cm.getValue() === moved && form.querySelector('#embed-title').value === 'Interactive chart', 'Undo must restore the original position and settings.');

    const commentExamples = '<!-- Opening comment\n' + marker + '\n\n<div>\n' + marker + '\n</div>\n\n```md\n' + marker + '\n```\n\n';
    await closeComponentSettings(form);
    cm.setValue(commentExamples + moved);
    await wait(widget);
    widget().querySelector('[data-component-edit]').click();
    await removeTestComponent(form.querySelector('[data-embed-editor] [data-component-remove]'));
    assert(cm.getValue() === commentExamples + initial, 'Move and Remove must handle placements as blocks, preserving comments in HTML or code examples.');
    await undoTestComponent(form.querySelector('[data-component-undo=embed]'));
    assert(cm.getValue() === commentExamples + moved, 'Undo must restore the real placement outside the examples.');
    await closeComponentSettings(form);
    cm.setValue('<div>\n\n' + marker + '\n\nConclusion.');
    await wait(widget);
    assert(widget().dataset.componentRow === 'embed', 'A marker after a blank HTML block boundary must get a control.');
    cm.setValue(moved);
    await wait(widget);

    const tight = 'First paragraph.\n' + marker + '\nSecond paragraph.';
    cm.setValue(tight);
    await wait(() => widget() && cm.getValue() === tight);
    await moveTestComponent(settings, 'up');
    assert(cm.getValue() === marker + '\nFirst paragraph.\n\nSecond paragraph.',
      'Moving the component must preserve the blank line needed to keep both paragraphs separate.');
    cm.undo();
    assert(cm.getValue() === tight, 'Undoing the move must restore the original Markdown.');
    await removeTestComponent(settings.querySelector('[data-component-remove]'));
    assert(cm.getValue() === 'First paragraph.\n\nSecond paragraph.', 'Removing the component must preserve the paragraph boundary.');
    await undoTestComponent(form.querySelector('[data-component-undo=embed]'));
    assert(cm.getValue() === tight, 'Undo from the removal notice must replace its temporary separator.');
    await closeComponentSettings(form);
    cm.setValue(moved);
    await wait(widget);

    form.querySelector('.tool-preview').click();
    await wait(() => form.querySelector('.editor-preview .web-embed'));
    checkOrder(form.querySelector('.editor-preview'), '.web-embed');
    form.querySelector('.tool-preview').click();
    assert(widget().getBoundingClientRect().width > 0 && document.documentElement.scrollWidth <= Math.max(innerWidth, pageWidth) + 1, 'Inline component actions must fit the editor.');
    form.elements.published.value = 'on';
    sessionStorage.setItem('placement-saved', 'yes');
    form.requestSubmit();
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
