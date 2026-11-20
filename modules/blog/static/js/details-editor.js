import {componentElement, initializeComponentPanels, jsonComponentValue, registerComponent} from './post-components.js';
import {markField} from './forms.js';

const initialized = new WeakSet();

function initialize(root) {
  for (const section of root.querySelectorAll('[data-details-editor]')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('textarea[name]');
    const label = section.querySelector('[data-details-source-label]');
    const fields = section.querySelector('[data-component-fields]');
    const title = componentElement(section, 'details-title');
    const body = componentElement(section, 'details-body');

    function validate() {
      title.setCustomValidity(title.value.trim() ? '' : 'Enter a title.');
      body.setCustomValidity(body.value.trim() ? '' : 'Enter details.');
    }

    function read() {
      let value;
      try { value = source.value.trim() ? JSON.parse(source.value) : {}; }
      catch { /* Allow correcting malformed source directly. */ }
      const valid = Boolean(value && typeof value === 'object' && !Array.isArray(value) &&
        Object.entries(value).every(([key, text]) => (key === 'title' || key === 'body') && typeof text === 'string'));
      source.hidden = label.hidden = valid;
      fields.hidden = !valid;
      fields.disabled = !valid || section.hidden;
      if (!valid) return;
      title.value = value.title ?? '';
      body.value = value.body ?? '';
      validate();
    }

    fields.addEventListener('input', event => {
      if (event.target.hasAttribute('aria-invalid')) markField(event.target, false);
      validate();
      source.value = title.value.trim() || body.value.trim() ? JSON.stringify({title: title.value, body: body.value}) : '';
      source.dispatchEvent(new Event('input', {bubbles: true}));
    });
    source.addEventListener('change', read);
    registerComponent(section, {
      label: () => title.value,
      normalize: jsonComponentValue,
      snapshot: () => source.value,
      clear: () => { source.value = ''; read(); },
      restore: value => { source.value = value; read(); },
      focus: () => (source.hidden ? title : source).focus()
    });
    read();
  }
}

initializeComponentPanels(initialize);
