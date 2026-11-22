import {initializeComponentPanels, jsonComponentValue, registerComponent} from './post-components.js';
import {markField} from './forms.js';
import {refreshCompactMarkdownEditors} from './markdown-editor.js';

const initialized = new WeakSet();

function initialize(root) {
  for (const section of root.querySelectorAll('.markdown-component-editor')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('textarea[name]');
    const label = source.labels[0];
    const fields = section.querySelector('[data-component-fields]');
    const inputs = new Map([...fields.querySelectorAll('[data-markdown-value]')].map(input => [input.dataset.markdownValue, input]));
    const title = inputs.get('title');
    const body = inputs.get('body');
    const optional = [...inputs].filter(([, input]) => !input.required).map(([key]) => key);

    function validate() {
      title.setCustomValidity(title.required && !title.value.trim() ? 'Enter a title.' : '');
      body.setCustomValidity(body.value.trim() ? '' : body.dataset.emptyMessage);
    }

    function read() {
      let value;
      try { value = source.value.trim() ? JSON.parse(source.value) : {}; }
      catch { /* Allow correcting malformed source directly. */ }
      const valid = Boolean(value && typeof value === 'object' && !Array.isArray(value) &&
        Object.entries(value).every(([key, text]) => {
          const input = inputs.get(key);
          return input && typeof text === 'string' &&
            (!input.options || [...input.options].some(option => option.value === text));
        }));
      source.hidden = label.hidden = valid;
      fields.hidden = !valid;
      fields.disabled = !valid || section.hidden;
      if (valid) {
        for (const [key, input] of inputs) input.value = value[key] ?? '';
        validate();
      }
      refreshCompactMarkdownEditors(section);
    }

    fields.addEventListener('input', event => {
      if (inputs.get(event.target.dataset.markdownValue) !== event.target) return;
      if (event.target.hasAttribute('aria-invalid')) markField(event.target, false);
      validate();
      const values = [...inputs].map(([key, input]) => [key, input.value]);
      source.value = values.some(([, text]) => text.trim())
        ? JSON.stringify(Object.fromEntries(values.filter(([key, text]) => text !== '' || !optional.includes(key))))
        : '';
      source.dispatchEvent(new Event('input', {bubbles: true}));
    });
    source.addEventListener('change', read);
    registerComponent(section, {
      label: () => title.value,
      normalize: value => jsonComponentValue(value, {strings: optional}),
      snapshot: () => source.value,
      clear: () => { source.value = ''; read(); },
      restore: value => { source.value = value; read(); },
      focus: () => (source.hidden ? title : source).focus()
    });
    read();
  }
}

initializeComponentPanels(initialize);
