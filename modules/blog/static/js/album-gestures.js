// Fit, zoom, and pan use the displayed image bounds. A gesture that starts
// zoomed or uses two fingers cannot also advance the album.
export function enableAlbumGestures(frame, image, {navigate, changed, enabled}) {
  let scale = 1;
  let x = 0;
  let y = 0;
  let gesture;
  let pinch;
  let multiple = false;
  let lastTap;
  let lastTouch = -Infinity;
  const pointers = new Map();
  const events = new AbortController();
  const clamp = (value, min, max) => Math.max(min, Math.min(max, value));
  function draw() {
    const width = frame.clientWidth;
    const height = frame.clientHeight;
    if (!width || !height) return;
    const ratio = image.naturalWidth / image.naturalHeight || width / height;
    const fittedWidth = Math.min(width, height * ratio);
    const fittedHeight = Math.min(height, width / ratio);
    const limitX = Math.max(0, (fittedWidth * scale - width) / 2);
    const limitY = Math.max(0, (fittedHeight * scale - height) / 2);
    x = clamp(x, -limitX, limitX);
    y = clamp(y, -limitY, limitY);
    image.style.transform = `translate(${x}px, ${y}px) scale(${scale})`;
    frame.classList.toggle('album-zoomed', scale > 1);
    changed(scale);
  }
  function zoom(next, point) {
    const bounds = frame.getBoundingClientRect();
    const focalX = point ? point.x - bounds.left - bounds.width / 2 : 0;
    const focalY = point ? point.y - bounds.top - bounds.height / 2 : 0;
    const nextScale = clamp(next, 1, 4);
    x = (x - focalX) * nextScale / scale + focalX;
    y = (y - focalY) * nextScale / scale + focalY;
    scale = nextScale;
    draw();
  }
  function clear() {
    for (const id of pointers.keys()) {
      if (frame.hasPointerCapture(id)) frame.releasePointerCapture(id);
    }
    pointers.clear();
    gesture = null;
    pinch = null;
    multiple = false;
    lastTap = null;
    frame.classList.remove('album-panning');
  }
  function geometry() {
    const [a, b] = [...pointers.values()];
    return {distance: Math.hypot(a.x - b.x, a.y - b.y), x: (a.x + b.x) / 2, y: (a.y + b.y) / 2};
  }
  frame.addEventListener('pointerdown', event => {
    if (!enabled() || event.target.closest('button') || image.hidden || !image.naturalWidth || (event.pointerType === 'mouse' && event.button !== 0)) return;
    if (event.pointerType === 'touch') lastTouch = performance.now();
    const point = {x: event.clientX, y: event.clientY};
    pointers.set(event.pointerId, point);
    try { frame.setPointerCapture(event.pointerId); } catch { /* Synthetic pointers have no native capture. */ }
    if (pointers.size === 1) {
      gesture = {id: event.pointerId, ...point, panX: x, panY: y, zoomed: scale > 1, touch: event.pointerType === 'touch'};
      frame.classList.toggle('album-panning', scale > 1);
    } else if (pointers.size === 2) {
      multiple = true;
      const bounds = frame.getBoundingClientRect();
      pinch = {...geometry(), scale, panX: x, panY: y, centerX: bounds.left + bounds.width / 2, centerY: bounds.top + bounds.height / 2};
    }
  });
  frame.addEventListener('pointermove', event => {
    if (!pointers.has(event.pointerId)) return;
    pointers.set(event.pointerId, {x: event.clientX, y: event.clientY});
    if (pointers.size === 2 && pinch) {
      const current = geometry();
      scale = clamp(pinch.scale * current.distance / Math.max(1, pinch.distance), 1, 4);
      const factor = scale / pinch.scale;
      x = (pinch.panX - pinch.x + pinch.centerX) * factor + current.x - pinch.centerX;
      y = (pinch.panY - pinch.y + pinch.centerY) * factor + current.y - pinch.centerY;
      draw();
    } else if (!multiple && gesture?.zoomed) {
      x = gesture.panX + event.clientX - gesture.x;
      y = gesture.panY + event.clientY - gesture.y;
      draw();
    }
  });
  function finish(event, canceled = false) {
    if (!pointers.has(event.pointerId)) return;
    pointers.delete(event.pointerId);
    if (frame.hasPointerCapture(event.pointerId)) frame.releasePointerCapture(event.pointerId);
    if (!canceled && !multiple && gesture?.id === event.pointerId && gesture.touch) {
      const dx = event.clientX - gesture.x;
      const dy = event.clientY - gesture.y;
      if (!gesture.zoomed && Math.abs(dx) > 50 && Math.abs(dx) > Math.abs(dy) * 1.5) {
        navigate(dx < 0 ? 1 : -1);
      } else if (Math.hypot(dx, dy) < 10) {
        const point = {x: event.clientX, y: event.clientY};
        if (lastTap && performance.now() - lastTap.time < 300 && Math.hypot(point.x - lastTap.x, point.y - lastTap.y) < 24) {
          zoom(scale > 1 ? 1 : 2, point);
          lastTap = null;
        } else lastTap = {...point, time: performance.now()};
      }
    }
    if (!pointers.size) {
      gesture = null;
      pinch = null;
      if (multiple || canceled) lastTap = null;
      multiple = false;
      frame.classList.remove('album-panning');
    }
  }
  frame.addEventListener('pointerup', event => finish(event));
  frame.addEventListener('pointercancel', event => finish(event, true));
  frame.addEventListener('lostpointercapture', event => finish(event, true));
  frame.addEventListener('dblclick', event => {
    if (enabled() && performance.now() - lastTouch > 500 && !event.target.closest('button') && !image.hidden) {
      event.preventDefault();
      zoom(scale > 1 ? 1 : 2, {x: event.clientX, y: event.clientY});
    }
  });
  frame.addEventListener('wheel', event => {
    if (!enabled() || image.hidden || !image.naturalWidth) return;
    event.preventDefault();
    zoom(scale * Math.exp(-event.deltaY * .002), {x: event.clientX, y: event.clientY});
  }, {passive: false});
  window.addEventListener('blur', clear, {signal: events.signal});
  const resize = new ResizeObserver(draw);
  resize.observe(frame);
  return {
    get scale() { return scale; },
    zoom,
    refresh: draw,
    reset() { clear(); scale = 1; x = y = 0; draw(); },
    pan(dx, dy) { x += dx; y += dy; draw(); },
    destroy() { clear(); events.abort(); resize.disconnect(); }
  };
}
