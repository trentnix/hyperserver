(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const visibleIn = (item, area) => {
    const bounds = item.getBoundingClientRect();
    const limit = area.getBoundingClientRect();
    return bounds.width > 0 && bounds.left >= limit.left - 1 && bounds.right <= limit.right + 1 &&
      bounds.top >= limit.top - 1 && bounds.bottom <= limit.bottom + 1;
  };
  const narrow = innerWidth < 320;
  function checkScroll(body, action) {
    const dialog = body.closest('dialog');
    const before = action.getBoundingClientRect();
    const pageScroll = window.scrollX;
    body.scrollLeft = body.scrollWidth;
    const after = action.getBoundingClientRect();
    assert((body.scrollLeft > 0) === narrow && before.left === after.left && visibleIn(action, dialog) &&
      after.right <= document.documentElement.clientWidth && after.bottom <= innerHeight && window.scrollX === pageScroll,
      dialog.className + ' must keep its actions in view and scroll horizontally only below the minimum width.');
    body.scrollLeft = 0;
  }
  function checkMediaScroll(dialog, pane) {
    const layout = dialog.querySelector('.media-browser-layout');
    const heading = layout.querySelector('h2').getBoundingClientRect();
    const cancel = layout.querySelector('[data-close-dialog]').getBoundingClientRect();
    const pageScroll = window.scrollX;
    const sections = [layout.querySelector('header'), pane, layout.querySelector('footer')].filter(Boolean);
    assert(layout.getBoundingClientRect().width >= 294 && cancel.left >= heading.right &&
      cancel.top < heading.bottom && heading.top < cancel.bottom,
      'The chooser must keep its minimum width and put its title and Cancel on one row.');
    dialog.scrollLeft = 0;
    const start = sections.map(section => section.getBoundingClientRect().left);
    dialog.scrollLeft = dialog.scrollWidth;
    assert((dialog.scrollLeft > 0) === narrow && pane.scrollWidth <= pane.clientWidth + 1 &&
      sections.every((section, i) => Math.abs(section.getBoundingClientRect().left - start[i] + dialog.scrollLeft) < 1),
      'The header, photos, and footer must scroll together below the minimum width.');
    assert(pane.getBoundingClientRect().height >= 120, 'The chooser must leave enough height for the gallery.');
    for (const action of layout.querySelectorAll('header button, footer button:not([hidden])')) {
      action.scrollIntoView({block: 'nearest', inline: 'nearest'});
      assert(action.getBoundingClientRect().height >= 44 && visibleIn(action, dialog) && window.scrollX === pageScroll,
        'Chooser actions must be reachable at the viewport edge without scrolling the page.');
    }
    dialog.scrollLeft = 0;
  }
  const close = async (dialog, button, opener) => {
    button.click();
    await waitForComponent(() => !dialog.isConnected && document.activeElement === opener, 'return to component settings');
  };

  try {
    const form = document.querySelector('#post-form');
    await waitForComponent(() => !form.querySelector('[data-component-picker]').hidden, 'component chooser');
    const minimum = form.closest('.article-column').getBoundingClientRect().width - 26;
    const settings = form.querySelector('.component-dialog');
    const body = settings.querySelector('.component-dialog-body');
    const done = settings.querySelector('[data-component-done]');
    for (const add of form.querySelectorAll('[data-component-add]')) {
      const type = add.dataset.componentAdd;
      await addTestComponent(add);
      const section = componentEditor(form, type);
      assert((narrow ? section.getBoundingClientRect().width >= minimum : section.getBoundingClientRect().width <= body.clientWidth + 1) &&
        section.scrollWidth <= section.clientWidth + 1, type + ' must fit its fields, accounting for the dialog scrollbar.');
      checkScroll(body, done);
      if (type === 'photo-album') {
        const browse = section.querySelector('[data-album-browse]');
        browse.focus();
        browse.click();
        await waitForComponent(() => document.querySelectorAll('.media-browser [data-media-select]').length === 2, 'album image choices');
        const library = document.querySelector('.media-browser');
        const results = library.querySelector('[data-media-results]');
        const addPhotos = library.querySelector('[data-media-add-selected]');
        checkMediaScroll(library, results);
        for (const photo of results.querySelectorAll('[data-media-select]')) photo.click();
        library.querySelector('[data-media-show-selected]').click();
        checkMediaScroll(library, library.querySelector('.media-selection-review'));
        await close(library, addPhotos, browse);

        const reorder = section.querySelector('[data-album-reorder]');
        reorder.focus();
        reorder.click();
        const order = document.querySelector('.album-order-dialog');
        const list = order.querySelector('.album-order-list');
        const row = list.firstElementChild;
        const thumbnail = row.querySelector('img').getBoundingClientRect();
        const up = row.querySelector('[data-album-move=up]').getBoundingClientRect();
        const down = row.querySelector('[data-album-move=down]');
        const next = down.getBoundingClientRect();
        assert(thumbnail.right < up.left && next.left - up.right >= 16 && next.width >= 44 && next.height >= 44 &&
          next.right < row.getBoundingClientRect().right, 'Reorder must keep the thumbnail and both touch targets apart.');
        const cancelOrder = order.querySelector('[data-album-order-cancel]');
        const doneOrder = order.querySelector('[data-album-order-done]');
        const cancelBox = cancelOrder.getBoundingClientRect();
        const doneBox = doneOrder.getBoundingClientRect();
        assert(cancelBox.width === doneBox.width && cancelBox.height === doneBox.height &&
          cancelBox.top === doneBox.top && doneBox.left - cancelBox.right >= 12 && visibleIn(cancelOrder, order),
        'Cancel and Done must have equal dimensions and stay apart on the narrow dialog.');
        checkScroll(list, doneOrder);
        if (narrow) {
          const grip = row.querySelector('.album-order-grip');
          const start = grip.getBoundingClientRect();
          const target = list.lastElementChild.getBoundingClientRect();
          const pointer = (node, name, x, y) => node.dispatchEvent(new PointerEvent(name, {
            bubbles: true, cancelable: true, pointerType: 'touch', pointerId: 17, button: 0, clientX: x, clientY: y
          }));
          pointer(grip, 'pointerdown', start.left + 10, start.top + 10);
          pointer(document, 'pointermove', target.left + 20, target.bottom - 10);
          const cue = order.querySelector('.album-drop-marker:not([hidden])');
          assert(cue && visibleIn(cue, list), 'The drop cue must stay within the scrollable list at 240px.');
          pointer(document, 'pointercancel', target.left + 20, target.bottom - 10);
        }
        list.scrollLeft = list.scrollWidth;
        assert(visibleIn(down, list), 'Scrolling must reveal the entire move button.');
        down.click();
        assert(list.lastElementChild === row, 'Move down must work at the minimum width.');
        await close(order, cancelOrder, reorder);

        const photo = section.querySelector('[data-album-photo]');
        photo.focus();
        photo.click();
        const viewer = document.querySelector('.album-viewer');
        const dismiss = viewer.querySelector('[data-album-close]');
        assert(visibleIn(dismiss, viewer) && dismiss.getBoundingClientRect().right <= document.documentElement.clientWidth,
          'The full-screen photo viewer must keep Close in view.');
        await close(viewer, dismiss, photo);
      } else if (type === 'event') {
        const browse = section.querySelector('[data-media-browse]');
        browse.focus();
        browse.click();
        await waitForComponent(() => document.querySelector('.media-browser [data-media-select]'), 'single-image choices');
        const library = document.querySelector('.media-browser');
        const cancel = library.querySelector('[data-close-dialog]');
        checkMediaScroll(library, library.querySelector('[data-media-results]'));
        await close(library, cancel, browse);
      } else if (type === 'link-preview' || type === 'video') {
        const prefix = type === 'video' ? 'video' : 'link';
        const url = section.querySelector('#' + prefix + '-url');
        const title = section.querySelector('#' + prefix + '-title');
        url.value = type === 'video' ? 'https://youtu.be/M7lc1UVf-VE' : 'https://example.com/article';
        title.value = 'Existing title';
        for (const input of [url, title]) input.dispatchEvent(new Event('input', {bubbles: true}));
        const fetchDetails = section.querySelector('[data-' + prefix + '-fetch]');
        fetchDetails.focus();
        fetchDetails.click();
        const confirmation = document.querySelector('.editor-confirmation');
        const cancel = confirmation.querySelector('[data-confirm-cancel]');
        assert(visibleIn(cancel, confirmation) && visibleIn(confirmation.querySelector('[data-confirm-accept]'), confirmation),
          'Both confirmation buttons must fit in the dialog.');
        await close(confirmation, cancel, fetchDetails);
      } else {
        const browse = section.querySelector('[data-download-browse], [data-audio-browse]');
        if (browse) {
          browse.focus();
          browse.click();
          const library = document.querySelector('.download-library');
          const cancel = library.querySelector('.form-actions button');
          checkScroll(library.querySelector('.download-library-body'), cancel);
          await close(library, cancel, browse);
        }
      }
      const duplicate = await addTestComponent(add);
      const source = duplicate.querySelector('textarea[name]');
      const ids = [...form.querySelectorAll('[id]')].map(node => node.id);
      assert(duplicate !== section && duplicate.dataset.componentId === source.name && source.hidden &&
        new Set(ids).size === ids.length && !duplicate.querySelector('.image-upload-controls[hidden]'),
        type + ' must initialize a separate panel with unique IDs and its image controls ready.');
      await removeTestComponent(duplicate.querySelector('[data-component-remove]'));
      await removeTestComponent(section.querySelector('[data-component-remove]'));
    }
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
