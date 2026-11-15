import {registerComponent} from './post-components.js';
import {markField} from './forms.js';
import {setMediaStatus} from './media.js';

const initialized = new WeakSet();

function initialize(root) {
  for (const section of root.querySelectorAll('[data-video-editor]')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('[name="video"]');
    const controls = section.querySelector('[data-video-controls]');
    const label = section.querySelector('[data-video-source-label]');
    const status = section.querySelector('[data-video-status]');
    const fields = [...controls.querySelectorAll('[data-video-field]')];

    function read() {
      let value;
      try {
        value = source.value.trim() ? JSON.parse(source.value) : {};
        if (!value || typeof value !== 'object' || Array.isArray(value) || Object.keys(value).some(key => !['url', 'title', 'caption'].includes(key) || typeof value[key] !== 'string')) throw new Error();
      } catch {
        source.hidden = label.hidden = false;
        controls.hidden = controls.disabled = true;
        setMediaStatus(status, 'The video source is invalid. Correct it before saving.', true);
        return;
      }
      for (const field of fields) field.value = value[field.dataset.videoField] || '';
      source.hidden = label.hidden = true;
      controls.hidden = false;
      controls.disabled = section.hidden;
      setMediaStatus(status, '');
    }

    for (const field of fields) {
      field.addEventListener('input', () => {
        markField(field, false);
        const value = Object.fromEntries(fields.map(input => [input.dataset.videoField, input.value]));
        source.value = Object.values(value).some(text => text.trim()) ? JSON.stringify(value) : '';
        source.dispatchEvent(new Event('input', {bubbles: true}));
      });
    }
    source.addEventListener('change', read);
    registerComponent(section, {
      snapshot: () => source.value,
      clear: () => { source.value = ''; read(); },
      restore: copy => { source.value = copy; read(); },
      focus: () => (source.hidden ? fields[0] : source).focus()
    });
    read();
  }
}

document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
