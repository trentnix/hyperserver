import {initializeComponentPanels, jsonComponentValue, registerComponent} from './post-components.js';

const initialized = new WeakSet();

function validDestination(value) {
  if (value.startsWith('/')) {
    const path = value.split(/[?#]/, 1)[0];
    return !/^\/(?:\/|%2f)/i.test(path) && !/\\|%5c/i.test(path);
  }
  if (!/^https?:\/\/[^/\\?#]/i.test(value)) return false;
  try {
    const destination = new URL(value);
    return !destination.username && !destination.password;
  } catch {
    return false;
  }
}

function initialize(root) {
  for (const section of root.querySelectorAll('.action-link-editor')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('textarea[name]');
    const sourceLabel = source.labels[0];
    const fields = section.querySelector('[data-component-fields]');
    const label = fields.querySelector('[data-action-link-label]');
    const url = fields.querySelector('[data-action-link-url]');

    function validate({label: text, url: destination}) {
      label.setCustomValidity(text ? '' : 'Enter a button label.');
      url.setCustomValidity(!destination ? 'Enter a destination URL.' : validDestination(destination) ? '' :
        'Enter a full URL starting with https:// or http://, or a site path like /page/projects.');
    }

    function read() {
      let value;
      try { value = source.value.trim() ? JSON.parse(source.value) : {}; }
      catch { /* Allow correcting malformed source directly. */ }
      const valid = Boolean(value && typeof value === 'object' && !Array.isArray(value) &&
        Object.entries(value).every(([key, text]) => (key === 'label' || key === 'url') && typeof text === 'string'));
      source.hidden = sourceLabel.hidden = valid;
      fields.hidden = !valid;
      fields.disabled = !valid || section.hidden;
      if (valid) {
        label.value = value.label ?? '';
        url.value = value.url ?? '';
        validate({label: label.value.trim(), url: url.value.trim()});
      }
    }

    fields.addEventListener('input', () => {
      const value = {label: label.value.trim(), url: url.value.trim()};
      validate(value);
      source.value = value.label || value.url ? JSON.stringify(value) : '';
      source.dispatchEvent(new Event('input', {bubbles: true}));
    });
    source.addEventListener('change', read);
    registerComponent(section, {
      label: () => source.hidden ? label.value.trim() : '',
      normalize: jsonComponentValue,
      snapshot: () => source.value,
      clear: () => { source.value = ''; read(); },
      restore: value => { source.value = value; read(); },
      focus: () => (source.hidden ? label : source).focus()
    });
    read();
  }
}

initializeComponentPanels(initialize);
