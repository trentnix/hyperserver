// Applications own error presentation. Native constraints still block invalid saves.
export function markField(control, invalid, description = '') {
  const controls = control.type === 'radio' ? [...control.form.elements].filter(input => input.name === control.name) :
    [control, control.matches('[data-image-value]') ? control.closest('[data-image-picker]') : null].filter(Boolean);
  for (const input of controls) {
    const descriptions = new Set((input.getAttribute('aria-describedby') || '').split(/\s+/).filter(Boolean));
    const previous = input.dataset.errorDescription;
    descriptions.delete(previous);
    if (invalid) {
      input.setAttribute('aria-invalid', 'true');
      input.dataset.errorDescription = description;
      if (description) descriptions.add(description);
    } else {
      input.removeAttribute('aria-invalid');
      input.classList.remove('input-error');
      delete input.dataset.errorDescription;
      const message = document.getElementById(previous);
      if (message?.hasAttribute('data-field-error')) message.remove();
    }
    if (descriptions.size) input.setAttribute('aria-describedby', [...descriptions].join(' '));
    else input.removeAttribute('aria-describedby');
  }
  control.dispatchEvent(new CustomEvent('blog:field-validation', {bubbles: true}));
}

const invalidForms = new WeakSet();
document.addEventListener('invalid', event => {
  const input = event.target;
  const form = input.form;
  if (!form || !input.id) return;
  event.preventDefault();
  const id = input.id + '-error';
  let message = document.getElementById(id);
  if (!message) {
    message = document.createElement('p');
    message.id = id;
    message.className = 'field-error';
    message.dataset.fieldError = input.name;
    (input.closest('.post-time-field') || input).after(message);
  }
  const required = {title: 'Enter a title.', name: 'Enter a name.', email: 'Enter your email.', password: 'Enter your password.', passwordMatch: 'Confirm your password.', publishDate: 'Choose a publication date.', publishTime: 'Enter a publication time.', image: 'Choose an image.'};
  message.textContent = input.validity.valueMissing ? required[input.name] || input.validationMessage : input.validationMessage;
  markField(input, true, id);
  if (!invalidForms.has(form)) {
    invalidForms.add(form);
    queueMicrotask(() => {
      invalidForms.delete(form);
      if (form.isConnected) form.querySelector('input:invalid, textarea:invalid, select:invalid')?.focus();
    });
  }
}, true);

function initialize(root) {
  for (const message of root.querySelectorAll('[data-error-fields], [data-field-error]')) {
    const form = message.closest('form');
    if (!form) continue;
    const summary = message.closest('.form-error');
    if (summary) {
      for (const control of form.querySelectorAll('[name][aria-invalid]')) markField(control, false);
    }
    if (!message.textContent.trim()) continue;
    const description = summary || message;
    const fields = (message.dataset.errorFields ?? message.dataset.fieldError).split(' ');
    for (const control of form.querySelectorAll('[name]')) {
      if (fields.includes(control.name)) {
        if (!description.id) description.id = control.id + '-error';
        markField(control, true, description.id);
        // A server error can refer to an image URL inside a closed disclosure.
        const details = control.closest('details');
        if (details) details.open = true;
      }
    }
  }
}

for (const name of ['input', 'change']) {
  document.addEventListener(name, event => {
    if (event.target.form && event.target.name) markField(event.target, false);
  });
}
document.addEventListener('htmx:afterSwap', event => initialize(event.target));
initialize(document);
