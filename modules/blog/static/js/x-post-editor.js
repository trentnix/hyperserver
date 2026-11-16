import {registerComponent, jsonComponentValue} from './post-components.js';
import {markField} from './forms.js';

const initialized = new WeakSet();

function initialize(root) {
  for (const section of root.querySelectorAll('[data-x-post-editor]')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('[name="xPost"]');
    const label = section.querySelector('[data-x-post-source-label]');
    const controls = section.querySelector('[data-x-post-controls]');
    const fields = [...controls.querySelectorAll('[data-x-post-field]')];
    const error = section.querySelector('[data-x-post-error]');

    function read() {
      let value;
      try {
        value = source.value.trim() ? JSON.parse(source.value) : {};
        if (!value || typeof value !== 'object' || Array.isArray(value) || !Object.keys(value).every(key => ['url', 'description'].includes(key) && typeof value[key] === 'string')) throw new Error();
      } catch {
        source.hidden = label.hidden = false;
        controls.hidden = controls.disabled = true;
        error.hidden = false;
        error.textContent = 'The X post source is invalid. Correct it before saving.';
        return;
      }
      for (const field of fields) field.value = value[field.dataset.xPostField] || '';
      source.hidden = label.hidden = true;
      controls.hidden = false;
      controls.disabled = section.hidden;
      error.hidden = true;
      error.textContent = '';
    }

    for (const field of fields) {
      field.addEventListener('input', () => {
        markField(field, false);
        const value = Object.fromEntries(fields.map(input => [input.dataset.xPostField, input.value]));
        source.value = Object.values(value).some(text => text.trim()) ? JSON.stringify(value) : '';
        source.dispatchEvent(new Event('input', {bubbles: true}));
      });
    }
    source.addEventListener('change', read);
    registerComponent(section, {
      normalize: value => jsonComponentValue(value, {strings: ['description']}),
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
