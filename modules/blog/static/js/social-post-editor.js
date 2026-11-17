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
    }

    for (const field of fields) {
      field.addEventListener('input', () => {
        markField(field, false);
        const value = Object.fromEntries(fields.map(input => [input.dataset.socialPostField, input.value]));
        source.value = Object.values(value).some(text => text.trim()) ? JSON.stringify(value) : '';
        source.dispatchEvent(new Event('input', {bubbles: true}));
        preview();
      });
    }
    source.addEventListener('change', read);
    source.form.addEventListener('blog:uploads-finished', preview);
    registerComponent(section, {
      normalize: value => jsonComponentValue(value, {strings: ['description', 'imageID']}),
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
