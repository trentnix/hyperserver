// Album gestures stay local to the list. The editor owns photo order, captions,
// and serialization. Global pointer listeners exist only during an active drag.
export function enableAlbumReordering(list, {move, announce, dropped, disabled, scrollRoot}) {
  let drag;
  const indexOf = item => [...list.children].indexOf(item);

  function finish(commit) {
    if (!drag) return;
    const current = drag;
    drag = null;
    current.events.abort();
    cancelAnimationFrame(current.frame);
    current.preview?.remove();
    current.marker?.remove();
    current.item.classList.remove('album-photo-dragging');
    list.classList.remove('album-reordering');
    if (list.hasPointerCapture(current.id)) list.releasePointerCapture(current.id);
    const changed = commit && current.started && move(indexOf(current.item), current.destination);
    if (changed) dropped(current.item);
    if (current.started) announce(commit ? changed ? `Photo moved to position ${indexOf(current.item) + 1} of ${list.children.length}.` : 'Photo position unchanged.' : 'Reordering canceled.');
  }

  function updatePosition() {
    if (!drag?.started) return;
    const {preview, marker, x, y} = drag;
    preview.style.left = Math.max(8, Math.min(x - preview.offsetWidth / 2, innerWidth - preview.offsetWidth - 8)) + 'px';
    preview.style.top = Math.max(8, Math.min(y - 36, innerHeight - preview.offsetHeight - 8)) + 'px';
    const bounds = list.getBoundingClientRect();
    marker.hidden = true;
    drag.destination = drag.original;
    if (x < bounds.left || x > bounds.right || y < bounds.top || y > bounds.bottom) return;
    // Gutters count as drop targets too. The marker shows the insertion edge
    // without shuffling cards beneath the pointer before the photo is dropped.
    let target;
    let distance = Infinity;
    for (const card of list.children) {
      const box = card.getBoundingClientRect();
      const dx = Math.max(box.left - x, 0, x - box.right);
      const dy = Math.max(box.top - y, 0, y - box.bottom);
      if (dx * dx + dy * dy < distance) {
        distance = dx * dx + dy * dy;
        target = card;
      }
    }
    if (!target) return;
    const box = target.getBoundingClientRect();
    const grid = getComputedStyle(list).display === 'grid';
    const after = grid ? y > box.bottom || (y >= box.top && x >= box.left + box.width / 2) : y >= box.top + box.height / 2;
    const slot = indexOf(target) + Number(after);
    drag.destination = slot > drag.original ? slot - 1 : slot;
    if (drag.destination === drag.original) return;
    if (grid) {
      const photo = target.querySelector('img').getBoundingClientRect();
      const height = Math.max(0, photo.height - 8);
      const center = box.top + box.height / 2;
      const top = Math.max(center - height / 2, bounds.top + 4);
      const bottom = Math.min(center + height / 2, bounds.bottom - 4);
      marker.style.left = (after ? box.right + 5 : box.left - 7) + 'px';
      marker.style.top = top + 'px';
      marker.style.width = '2px';
      marker.style.height = Math.max(0, bottom - top) + 'px';
    } else {
      marker.style.left = box.left + 4 + 'px';
      marker.style.top = (after ? box.bottom + 2 : box.top - 5) + 'px';
      marker.style.width = box.width - 8 + 'px';
      marker.style.height = '2px';
    }
    marker.hidden = false;
  }

  function scroll() {
    if (!drag) return;
    if (!list.isConnected || disabled()) { finish(false); return; }
    if (drag.started) {
      const bounds = scrollRoot?.getBoundingClientRect() || {top: 0, bottom: innerHeight};
      const amount = drag.y < bounds.top + 64 ? (drag.y - bounds.top - 64) / 4 : drag.y > bounds.bottom - 64 ? (drag.y - bounds.bottom + 64) / 4 : 0;
      if (amount) (scrollRoot || window).scrollBy(0, Math.max(-16, Math.min(16, amount)));
      updatePosition();
    }
    drag.frame = requestAnimationFrame(scroll);
  }

  list.addEventListener('pointerdown', event => {
    const handle = event.target.closest('[data-album-drag]');
    if (!handle || handle.disabled || disabled() || event.button !== 0 || drag) return;
    if (event.pointerType !== 'mouse' && !event.target.closest('.album-order-grip')) return;
    const item = handle.closest('[data-album-index]');
    const events = new AbortController();
    const original = indexOf(item);
    drag = {id: event.pointerId, handle, item, original, destination: original, x: event.clientX, y: event.clientY, startX: event.clientX, startY: event.clientY, events};
    handle.focus({preventScroll: true});
    // Capture on the stable list, because moving a card can release capture
    // owned by that card. Synthetic pointers do not own browser capture.
    try { list.setPointerCapture(event.pointerId); } catch { /* No active browser pointer. */ }
    document.addEventListener('pointermove', event => {
      if (!drag || event.pointerId !== drag.id) return;
      drag.x = event.clientX;
      drag.y = event.clientY;
      if (!drag.started && Math.hypot(drag.x - drag.startX, drag.y - drag.startY) >= 6) {
        drag.started = true;
        drag.item.classList.add('album-photo-dragging');
        list.classList.add('album-reordering');
        const preview = document.createElement('div');
        preview.className = 'album-drag-preview';
        preview.setAttribute('aria-hidden', 'true');
        preview.style.width = Math.min(180, item.getBoundingClientRect().width) + 'px';
        preview.append(item.querySelector('img').cloneNode());
        (list.closest('dialog') || document.body).append(preview);
        drag.preview = preview;
        const marker = document.createElement('div');
        marker.className = 'album-drop-marker';
        marker.setAttribute('aria-hidden', 'true');
        (list.closest('dialog') || document.body).append(marker);
        drag.marker = marker;
      }
      if (drag.started) { event.preventDefault(); updatePosition(); }
    }, {signal: events.signal, passive: false});
    document.addEventListener('pointerup', event => {
      if (event.pointerId === drag?.id) finish(true);
    }, {signal: events.signal});
    document.addEventListener('pointercancel', event => {
      if (event.pointerId === drag?.id) finish(false);
    }, {signal: events.signal});
    document.addEventListener('keydown', event => {
      if (event.key === 'Escape') { event.preventDefault(); finish(false); }
    }, {signal: events.signal, capture: true});
    window.addEventListener('blur', () => finish(false), {signal: events.signal});
    document.addEventListener('htmx:before:swap', event => {
      const candidate = event.detail.ctx.target;
      const target = typeof candidate === 'string' ? document.querySelector(candidate) : candidate;
      if (target?.contains(list)) finish(false);
    }, {signal: events.signal});
    drag.frame = requestAnimationFrame(scroll);
  });
  list.addEventListener('lostpointercapture', event => {
    if (event.pointerId === drag?.id) finish(false);
  });
  list.addEventListener('keydown', event => {
    const handle = event.target.closest('[data-album-drag]');
    if (!handle || handle.disabled || disabled() || drag || !['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault();
    const index = indexOf(handle.closest('[data-album-index]'));
    const firstRow = list.firstElementChild.getBoundingClientRect().top;
    const columns = getComputedStyle(list).display === 'grid' ? [...list.children].filter(card => Math.abs(card.getBoundingClientRect().top - firstRow) < 1).length : 1;
    const step = ['ArrowUp', 'ArrowDown'].includes(event.key) ? columns : 1;
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? list.children.length - 1 : index + (['ArrowRight', 'ArrowDown'].includes(event.key) ? step : -step);
    if (move(index, next)) {
      handle.scrollIntoView({block: 'nearest', inline: 'nearest'});
      announce(`Photo moved to position ${next + 1} of ${list.children.length}.`);
    }
  });
  return {cancel: () => finish(false), get dragging() { return Boolean(drag); }};
}
