// Use the same Markdown boundaries as the keyboard placement action. The
// pointer only chooses among those boundaries and never edits the document.
export function enableComponentDrag(cm, layout, signal) {
  const wrapper = cm.getWrapperElement();
  const form = wrapper.closest('form');
  const cue = document.createElement('div');
  cue.className = 'component-drop-cue';
  cue.setAttribute('aria-hidden', 'true');
  let drag;
  let suppressClick = false;

  const blocked = () => form.dataset.uploading || form.hasAttribute('data-component-action-pending');

  function finish(commit) {
    if (!drag) return;
    const current = drag;
    drag = null;
    current.lifetime.abort();
    cancelAnimationFrame(current.frame);
    cue.remove();
    current.handle.closest('.component-marker')?.classList.remove('component-marker-dragging');
    wrapper.classList.remove('component-dragging');
    if (wrapper.hasPointerCapture(current.id)) wrapper.releasePointerCapture(current.id);
    if (commit && current.destination !== undefined && cm.getValue() === current.source) {
      wrapper.dispatchEvent(new CustomEvent('blog:component-drop', {
        bubbles: true, detail: {type: current.type, destination: current.destination, layout: {...current.layout, source: current.source}}
      }));
    }
  }

  function bounds() {
    const box = wrapper.getBoundingClientRect();
    return {left: box.left, right: box.right, top: Math.max(0, box.top), bottom: Math.min(innerHeight, box.bottom)};
  }

  function showTarget(destination, top, box) {
    if (top < 0 || top > innerHeight) return;
    drag.destination = destination;
    cue.style.left = box.left + 8 + 'px';
    cue.style.top = top + 'px';
    cue.style.width = box.right - box.left - 16 + 'px';
    cue.hidden = false;
  }

  function targetBelowEditor() {
    const {x, y, type, endRows} = drag;
    const box = endRows?.getBoundingClientRect();
    if (!box || x < box.left || x > box.right || y < box.top || y > box.bottom) return;
    const rows = [...endRows.children].filter(row => !row.hidden);
    if (!rows.length) return;
    const positions = rows.map(row => row.getBoundingClientRect());
    const next = positions.findIndex(row => y < (row.top + row.bottom) / 2);
    const slot = next === -1 ? rows.length : next;
    const current = rows.findIndex(row => row.dataset.componentRow === type);
    if (current !== -1 && (slot === current || slot === current + 1)) return;
    const top = ((positions[slot - 1]?.bottom ?? box.top) + (positions[slot]?.top ?? box.bottom)) / 2;
    showTarget(rows[slot]?.dataset.componentRow ?? null, top, box);
  }

  function updateTarget() {
    cue.hidden = true;
    drag.destination = undefined;
    const {x, y, layout, type} = drag;
    const lines = layout?.drops;
    const start = layout?.markers[type];
    const box = bounds();
    if (!layout) return;
    if (y > box.bottom) {
      targetBelowEditor();
      return;
    }
    if (!lines.length || x < box.left || x > box.right || y < box.top) return;

    // Block positions are sorted. Compare the two boundaries closest to the
    // pointer, using the editor's rendered heights for wrapped lines.
    const height = index => cm.heightAtLine(lines[index], 'window');
    let left = 0, right = lines.length - 1;
    while (left < right) {
      const middle = Math.floor((left + right) / 2);
      if (height(middle) < y) left = middle + 1;
      else right = middle;
    }
    const index = left > 0 && y - height(left - 1) < height(left) - y ? left - 1 : left;
    const target = lines[index];
    const top = height(index);
    if (target === start || target === start + 1 || top < box.top || top > box.bottom) return;
    showTarget(target, top, box);
  }

  function tick() {
    if (!drag) return;
    if (!wrapper.isConnected || blocked()) { finish(false); return; }
    const box = bounds();
    if (drag.x >= box.left && drag.x <= box.right) {
      const {y} = drag;
      const amount = y < box.top + 40 ? (y - box.top - 40) / 4 : y > box.bottom - 40 ? (y - box.bottom + 40) / 4 : 0;
      const delta = Math.max(-16, Math.min(16, amount));
      if (delta) {
        const top = cm.getScrollInfo().top;
        if (y >= box.top && y <= box.bottom) cm.scrollTo(null, top + delta);
        const nearPageEdge = delta < 0 ? y < 40 : y > innerHeight - 40;
        if (nearPageEdge && cm.getScrollInfo().top === top) window.scrollBy(0, delta);
      }
    }
    updateTarget();
    drag.frame = requestAnimationFrame(tick);
  }

  async function loadBoundaries(current) {
    try {
      const positions = await layout(current.source);
      if (drag !== current) return;
      current.layout = positions;
      updateTarget();
    } catch {
      if (drag !== current) return;
      finish(false);
      wrapper.dispatchEvent(new Event('blog:component-drag-error', {bubbles: true}));
    }
  }

  form.addEventListener('pointerdown', event => {
    const handle = event.target.closest('[data-component-drag]');
    if (!handle || event.button !== 0 || blocked() || drag) return;
    suppressClick = false;
    const type = handle.dataset.componentPlace;
    const lifetime = new AbortController();
    drag = {
      id: event.pointerId, type, handle, lifetime,
      endRows: form.querySelector('.component-end-list'),
      source: cm.getValue(), x: event.clientX, y: event.clientY, originX: event.clientX, originY: event.clientY
    };
    const options = {signal: lifetime.signal};
    document.addEventListener('pointermove', event => {
      if (!drag || event.pointerId !== drag.id) return;
      drag.x = event.clientX;
      drag.y = event.clientY;
      if (!drag.started) {
        if (Math.hypot(drag.x - drag.originX, drag.y - drag.originY) < 6) return;
        drag.started = suppressClick = true;
        handle.closest('.component-marker').classList.add('component-marker-dragging');
        wrapper.classList.add('component-dragging');
        cue.hidden = true;
        document.body.append(cue);
        try { wrapper.setPointerCapture(event.pointerId); } catch { /* Synthetic test pointers have no capture. */ }
        loadBoundaries(drag);
        tick();
      }
      event.preventDefault();
    }, {...options, passive: false});
    document.addEventListener('pointerup', event => {
      if (event.pointerId !== drag?.id) return;
      if (!drag.started || blocked()) { finish(false); return; }
      const shown = drag.destination;
      drag.x = event.clientX;
      drag.y = event.clientY;
      updateTarget();
      finish(shown !== undefined && drag.destination === shown);
    }, options);
    const cancelPointer = event => { if (event.pointerId === drag?.id) finish(false); };
    document.addEventListener('pointercancel', cancelPointer, options);
    wrapper.addEventListener('lostpointercapture', event => {
      // Touch starts with implicit capture on the handle. Transferring that
      // capture to the editor is part of starting a drag.
      if (event.target === wrapper) cancelPointer(event);
    }, options);
    window.addEventListener('blur', () => finish(false), options);
    document.addEventListener('keydown', event => {
      if (event.key !== 'Escape' || !drag?.started) return;
      event.preventDefault();
      finish(false);
    }, {...options, capture: true});
  }, {signal});

  form.addEventListener('click', event => {
    if (!suppressClick || !event.detail || !event.target.closest('[data-component-drag]')) return;
    event.preventDefault();
    event.stopPropagation();
  }, {capture: true, signal});
  const cancel = () => finish(false);
  cm.on('change', cancel);
  return () => {
    cm.off('change', cancel);
    cancel();
  };
}
