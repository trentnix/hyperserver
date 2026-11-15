import {registerComponent} from './post-components.js';
import {markField} from './forms.js';
import {runFormUpload, setMediaStatus, showUploadFailure} from './media.js';

const initialized = new WeakSet();
const fetchedMessage = 'Video details fetched.';

function validSource(value) {
  return value && typeof value === 'object' && !Array.isArray(value) && Object.keys(value).every(key => ['url', 'title', 'caption'].includes(key) && typeof value[key] === 'string');
}

function initialize(root) {
  for (const section of root.querySelectorAll('[data-video-editor]')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('[name="video"]');
    const controls = section.querySelector('[data-video-controls]');
    const label = section.querySelector('[data-video-source-label]');
    const status = section.querySelector('[data-video-status]');
    const fields = [...controls.querySelectorAll('[data-video-field]')];
    const [url, title, caption] = fields;
    const fetchButton = section.querySelector('[data-video-fetch]');
    const form = source.form;
    const lifetime = new AbortController();
    let request;

    function serialize() {
      const value = Object.fromEntries(fields.map(input => [input.dataset.videoField, input.value]));
      source.value = Object.values(value).some(text => text.trim()) ? JSON.stringify(value) : '';
      source.dispatchEvent(new Event('input', {bubbles: true}));
    }

    function read() {
      request?.abort();
      let value;
      try {
        value = source.value.trim() ? JSON.parse(source.value) : {};
        if (!validSource(value)) throw new Error();
      } catch {
        source.hidden = label.hidden = false;
        controls.hidden = controls.disabled = true;
        setMediaStatus(status, 'The video source is invalid. Correct it before saving.', true);
        return;
      }
      for (const field of fields) field.value = value[field.dataset.videoField] || '';
      fetchButton.textContent = title.value || caption.value ? 'Refresh details' : 'Fetch details';
      source.hidden = label.hidden = true;
      controls.hidden = false;
      controls.disabled = section.hidden;
      setMediaStatus(status, '');
    }

    async function fetchDetails() {
      if (!section.isConnected || form.dataset.uploading || !url.reportValidity()) return;
      const before = source.value;
      const destination = url.value.trim();
      request?.abort();
      request = new AbortController();
      const active = request;
      await runFormUpload(form, async () => {
        setMediaStatus(status, 'Fetching video details…');
        try {
          const response = await fetch('/post/video-details', {method: 'POST', body: new URLSearchParams({url: destination}), signal: active.signal, redirect: 'error'});
          if (!(response.headers.get('Content-Type') || '').includes('application/json')) throw new Error('Unable to fetch video details. You can enter them yourself.');
          const result = await response.json();
          if (!response.ok) throw Object.assign(new Error(result.error || 'Unable to fetch video details.'), {code: result.code});
          if (!validSource(result.video) || result.video.url !== destination) throw new Error('Unable to read the video details. You can enter them yourself.');
          if (!section.isConnected || active.signal.aborted) return;
          if (source.value !== before) { setMediaStatus(status, 'Your details changed while the request loaded. Fetch again to replace them.'); return; }
          if (result.video.title) title.value = result.video.title;
          if (result.video.caption) caption.value = result.video.caption;
          serialize();
          read();
          setMediaStatus(status, result.warning || fetchedMessage);
        } catch (error) {
          if (section.isConnected && !active.signal.aborted) {
            showUploadFailure(status, error, fetchDetails);
            const retry = status.querySelector('[data-upload-retry]');
            if (retry) retry.textContent = 'Retry details';
          }
        }
      });
    }

    fetchButton.addEventListener('click', () => {
      if (form.dataset.uploading || !url.reportValidity()) return;
      if (title.value.trim() || caption.value.trim()) {
        window.blogConfirm({title: 'Replace video details?', message: 'Fetching again replaces the title and caption when the source provides them.', action: 'Replace details', cancel: 'Keep editing'}, fetchDetails);
      } else fetchDetails();
    });
    for (const field of fields) {
      field.addEventListener('input', () => {
        if (status.textContent === fetchedMessage) setMediaStatus(status, '');
        markField(field, false);
        serialize();
        fetchButton.textContent = title.value || caption.value ? 'Refresh details' : 'Fetch details';
      });
    }
    source.addEventListener('change', read);
    registerComponent(section, {
      snapshot: () => source.value,
      clear: () => { source.value = ''; read(); },
      restore: copy => { source.value = copy; read(); },
      focus: () => (source.hidden ? fields[0] : source).focus()
    });
    document.addEventListener('htmx:before:cleanup', event => {
      if (event.target.contains?.(form)) { request?.abort(); lifetime.abort(); }
    }, {signal: lifetime.signal});
    read();
  }
}

document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
