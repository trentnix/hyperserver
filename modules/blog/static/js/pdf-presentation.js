const active = new Map();
let library;

function pdfLibrary() {
  if (!library) {
    library = import('../lib/pdfjs/build/pdf.min.mjs').then(pdf => {
      pdf.GlobalWorkerOptions.workerSrc = new URL('../lib/pdfjs/build/pdf.worker.min.mjs', import.meta.url).href;
      return pdf;
    }).catch(error => { library = undefined; throw error; });
  }
  return library;
}

const visibility = typeof IntersectionObserver === 'function' ? new IntersectionObserver(entries => {
  for (const entry of entries) {
    if (!entry.isIntersecting) continue;
    visibility.unobserve(entry.target);
    load(active.get(entry.target));
  }
}, {rootMargin: '200px'}) : null;

const sizes = new ResizeObserver(entries => {
  for (const entry of entries) {
    const state = active.get(entry.target);
    if (!state?.document || state.failed) continue;
    const size = slideSize(state.slide);
    if (size !== state.size) { state.size = size; state.dirty = true; render(state); }
  }
});

function slideSize(slide) {
  return `${slide.clientWidth}:${slide.clientHeight}`;
}

function controls(state) {
  state.previous.hidden = state.next.hidden = state.page.hidden = Boolean(state.failed);
  state.toolbar.classList.toggle('pdf-presentation-failed', Boolean(state.failed));
  state.previous.disabled = !state.document || state.wanted <= 1;
  state.next.disabled = !state.document || state.wanted >= state.document.numPages;
  const fullscreen = document.fullscreenElement === state.root;
  state.fullscreen.hidden = !fullscreen && (!state.document || !document.fullscreenEnabled);
  state.fullscreen.setAttribute('aria-label', fullscreen ? 'Exit fullscreen' : 'Enter fullscreen');
}

function releaseDocument(state) {
  state.renderTask?.cancel();
  state.renderTask = null;
  state.loadingTask?.destroy().catch(() => {});
  state.loadingTask = null;
  state.document = null;
  state.displayed = null;
}

async function failure(state, message) {
  if (state.disposed || state.failed) return;
  const hadFocus = state.root.contains(document.activeElement);
  state.failed = true;
  releaseDocument(state);
  if (document.fullscreenElement === state.root) {
    try { await document.exitFullscreen(); }
    catch { /* Keep the fullscreen exit control available if the browser refuses. */ }
    if (state.disposed) return;
  }
  state.slide.hidden = true;
  const canvas = state.slide.querySelector('canvas');
  canvas.width = canvas.height = 0;
  state.text.textContent = '';
  state.status.textContent = message || 'Unable to load the presentation. You can use the PDF viewer below or Open PDF.';
  state.retry.hidden = false;
  controls(state);
  const frame = document.createElement('iframe');
  frame.src = state.root.dataset.pdfUrl;
  frame.title = state.root.closest('.pdf-preview').querySelector('h3').textContent;
  frame.className = 'pdf-preview-frame';
  frame.loading = 'lazy';
  frame.dataset.pdfFallback = '';
  state.root.append(frame);
  if (hadFocus && (document.activeElement === document.body || state.root.contains(document.activeElement))) {
    state.retry.focus({preventScroll: true});
  }
}

function setDisplayedPage(state, page) {
  if (state.displayed?.page === page) return;
  state.displayed?.page.cleanup();
  const displayed = {page};
  state.displayed = displayed;
  state.text.textContent = '';
  // The canvas can appear before text extraction finishes. Ignore text from a page we left.
  page.getTextContent().then(text => {
    if (state.displayed !== displayed) return;
    state.text.textContent = text.items.map(item => item.str + (item.hasEOL ? '\n' : ' ')).join('');
  }).catch(() => {});
}

async function render(state) {
  if (state.rendering || !state.document || state.disposed || state.failed) return;
  state.rendering = true;
  state.root.setAttribute('aria-busy', 'true');
  try {
    while (state.dirty && !state.disposed && !state.failed) {
      state.dirty = false;
      const number = state.wanted;
      const page = await state.document.getPage(number);
      if (state.disposed || state.failed) break;
      if (number !== state.wanted) continue;
      const original = page.getViewport({scale: 1});
      state.slide.classList.toggle('pdf-slide-portrait', original.height > original.width);
      state.size = slideSize(state.slide);
      const scale = Math.min(state.slide.clientWidth / original.width, state.slide.clientHeight / original.height);
      if (!(scale > 0)) break;
      const viewport = page.getViewport({scale});
      // Bound the backing canvas on ultrawide and high-density screens.
      const density = Math.min(devicePixelRatio || 1, 2, Math.sqrt(8_000_000 / (viewport.width * viewport.height)));
      const canvas = document.createElement('canvas');
      canvas.setAttribute('aria-hidden', 'true');
      canvas.width = Math.max(1, Math.floor(viewport.width * density));
      canvas.height = Math.max(1, Math.floor(viewport.height * density));
      canvas.style.width = viewport.width + 'px';
      canvas.style.height = viewport.height + 'px';
      state.renderTask = page.render({canvas, viewport, transform: [density, 0, 0, density, 0, 0]});
      await state.renderTask.promise;
      state.renderTask = null;
      if (state.disposed || state.failed) break;
      if (number !== state.wanted) {
        if (state.displayed?.page !== page) page.cleanup();
        continue;
      }
      state.slide.querySelector('canvas').replaceWith(canvas);
      state.page.textContent = `Page ${number} of ${state.document.numPages}`;
      state.root.dataset.pdfPage = String(number);
      state.status.textContent = '';
      controls(state);
      setDisplayedPage(state, page);
    }
  } catch {
    await failure(state);
  } finally {
    state.rendering = false;
    if (!state.disposed) state.root.removeAttribute('aria-busy');
  }
}

