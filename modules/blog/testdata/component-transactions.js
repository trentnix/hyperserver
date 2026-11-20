(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const fetchOriginal = window.fetch;
  const report = message => fetchOriginal('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const marker = '<!-- hyperserver:component embed -->';
  let pending;
  function delayLayout(match) {
    window.fetch = (input, options) => {
      if (input !== '/post/component-position' || !match(options.body.get('content'))) return fetchOriginal(input, options);
      return new Promise(resolve => { pending = async () => { resolve(await fetchOriginal(input, options)); }; });
    };
  }
  const resumeLayout = () => {
    window.fetch = fetchOriginal;
    const request = pending();
    pending = null;
    return request;
  };
  const set = (field, value, event = 'input') => {
    field.value = value;
    field.dispatchEvent(new Event(event, {bubbles: true}));
  };
  try {
    const form = document.querySelector('#post-form');
    await waitForComponent(() => form.querySelector('.CodeMirror'), 'Content editor');
    const cm = form.querySelector('.CodeMirror').CodeMirror;
    const section = componentEditor(form, 'embed');
    const picker = form.querySelector('[data-component-picker]');
    const order = () => form.elements.componentOrder.value;
    const row = () => componentRowIn(form, 'embed');
    set(form.elements.title, 'Component transaction review');
    set(form.elements.slug, 'component-transaction-review');

    const html = '<div>\n</div>';
    cm.setValue(html);
    delayLayout(source => source === html);
    picker.querySelector('summary').click();
    picker.querySelector('[data-component-add=embed]').click();
    await waitForComponent(() => pending, 'initial placement request');
    let saveAttempted = false;
    form.addEventListener('htmx:before:request', event => {
      saveAttempted = true;
      event.preventDefault();
    });
    form.requestSubmit();
    assert(!saveAttempted && order() === '[]' && cm.getValue() === html, 'Save must wait until Add has a placement.');
    resumeLayout();
    await waitForComponent(() => section.matches('.component-dialog:modal .component-panel-active'), 'new component settings');
    assert(order() === '[]' && cm.getValue() === html && !new FormData(form).has('embed'),
      'A new component must stay out of the post form until Done.');
    const url = 'https://widgets.example.test/demo';
    set(form.querySelector('#embed-url'), url);
    set(form.querySelector('#embed-title'), 'Chart');
    delayLayout(source => source.includes(marker));
    closeComponentSettings(form);
    await waitForComponent(() => pending, 'layout refresh after Done');
    const placed = new FormData(form);
    assert(placed.get('content') === html + '\n\n' + marker + '\n' && placed.get('componentOrder') === '["embed"]' &&
      JSON.parse(placed.get('embed')).title === 'Chart', 'Done must publish the marker, form fields, and settings together.');
    resumeLayout();
    cm.undo();
    assert(cm.getValue() === html && order() === '[]' && section.hidden && !form.elements.embed.value, 'Undo must deactivate an added component.');
    cm.redo();
    assert(cm.getValue() === placed.get('content') && order() === '["embed"]' && form.querySelector('#embed-url').value === url,
      'Redo must restore the marker and settings entered after Add.');

    const positioned = 'Opening for keyboard move.\n\n' + marker + '\n\nClosing paragraph.';
    cm.setValue(positioned);
    const handle = () => row()?.querySelector('[data-component-place]');
    await waitForComponent(() => handle()?.closest('.CodeMirror'), 'inline Move handle');
    handle().click();
    const up = () => row()?.querySelector('[data-component-shift=up]:not(:disabled)');
    await waitForComponent(up, 'Move up');
    delayLayout(source => source.startsWith(marker));
    up().focus();
    up().click();
    await waitForComponent(() => pending, 'layout refresh after Move up');
    assert(row().contains(document.activeElement), 'Moving must preserve row focus while layout refreshes.');
    document.activeElement.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true}));
    assert(document.activeElement === handle() && handle().getAttribute('aria-expanded') === 'false',
      'Escape must close Move controls and focus the handle before layout refreshes.');
    resumeLayout();

    cm.setValue(positioned);
    await waitForComponent(() => handle()?.closest('.CodeMirror'), 'Move handle before a delayed step');
    handle().click();
    await waitForComponent(up, 'Move up before editing the writing');
    delayLayout(source => source.includes('New writing before moving.'));
    cm.replaceRange('\n\nNew writing before moving.', cm.getCursor('end'));
    const beforeDelayedMove = cm.getValue();
    up().click();
    await waitForComponent(() => pending, 'layout for a pending Move up');
    saveAttempted = false;
    form.requestSubmit();
    const saveStatus = form.querySelector('[data-component-save-status]');
    assert(!saveAttempted && saveStatus.textContent.includes('Wait for the component edit'),
      'Save must wait for a requested move to finish and show why.');
    handle().dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true}));
    assert(!saveStatus.textContent, 'Canceling a pending move must release Save immediately.');
    handle().click();
    await resumeLayout();
    await new Promise(resolve => setTimeout(resolve, 30));
    assert(cm.getValue() === beforeDelayedMove && handle().getAttribute('aria-expanded') === 'true',
      'A delayed Move must expire when its controls close, even when the same controls reopen.');
    delayLayout(source => source.includes('More writing before moving.'));
    cm.replaceRange(' More writing before moving.', {line: cm.lastLine(), ch: cm.getLine(cm.lastLine()).length});
    const beforeCancel = cm.getValue();
    up().click();
    await waitForComponent(() => pending, 'layout for Move canceled by Undo');
    handle().dispatchEvent(new KeyboardEvent('keydown', {key: 'z', ctrlKey: true, bubbles: true, cancelable: true}));
    assert(!form.hasAttribute('data-component-action-pending') && cm.getValue() === beforeCancel,
      'Undo must cancel the pending component move first without undoing an earlier edit.');
    await resumeLayout();
    await new Promise(resolve => setTimeout(resolve, 30));
    assert(cm.getValue() === beforeCancel, 'A move canceled by Undo must not apply after its layout response arrives.');
    handle().click();

    delayLayout(source => source.includes('Edit before removal.'));
    cm.replaceRange('Edit before removal.\n\n', {line: 0, ch: 0});
    row().querySelector('[data-component-edit]').click();
    section.querySelector('[data-component-remove]').click();
    await waitForComponent(() => pending, 'layout for pending removal');
    closeComponentSettings(form);
    row().querySelector('[data-component-edit]').click();
    const title = section.querySelector('#embed-title');
    set(title, 'New title after reopening');
    await resumeLayout();
    await new Promise(resolve => setTimeout(resolve, 30));
    assert(section.matches('.component-panel-active') && title.value === 'New title after reopening',
      'A delayed Remove must expire when its settings dialog closes.');
    closeComponentSettings(form);

    cm.setValue('Before the unfinished block.\n\n```md\nUnfinished example.');
    cm.setCursor({line: 0, ch: 0});
    picker.querySelector('summary').click();
    picker.querySelector('[data-component-add=video]').click();
    await waitForComponent(() => form.querySelector('[data-component-placement-status]').textContent.includes('before adding a component'), 'guidance for the unfinished fence');
    assert(order() === '["embed"]' && !form.querySelector('dialog[open]'), 'Add must wait for the unfinished fence even when the cursor precedes it.');

    const writing = 'Opening for legacy drag.\n\nClosing paragraph.';
    cm.setValue(writing);
    cm.replaceRange(' More writing.', cm.getCursor('end'), undefined, '+input');
    const legacy = cm.getValue();
    set(form.elements.video, '{"url":"https://youtu.be/M7lc1UVf-VE","title":"Video"}', 'change');
    await waitForComponent(() => row()?.matches('.component-end-row'), 'legacy component row');
    delayLayout(source => source === legacy);
    const pointer = (target, type, x, y) => target.dispatchEvent(new PointerEvent(type, {
      pointerType: 'touch', pointerId: 21, button: 0, clientX: x, clientY: y, bubbles: true, cancelable: true
    }));
    handle().scrollIntoView({block: 'center'});
    const box = handle().getBoundingClientRect();
    const x = cm.getWrapperElement().getBoundingClientRect().left + 16;
    const y = cm.heightAtLine(2, 'window');
    pointer(handle(), 'pointerdown', box.left + box.width / 2, box.top + box.height / 2);
    pointer(document, 'pointermove', x, y);
    await waitForComponent(() => pending, 'layout for legacy touch drag');
    assert(!document.querySelector('.component-drop-cue:not([hidden])'), 'A pending layout cannot show a drop cue.');
    pointer(document, 'pointerup', x, y);
    assert(!document.querySelector('.component-drop-cue'), 'Releasing a drag without a cue must cancel it immediately.');
    resumeLayout();
    await moveTestComponent(section, 'down');
    assert(cm.getValue() === legacy && order() === '["video","embed"]', 'The canceled drag must leave both components below the writing.');
    cm.undo();
    assert(cm.getValue() === legacy && order() === '["embed","video"]', 'Undo must restore the legacy order and keep the latest prose.');
    cm.redo();
    assert(cm.getValue() === legacy && order() === '["video","embed"]', 'Redo must restore the new legacy order.');
    cm.undo();
    cm.undo();
    assert(cm.getValue() === writing && order() === '["embed","video"]', 'The next Undo must still reach the latest prose edit.');

    const third = componentEditor(form, 'link-preview');
    set(form.elements.link, '{"url":"https://example.com/","title":"Third component"}', 'change');
    await waitForComponent(() => componentRowIn(form, 'link-preview')?.matches('.component-end-row'), 'three rows below Content');
    await removeTestComponent(componentEditor(form, 'video').querySelector('[data-component-remove]'));
    await moveTestComponent(section, 'up');
    await undoTestComponent(form.querySelector('[data-component-undo=video]'));
    closeComponentSettings(form);
    assert(row()?.closest('.CodeMirror') && !third.hidden &&
      JSON.stringify([...form.querySelectorAll('.component-end-row:not([hidden])')].map(row => row.dataset.componentRow)) === '["video","link-preview"]',
    'Moving an earlier row into Content must keep the removed row before its original following sibling.');

    const pendingAddSource = 'Writing before selecting the component.';
    cm.setValue(pendingAddSource);
    delayLayout(source => source === pendingAddSource);
    picker.querySelector('summary').click();
    picker.querySelector('[data-component-add=event]').click();
    await waitForComponent(() => pending, 'layout for Add while typing');
    cm.replaceRange(' More writing.', {line: 0, ch: pendingAddSource.length});
    await waitForComponent(() => form.querySelector('[data-component-placement-status]').textContent.includes('Choose Add component again'), 'Add retry feedback');
    assert(!form.hasAttribute('data-component-action-pending'), 'A change to the writing must release a canceled Add before the layout response arrives.');
    await resumeLayout();
    assert(!form.querySelector('.component-dialog[open]') && cm.getValue() === pendingAddSource + ' More writing.',
      'An outdated Add must keep the current writing and explain how to retry.');
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
