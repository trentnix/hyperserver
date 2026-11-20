(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const set = (field, value) => {
    field.value = value;
    field.dispatchEvent(new Event('input', {bubbles: true}));
  };
  const wait = waitForComponent;
  const key = (target, value, options = {}) => target.dispatchEvent(new KeyboardEvent('keydown', {
    key: value, bubbles: true, cancelable: true, ...options
  }));
  const stage = sessionStorage.getItem('component-instance-stage');
  const text = 'Opening paragraph.\n\nMiddle paragraph.\n\nClosing paragraph.';
  try {
    if (stage === 'published') {
      const body = document.querySelector('.post-body');
      const views = [...body.querySelectorAll('.web-embed')];
      assert(views.length === 2 && views[0].textContent.includes('First chart') && views[1].textContent.includes('Second chart') &&
        views[0].compareDocumentPosition(views[1]) & Node.DOCUMENT_POSITION_FOLLOWING, 'Published components must keep their independent content and order.');
      const edit = document.querySelector('a[href^="/post/update/"]');
      await htmx.ajax('GET', edit.getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
    }
    await wait(() => document.querySelector('#post-form .CodeMirror'), 'Content editor');
    const form = document.querySelector('#post-form');
    const cm = form.querySelector('.CodeMirror').CodeMirror;
    const addEmbed = () => addTestComponent(form.querySelector('[data-component-add=embed]'));
    const title = panel => panel.querySelector('[id^=embed-title]');
    const instance = panel => panel.dataset.componentId || panel.dataset.postComponent;
    const order = () => JSON.parse(form.elements.componentOrder.value);
    const marker = id => '<!-- hyperserver:component ' + id + ' -->';
    const rowName = id => componentRowIn(form, id)?.getAttribute('aria-label');
    const open = async id => {
      await wait(() => componentRowIn(form, id), 'component row ' + id);
      componentRowIn(form, id).querySelector('[data-component-edit]').click();
      return componentEditor(form, id);
    };
    if (stage === 'published') {
      const ids = JSON.parse(sessionStorage.getItem('review-component-ids'));
      assert(order().join() === ids.join(), 'Reloading must preserve instance IDs.');
      for (const [id, label] of [[ids[0], 'First chart'], [ids[1], 'Second chart']]) {
        await wait(() => rowName(id) === 'Embed · ' + label, 'saved title as the editor label');
        const panel = await open(id);
        assert(panel.matches('.component-panel-active') && title(panel).value === label, 'Reopening must focus the correct instance and restore its title.');
        closeComponentSettings(form);
      }
      const second = await open(ids[1]);
      set(title(second), 'First chart');
      const first = componentEditor(form, ids[0]);
      await removeTestComponent(second.querySelector('[data-component-remove]'));
      const earlier = form.querySelector('[data-component-undo]');
      assert(earlier.dataset.componentUndo === ids[1] && /^Undo removal of Embed #[a-f0-9]+ · First chart$/.test(earlier.getAttribute('aria-label')),
        'The removal notice must distinguish a component with a matching title.');
      await removeTestComponent(first.querySelector('[data-component-remove]'));
      const latest = form.querySelector('[data-component-undo]');
      assert(!earlier.isConnected && form.querySelectorAll('[data-component-undo]').length === 1 &&
        latest.dataset.componentUndo === ids[0] && latest.getAttribute('aria-label') === 'Undo removal of Embed · First chart',
      'A new removal must replace the previous notice and identify its own component.');
      const previousFocus = document.activeElement;
      latest.focus();
      assert(getComputedStyle(latest.parentElement).animationPlayState === 'paused',
        'The notice must stay available while Undo has keyboard focus.');
      previousFocus.focus();
      await wait(() => !latest.isConnected, 'removal notice fading out');
      assert(!form.querySelector('[data-component-undo]') && order().length === 0,
        'Fading out must remove the notice while leaving both removals in effect.');
      key(document.activeElement, 'z', {ctrlKey: true});
      key(document.activeElement, 'z', {ctrlKey: true});
      assert(order().join() === ids.join() && title(first).value === 'First chart' && title(second).value === 'First chart',
        'Keyboard Undo must restore both components after their notices are gone.');
      await removeTestComponent(second.querySelector('[data-component-remove]'));
      await removeTestComponent(first.querySelector('[data-component-remove]'));
      const replacement = await addEmbed();
      assert(!ids.includes(instance(replacement)) && !title(replacement).value &&
        form.querySelector('[data-component-undo]').dataset.componentUndo === ids[0],
      'Adding a replacement must preserve the original component’s Undo action and use a distinct instance ID.');
      closeComponentSettings(form);
      await undoTestComponent(form.querySelector('[data-component-undo]'));
      assert(title(first).value === 'First chart' && !title(replacement).value,
        'Undo must restore the removed instance independently of the replacement’s settings.');
      await report('PASS');
      return;
    }

    if (stage === 'restore') {
      await wait(() => !form.querySelector('[data-draft-recovery]').hidden, 'recoverable copy');
      cm.setValue('Other prose.');
      const unrelated = await addEmbed();
      set(title(unrelated), 'Unrelated chart');
      closeComponentSettings(form);
      form.querySelector('[data-draft-restore]').click();
      document.querySelector('[data-confirm-accept]')?.click();
      const ids = JSON.parse(sessionStorage.getItem('review-component-ids'));
      await wait(() => order().join() === ids.join() && title(componentEditor(form, ids[1])).value === 'Second chart', 'both instances restored from the browser');
      const restored = cm.getValue();
      cm.undo();
      assert(cm.getValue() === restored && order().join() === ids.join(), 'Undo must stop at the browser restore boundary.');
      assert(form.querySelector('[data-draft-status]').textContent.includes('Copy restored'), 'Recovery must retain the restored state.');
      form.elements.published.value = 'on';
      sessionStorage.setItem('component-instance-stage', 'published');
      form.requestSubmit();
      return;
    }

    set(form.elements.title, 'Two embeds for review');
    set(form.elements.slug, 'two-embeds-for-review');
    cm.setValue(text);
    cm.focus();
    cm.setCursor({line: 2, ch: 0});
    const first = await addEmbed();
    set(first.querySelector('[id^=embed-url]'), 'https://widgets.example.test/first');
    set(title(first), 'First chart');
    closeComponentSettings(form);
    cm.focus();
    cm.setCursor({line: 5, ch: 0});
    const second = await addEmbed();
    const id = instance(second);
    const reference = '#' + id.split(':')[1].slice(0, 6);
    await wait(() => rowName(id) === 'Embed ' + reference, 'an untitled instance reference');
    closeComponentSettings(form);
    form.requestSubmit();
    await wait(() => second.matches('.component-panel-active') && document.activeElement === second.querySelector('[id^=embed-url]'),
      'native validation of the second embed');
    set(second.querySelector('[id^=embed-url]'), 'https://widgets.example.test/second');
    set(title(second), 'First chart');
    assert(rowName(id) === `Embed ${reference} · First chart` && rowName(instance(first)) === 'Embed · First chart',
      'Matching titles must have distinct labels.');
    const longTitle = 'W'.repeat(220);
    set(title(second), longTitle);
    const longRow = componentRowIn(form, id);
    const longLabel = longRow.querySelector('strong');
    const shortLabel = longLabel.getBoundingClientRect().width;
    const editButton = longRow.querySelector('[data-component-edit]');
    assert(shortLabel < longLabel.scrollWidth && longLabel.title === 'Embed · ' + longTitle &&
      editButton.getBoundingClientRect().right <= cm.getWrapperElement().getBoundingClientRect().right,
      'A long title must truncate while preserving the full label and the Edit button.');
    set(title(second), 'Second chart');
    closeComponentSettings(form);
    assert(first !== second && id !== instance(first) && title(first).value === 'First chart', 'Each same-type component needs separate fields.');
    await wait(() => rowName(id) === 'Embed · Second chart', 'title as the editor label');
    const original = cm.getValue();
    let moves = 0;
    while (moves < 4 && order()[0] !== id) {
      await moveTestComponent(second, 'up');
      moves++;
    }
    assert(order()[0] === id && rowName(id) === 'Embed · Second chart' && rowName(instance(first)) === 'Embed · First chart',
      'Moving an embed before its sibling must preserve both labels.');
    for (let step = 0; step < moves; step++) key(document.activeElement, 'z', {ctrlKey: true});
    assert(cm.getValue() === original && title(second).value === 'Second chart', 'Ctrl+Z on the Move control must undo the move.');
    key(document.activeElement, 'z', {metaKey: true, shiftKey: true});
    assert(cm.getValue() !== original, 'Redo must work from the component row.');
    key(document.activeElement, 'z', {ctrlKey: true});
    await removeTestComponent(second.querySelector('[data-component-remove]'));
    assert(order().join() === 'embed' && !cm.getValue().includes(marker(id)) && title(first).value === 'First chart',
      'Removing the second embed must leave the first intact.');
    const undo = form.querySelector('[data-component-undo="' + id + '"]');
    assert(undo.parentElement.querySelector('span').textContent === 'Embed · Second chart removed.' &&
      undo.getAttribute('aria-label') === 'Undo removal of Embed · Second chart', 'Removal and Undo must identify the instance by title.');
    await undoTestComponent(undo);
    assert(cm.getValue() === original && title(second).value === 'Second chart', 'Undo removal must restore the same instance and position.');
    closeComponentSettings(form);

    const firstID = instance(first);
    const adjacent = 'Opening.\n\n' + marker(firstID) + '\n' + marker(id) + '\n\nClosing.';
    const removeBoth = async () => {
      for (const panel of [first, second]) await removeTestComponent(panel.querySelector('[data-component-remove]'));
    };
    cm.setValue(adjacent);
    await wait(() => componentRowIn(form, id)?.closest('.CodeMirror'), 'adjacent components');
    await removeBoth();
    await undoTestComponent(form.querySelector('[data-component-undo="' + id + '"]'));
    closeComponentSettings(form);
    assert(order().join() === id && !form.querySelector('[data-component-undo]'),
      'Undoing the latest removal must clear its notice without resurfacing the earlier one.');
    cm.undo();
    cm.undo();
    cm.undo();
    assert(cm.getValue() === adjacent && order().join() === [firstID, id].join() &&
      title(first).value === 'First chart' && title(second).value === 'Second chart',
    'Keyboard Undo must restore adjacent removals in their original order with their settings.');

    const writing = 'Both components are below this writing.';
    cm.setValue(writing);
    const trailing = instanceID => form.querySelector('.component-end-row[data-component-row="' + instanceID + '"]:not([hidden])');
    await wait(() => trailing(firstID) && trailing(id), 'two trailing rows');
    set(title(second), longTitle);
    const trailingLabel = trailing(id).querySelector('strong');
    assert(trailingLabel.scrollWidth > trailingLabel.clientWidth &&
      trailing(id).getBoundingClientRect().right <= form.getBoundingClientRect().right + 1,
    'A long title must truncate in the list below Content without widening the page.');
    set(title(second), 'Second chart');
    await removeBoth();
    cm.undo();
    cm.undo();
    assert(cm.getValue() === writing && order().join() === [firstID, id].join(),
      'Undo must also keep the order of adjacent removals below the editor.');

    const pointerType = innerWidth <= 360 ? 'touch' : 'mouse';
    const pointer = (target, type, x, y) => target.dispatchEvent(new PointerEvent(type, {
      pointerType, pointerId: 25, button: 0, clientX: x, clientY: y, bubbles: true, cancelable: true
    }));
    const drag = async (from, to, below = false) => {
      const handle = from.querySelector('[data-component-drag]');
      to.scrollIntoView({block: 'center'});
      const start = handle.getBoundingClientRect();
      const target = to.getBoundingClientRect();
      const x = target.left + 12;
      const y = below ? target.bottom - 4 : target.top + 4;
      pointer(handle, 'pointerdown', start.left + start.width / 2, start.top + start.height / 2);
      pointer(document, 'pointermove', x, y);
      await wait(() => document.querySelector('.component-drop-cue:not([hidden])'), 'drop cue below the editor');
      pointer(document, 'pointerup', x, y);
    };
    await drag(trailing(id), trailing(firstID));
    assert(order().join() === [id, firstID].join() && cm.getValue() === writing &&
      document.activeElement === trailing(id).querySelector('[data-component-place]'),
    'Dragging between trailing rows must update the order, preserve the writing, and return focus.');
    cm.undo();
    assert(order().join() === [firstID, id].join(), 'Keyboard Undo must restore the order after a trailing drag.');

    cm.setValue(writing + '\n\n' + marker(firstID) + '\n');
    await wait(() => componentRowIn(form, firstID)?.closest('.CodeMirror') && trailing(id), 'one inline row and one trailing row');
    await drag(componentRowIn(form, firstID), trailing(id), true);
    assert(!cm.getValue().includes(marker(firstID)) && order().join() === [id, firstID].join(),
      'Dragging an inline component below the final trailing row must put it last.');
    cm.undo();
    assert(cm.getValue().includes(marker(firstID)) && order().join() === [firstID, id].join(),
      'Undo must move the component back into the writing.');
    cm.setValue(original);
    await wait(() => form.querySelector('[data-draft-status]').textContent.startsWith('Saved locally'), 'recovery copy with both instances');
    sessionStorage.setItem('review-component-ids', JSON.stringify(order()));
    sessionStorage.setItem('component-instance-stage', 'restore');
    location.assign('/post/create?restore-instances');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