async function load(state) {
  if (!state || state.disposed || state.failed || state.loadingTask) return;
  try {
    const pdf = await pdfLibrary();
    if (state.disposed) return;
    const assets = new URL('../lib/pdfjs/', import.meta.url).href;
    state.loadingTask = pdf.getDocument({
      url: state.root.dataset.pdfUrl,
      cMapUrl: assets + 'cmaps/', cMapPacked: true,
      standardFontDataUrl: assets + 'standard_fonts/',
      wasmUrl: assets + 'wasm/', useWasm: false,
      disableWorkerRendering: true,
      canvasMaxAreaInBytes: 32 * 1024 * 1024
    });
    state.loadingTask.onPassword = () => failure(state, 'This PDF needs a password. Use the PDF viewer below or Open PDF.');
    state.document = await state.loadingTask.promise;
    if (state.disposed || state.failed) return;
    state.wanted = Math.max(1, Math.min(state.document.numPages, Number(state.root.dataset.pdfPage) || 1));
    controls(state);
    state.dirty = true;
    await render(state);
  } catch {
    await failure(state);
  }
}

function dispose(state) {
  if (state.disposed) return;
  state.disposed = true;
  visibility?.unobserve(state.root);
  sizes.unobserve(state.root);
  state.listeners.abort();
  releaseDocument(state);
  active.delete(state.root);
  state.root.querySelector('[data-pdf-fallback]')?.remove();
  const canvas = state.slide.querySelector('canvas');
  canvas.width = canvas.height = 0;
  state.slide.hidden = true;
  state.toolbar.hidden = true;
  state.status.textContent = '';
  state.text.textContent = '';
  state.root.removeAttribute('aria-busy');
}

function initialize(root) {
  for (const section of root.querySelectorAll('[data-pdf-presentation]')) {
    if (active.has(section)) continue;
    const state = {
      root: section, slide: section.querySelector('[data-pdf-slide]'),
      toolbar: section.querySelector('.pdf-presentation-controls'),
      previous: section.querySelector('[data-pdf-previous]'), next: section.querySelector('[data-pdf-next]'),
      fullscreen: section.querySelector('[data-pdf-fullscreen]'), retry: section.querySelector('[data-pdf-retry]'),
      page: section.querySelector('[data-pdf-page]'), text: section.querySelector('[data-pdf-text]'),
      status: section.querySelector('[data-pdf-status]'),
      listeners: new AbortController(), wanted: 1
    };
    active.set(section, state);
    state.slide.hidden = state.toolbar.hidden = false;
    state.retry.hidden = true;
    state.page.textContent = 'Loading PDF…';
    controls(state);
    function move(number) {
      if (!state.document || state.failed) return;
      const wanted = Math.max(1, Math.min(state.document.numPages, number));
      if (wanted === state.wanted) return;
      state.wanted = wanted;
      state.dirty = true;
      controls(state);
      if (document.activeElement?.disabled) section.focus({preventScroll: true});
      render(state);
    }
    const options = {signal: state.listeners.signal};
    state.previous.addEventListener('click', () => move(state.wanted - 1), options);
    state.next.addEventListener('click', () => move(state.wanted + 1), options);
    section.addEventListener('keydown', event => {
      if (!state.document || state.failed || event.target.closest('button, a, input, textarea, select') || event.altKey || event.ctrlKey || event.metaKey) return;
      const number = {ArrowLeft: state.wanted - 1, ArrowRight: state.wanted + 1, PageUp: state.wanted - 1, PageDown: state.wanted + 1, Home: 1, End: state.document?.numPages}[event.key];
      if (!Number.isFinite(number)) return;
      event.preventDefault(); move(number);
    }, options);
    state.fullscreen.addEventListener('click', async () => {
      try {
        if (document.fullscreenElement === section) await document.exitFullscreen();
        else await section.requestFullscreen();
      } catch { state.status.textContent = 'Fullscreen is unavailable. You can continue here or Open PDF.'; }
    }, options);
    document.addEventListener('fullscreenchange', () => controls(state), options);
    state.retry.addEventListener('click', () => {
      dispose(state); initialize(section.parentElement);
      section.focus({preventScroll: true});
    }, options);
    sizes.observe(section);
    if (visibility) visibility.observe(section);
    else load(state);
  }
}

document.addEventListener('htmx:before:cleanup', event => {
  for (const state of active.values()) {
    if (event.target === state.root || event.target.contains?.(state.root)) dispose(state);
  }
});
// HTMX only emits cleanup events for elements it initialized itself.
document.addEventListener('htmx:after:swap', () => {
  for (const state of active.values()) if (!state.root.isConnected) dispose(state);
});
window.addEventListener('pagehide', () => { for (const state of active.values()) dispose(state); });
window.addEventListener('pageshow', () => initialize(document));
document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
