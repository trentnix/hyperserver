import {registerComponent, jsonComponentValue} from './post-components.js';
import './media.js';
import {markField} from './forms.js';

const initialized = new WeakSet();
const limits = {beforeID: 69, afterID: 69, beforeLabel: 80, afterLabel: 80, beforeAlt: 500, afterAlt: 500, title: 240, caption: 1000};
const imageID = /^[a-f0-9]{64}\.(png|jpeg|gif)$/;

function initialize(root) {
  for (const section of root.querySelectorAll('[data-comparison-editor]')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('[name="comparison"]');
    const label = section.querySelector('[data-comparison-source-label]');
    const controls = section.querySelector('[data-comparison-controls]');
    const fields = [...controls.querySelectorAll('[data-comparison-field]')];
    const error = section.querySelector('[data-comparison-error]');

    function previews() {
      for (const picker of controls.querySelectorAll('[data-image-picker]')) {
        const id = picker.querySelector('[data-image-value]').value;
        const image = picker.querySelector('img');
        image.hidden = !id;
        if (id) image.src = '/images/' + id + '?variant=thumbnail';
        else image.removeAttribute('src');
        picker.querySelector('.image-empty').hidden = Boolean(id);
        picker.querySelector('[data-image-reset]').disabled = Boolean(source.form.dataset.uploading) || !id;
      }
    }

    function serialize() {
      const value = Object.fromEntries(fields.map(field => [field.dataset.comparisonField, field.value]));
      source.value = Object.values(value).some(text => text.trim()) ? JSON.stringify(value) : '';
      source.dispatchEvent(new Event('input', {bubbles: true}));
      previews();
    }

    function read() {
      try {
        const value = source.value.trim() ? JSON.parse(source.value) : {};
        if (!value || typeof value !== 'object' || Array.isArray(value) || !Object.entries(value).every(([key, text]) => Object.hasOwn(limits, key) && typeof text === 'string' && [...text].length <= limits[key] && (!key.endsWith('ID') || !text || imageID.test(text)))) throw new Error();
        for (const field of fields) field.value = value[field.dataset.comparisonField] || '';
      } catch {
        source.hidden = label.hidden = false;
        controls.hidden = controls.disabled = true;
        error.hidden = false;
        error.textContent = 'The image comparison source is invalid. Correct it before saving.';
        return;
      }
      source.hidden = label.hidden = true;
      controls.hidden = false;
      controls.disabled = section.hidden;
      error.hidden = true;
      error.textContent = '';
      previews();
    }

    for (const field of fields) field.addEventListener('input', () => { markField(field, false); serialize(); });
    source.form.addEventListener('blog:uploads-finished', previews);
    source.addEventListener('change', read);
    registerComponent(section, {
      normalize: value => jsonComponentValue(value, {strings: ['beforeAlt', 'afterAlt', 'beforeLabel', 'afterLabel', 'title', 'caption']}),
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
