import {componentElement, initializeComponentPanels, registerComponent, jsonComponentValue} from './post-components.js';
import {focusField, markField} from './forms.js';
import {refreshCompactMarkdownEditors} from './markdown-editor.js';

const initialized = new WeakSet();

function initialize(root) {
  for (const section of root.querySelectorAll('[data-embed-editor]')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('textarea[name]');
    const label = section.querySelector('[data-embed-source-label]');
    const fields = section.querySelector('[data-component-fields]');
    const url = componentElement(section, 'embed-url');
    const title = componentElement(section, 'embed-title');
    const description = componentElement(section, 'embed-description');
    const button = fields.querySelector('[data-embed-preview-button]');
    const preview = componentElement(section, 'embed-editor-preview');
    const status = preview.querySelector('[data-embed-preview-status]');
    const link = preview.querySelector('[data-embed-preview-link]');
    const frame = preview.querySelector('iframe');
    const inputs = {url, title, description};
    const fieldErrors = Object.fromEntries(Object.entries(inputs).map(([key, input]) => ['embed.' + key, input]));
    const errorID = source.id + '-component-error';
    let errorField;
    let pending;

    function hidePreview() {
      pending?.abort();
      pending = null;
      preview.hidden = true;
      frame.parentElement.hidden = true;
      frame.removeAttribute('src');
      link.hidden = true;
      link.removeAttribute('href');
      status.textContent = '';
      button.setAttribute('aria-expanded', 'false');
      button.textContent = 'Preview embed';
    }

    function updatePreviewTitle() {
      frame.title = title.value.trim() || 'Embed preview';
      const frameDocument = frame.contentDocument;
      const inner = frameDocument?.querySelector('iframe');
      if (inner) {
        frameDocument.title = frame.title;
        inner.title = frame.title;
      }
    }

    function validateFields() {
      url.setCustomValidity(url.value && !/^https:\/\//i.test(url.value) ? 'Enter an HTTPS embed URL.' : '');
      title.setCustomValidity(title.value && !title.value.trim() ? 'Enter an embed title.' : '');
    }

    function syncError() {
      if (errorField?.dataset.errorDescription === errorID) markField(errorField, false);
      errorField = null;
      if (!source.hidden || source.getAttribute('aria-invalid') !== 'true') return;
      const summary = document.getElementById(source.dataset.errorDescription);
      const names = summary?.querySelector('[data-error-fields]')?.dataset.errorFields.split(' ') ?? [];
      errorField = fieldErrors[names.find(name => Object.hasOwn(fieldErrors, name))];
      if (errorField) markField(errorField, true, errorID);
    }

    function read() {
      hidePreview();
      let valid = false;
      try {
        const value = source.value.trim() ? JSON.parse(source.value) : {};
        if (value && typeof value === 'object' && !Array.isArray(value) &&
            Object.entries(value).every(([key, text]) => Object.hasOwn(inputs, key) && typeof text === 'string')) {
          for (const [key, input] of Object.entries(inputs)) input.value = value[key] ?? '';
          valid = true;
        }
      } catch {
        // Leave unfamiliar source visible so the author can correct it.
      }
      source.hidden = label.hidden = valid;
      fields.hidden = !valid;
      fields.disabled = !valid || section.hidden;
      refreshCompactMarkdownEditors(section);
      validateFields();
      syncError();
    }

    fields.addEventListener('input', event => {
      if (!Object.values(inputs).includes(event.target)) return;
      if (event.target.hasAttribute('aria-invalid')) markField(event.target, false);
      validateFields();
      const value = {url: url.value, title: title.value, description: description.value.trim() ? description.value : ''};
      source.value = value.url.trim() || value.title.trim() || value.description ? JSON.stringify(value) : '';
      source.dispatchEvent(new Event('input', {bubbles: true}));
      if (event.target === url) hidePreview();
      else if (event.target === title) updatePreviewTitle();
    });
    const previewURL = (destination, name) => '/embed/frame?' + new URLSearchParams({title: name, url: destination});
    button.addEventListener('click', async () => {
      if (!preview.hidden) {
        hidePreview();
        return;
      }
      if (!url.reportValidity() || !title.reportValidity()) return;
      const name = title.value.trim();
      const request = new AbortController();
      pending = request;
      preview.hidden = false;
      button.setAttribute('aria-expanded', 'true');
      button.textContent = 'Hide preview';
      status.textContent = 'Checking embed…';
      try {
        const response = await fetch(previewURL(url.value.trim(), name), {
          headers: {Accept: 'application/json'}, signal: request.signal
        });
        const result = await response.json();
        if (pending !== request || !section.isConnected) return;
        if (!response.ok) {
          const field = Object.hasOwn(fieldErrors, result.field) ? fieldErrors[result.field] : null;
          if (field && result.message) {
            hidePreview();
            field.setCustomValidity(result.message);
            field.reportValidity();
          } else {
            status.textContent = result.message || 'Unable to check the embed. Try again.';
          }
          return;
        }
        link.href = result.url;
        link.hidden = false;
        status.textContent = result.allowed ? '' : 'This site is outside the approved origins. Readers will see a link.';
        if (result.allowed) {
          updatePreviewTitle();
          frame.src = previewURL(result.url, name);
          frame.parentElement.hidden = false;
        }
      } catch {
        if (pending === request && section.isConnected) status.textContent = 'Unable to check the embed. Try again.';
      } finally {
        if (pending === request) pending = null;
      }
    });
    frame.addEventListener('load', updatePreviewTitle);
    source.addEventListener('change', read);
    source.addEventListener('blog:field-validation', syncError);
    registerComponent(section, {
      label: () => title.value,
      normalize: value => jsonComponentValue(value, {strings: ['description']}),
      snapshot: () => source.value,
      clear: () => { source.value = ''; read(); },
      restore: value => { source.value = value; read(); },
      focus: () => focusField(source.hidden ? errorField || url : source)
    });
    read();
  }
}

initializeComponentPanels(initialize);
