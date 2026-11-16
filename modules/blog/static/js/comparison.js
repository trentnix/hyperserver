const initialized = new WeakSet();

function initialize(root) {
  for (const comparison of root.querySelectorAll('[data-image-comparison]')) {
    if (initialized.has(comparison)) continue;
    const slider = comparison.querySelector('input[type="range"]');
    if (!slider) continue; // Feeds and historical previews keep only the images.
    initialized.add(comparison);
    const stage = comparison.querySelector('.comparison-stage');
    const beforeLabel = comparison.querySelector('.comparison-before .comparison-label').textContent;
    const afterLabel = comparison.querySelector('.comparison-after .comparison-label').textContent;
    function update() {
      const percent = Number(slider.value);
      stage.style.setProperty('--comparison-position', percent + '%');
      slider.setAttribute('aria-valuetext', `${percent}% ${beforeLabel}, ${100 - percent}% ${afterLabel}`);
    }
    slider.addEventListener('input', update);
    let pointer;
    function move(event) {
      const bounds = stage.getBoundingClientRect();
      slider.value = String(Math.round(Math.max(0, Math.min(100, (event.clientX - bounds.left) / bounds.width * 100))));
      update();
    }
    stage.addEventListener('pointerdown', event => {
      if (!event.isPrimary || event.button !== 0) return;
      if (event.pointerType !== 'touch') event.preventDefault();
      pointer = event.pointerId;
      stage.setPointerCapture(pointer);
      slider.focus({preventScroll: true});
      move(event);
    });
    stage.addEventListener('pointermove', event => { if (event.pointerId === pointer) move(event); });
    stage.addEventListener('lostpointercapture', () => { pointer = undefined; });
    comparison.classList.add('comparison-ready');
    comparison.querySelector('.comparison-divider').hidden = false;
    update();
  }
}

document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
