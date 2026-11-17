import {registerComponent, jsonComponentValue} from './post-components.js';
import {markField} from './forms.js';
import './media.js';

const initialized = new WeakSet();
const imageID = /^[a-f0-9]{64}\.(png|jpeg|gif)$/;

function initialize(root) {
  for (const section of root.querySelectorAll('[data-social-post-editor]')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('textarea[name]');
    const name = section.dataset.socialPostName;
    const label = section.querySelector('[data-social-post-source-label]');
    const controls = section.querySelector('[data-social-post-controls]');
    const fields = [...controls.querySelectorAll('[data-social-post-field]')];
    const error = section.querySelector('[data-social-post-error]');
    const picker = controls.querySelector('[data-image-picker]');
    const url = fields.find(field => field.dataset.socialPostField === 'url');
    const description = fields.find(field => field.dataset.socialPostField === 'description');
    const livePreview = controls.querySelector('[data-social-post-live-preview]');
    const previewContent = livePreview.querySelector('[data-social-post-preview-content]');
    const previewStatus = livePreview.querySelector('[data-social-post-preview-status]');
    const retry = livePreview.querySelector('[data-social-post-preview-retry]');
    const lifetime = new AbortController();
    let request, previewKey = '';

    function clearPreview() {
      request?.abort();
      request = undefined;
      previewKey = '';
      // Release provider observers, timers, and requests before replacing a card.
      previewContent.dispatchEvent(new CustomEvent('htmx:before:cleanup', {bubbles: true}));
      previewContent.replaceChildren();
      livePreview.hidden = retry.hidden = true;
      previewStatus.textContent = '';
    }

    function updateDescription() {
      const card = previewContent.querySelector('.social-post-component, .x-post-component');
      if (!card) return;
      let text = card.querySelector('.social-post-description, .x-post-description');
      if (!description.value) { text?.remove(); return; }
      if (!text) {
        text = document.createElement('p');
        text.className = card.classList.contains('x-post-component') ? 'x-post-description' : 'social-post-description';
        card.prepend(text);
      }
      text.textContent = description.value;
    }

    async function loadPreview(force = false) {
      if (!section.isConnected || section.hidden || controls.disabled || !url.value.trim() || !url.validity.valid) return;
      const value = {url: url.value.trim(), imageID: picker.querySelector('[data-image-value]').value};
      const key = JSON.stringify(value);
      if (!force && key === previewKey) return;
      const retrying = document.activeElement === retry;
      clearPreview();
      previewKey = key;
      request = new AbortController();
      const active = request;
      livePreview.hidden = false;
      previewStatus.textContent = 'Loading preview…';
      if (retrying) livePreview.focus({preventScroll: true});
      function failed(message) {
        previewStatus.textContent = message;
        retry.hidden = false;
      }
      const timeout = setTimeout(() => {
        active.abort();
        if (request === active && section.isConnected) failed('The preview timed out. Try again.');
      }, 10000);
      try {
        const response = await fetch('/post/social-preview', {
          method: 'POST', body: new URLSearchParams({socialPost: JSON.stringify(value)}), signal: active.signal, redirect: 'error'
        });
        const html = await response.text();
        if (active.signal.aborted || !section.isConnected) return;
        if (!response.ok) throw new Error(response.status === 422 ? html.trim() : 'Unable to load the preview. Try again.');
        if (!(response.headers.get('Content-Type') || '').includes('text/html')) throw new Error('Unable to load the preview. Try again.');
        // Only the same-origin, validated component renderer supplies this HTML.
        previewContent.innerHTML = html;
        updateDescription();
        previewStatus.textContent = '';
        htmx.process(previewContent);
      } catch (error) {
        if (!active.signal.aborted && section.isConnected) failed(error.message);
      } finally { clearTimeout(timeout); }
    }

    function preview() {
      const id = picker.querySelector('[data-image-value]').value;
      const image = picker.querySelector('[data-image-preview]');
      picker.querySelector('[data-social-post-preview]').hidden = !id;
      image.hidden = !id;
      if (id) image.src = '/images/' + id + '?variant=thumbnail';
      else image.removeAttribute('src');
      picker.querySelector('[data-image-reset]').disabled = Boolean(source.form.dataset.uploading) || !id;
      picker.querySelector('[data-image-reset]').hidden = !id;
    }

    function read() {
      clearPreview();
      let value;
      try {
        value = source.value.trim() ? JSON.parse(source.value) : {};
        if (!value || typeof value !== 'object' || Array.isArray(value) || !Object.keys(value).every(key => ['url', 'description', 'imageID'].includes(key) && typeof value[key] === 'string') || value.imageID && !imageID.test(value.imageID)) throw new Error();
      } catch {
        source.hidden = label.hidden = false;
        controls.hidden = controls.disabled = true;
        error.hidden = false;
        error.textContent = 'The ' + name + ' post source is invalid. Correct it before saving.';
        return;
      }
      for (const field of fields) field.value = value[field.dataset.socialPostField] || '';
      source.hidden = label.hidden = true;
      controls.hidden = false;
      controls.disabled = section.hidden;
      error.hidden = true;
      error.textContent = '';
      preview();
      // Recovery can reveal a component after its source change has bubbled.
      queueMicrotask(() => loadPreview());
    }

    for (const field of fields) {
      field.addEventListener('input', () => {
        markField(field, false);
        const value = Object.fromEntries(fields.map(input => [input.dataset.socialPostField, input.value]));
        source.value = Object.values(value).some(text => text.trim()) ? JSON.stringify(value) : '';
        source.dispatchEvent(new Event('input', {bubbles: true}));
        preview();
        if (field === url) clearPreview();
        else if (field === description) updateDescription();
        else loadPreview();
      });
    }
    source.addEventListener('change', read);
    source.form.addEventListener('blog:uploads-finished', preview);
    url.addEventListener('blur', () => loadPreview());
    retry.addEventListener('click', () => loadPreview(true));
    window.addEventListener('pagehide', clearPreview, {signal: lifetime.signal});
    window.addEventListener('pageshow', () => loadPreview(), {signal: lifetime.signal});
    document.addEventListener('htmx:before:cleanup', event => {
      if (event.target.contains?.(section)) { request?.abort(); lifetime.abort(); }
    }, {signal: lifetime.signal});
    registerComponent(section, {
      normalize: value => jsonComponentValue(value, {strings: ['description', 'imageID']}),
      snapshot: () => source.value,
      clear: () => { source.value = ''; read(); },
      restore: copy => { source.value = copy; read(); },
      beforeMove: clearPreview,
      afterMove: () => loadPreview(),
      focus: () => (source.hidden ? fields[0] : source).focus()
    });
    read();
  }
}

document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
