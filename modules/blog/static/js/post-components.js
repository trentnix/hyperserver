import {getMarkdownEditor} from './markdown-editor.js';

const editors = new WeakMap();

const comparisons = new WeakMap();

export function componentValue(source, value) {
  return comparisons.get(source)?.(value) ?? value;
}

// Each editor declares its optional defaults. Canonical JSON comparison itself
// stays shared, including nested entries in file lists.
export function jsonComponentValue(value, {strings = [], flags = [], empty = '{}'} = {}) {
  const optionalDefault = (key, value) => value === '' && strings.includes(key) || value === false && flags.includes(key);
  const normalize = value => {
    if (Array.isArray(value)) return value.map(normalize);
    if (!value || typeof value !== 'object') return value;
    return Object.fromEntries(Object.keys(value).sort().filter(key => !optionalDefault(key, value[key])).map(key => [key, normalize(value[key])]));
  };
  try { return JSON.stringify(normalize(JSON.parse(value || empty))); }
  catch { return value; }
}

// One controller owns component placement and removal. Each component owns its
// fields and supplies a snapshot so Undo also restores its unsaved local state.
export function registerComponent(section, callbacks) {
  const source = section.querySelector('textarea[name]');
  comparisons.set(source, callbacks.normalize);
  const root = section.closest('[data-post-components]');
  let editor = editors.get(root);
  if (!editor) {
    editor = createEditor(root);
    editors.set(root, editor);
  }
  editor.register(section.dataset.postComponent, callbacks);
}

function createEditor(root) {
  const form = root.closest('form');
  const field = form.elements.componentOrder;
  const picker = root.querySelector('[data-component-picker]');
  const sections = new Map([...root.querySelectorAll('[data-post-component]')].map(section => [section.dataset.postComponent, section]));
  const entries = new Map();
  const removed = new Map();
  let order = [];
  let baseline;

  function updateError(section) {
    const source = section.querySelector('textarea[name]');
    let message = section.querySelector('[data-component-error]');
    const invalid = source.getAttribute('aria-invalid') === 'true';
    if (!message && invalid) {
      message = document.createElement('p');
      message.className = 'field-error';
      message.dataset.componentError = '';
      message.id = source.id + '-component-error';
      section.querySelector('.album-editor-heading').after(message);
    }
    if (message) {
      message.hidden = !invalid;
      message.textContent = invalid ? document.getElementById(source.dataset.errorDescription)?.textContent.trim() || '' : '';
      if (invalid) section.setAttribute('aria-describedby', message.id);
      else section.removeAttribute('aria-describedby');
    }
    if (invalid) queueMicrotask(() => {
      if (!section.isConnected || section.hidden || source.getAttribute('aria-invalid') !== 'true') return;
      entries.get(section.dataset.postComponent)?.focus();
      section.scrollIntoView({block: 'nearest'});
    });
  }

  function updateUnsaved() {
    if (baseline === undefined) return;
    const savedOrder = baseline ? JSON.parse(baseline.componentOrder || '[]') || [] : [];
    for (const [type, section] of sections) {
      const source = section.querySelector('textarea[name]');
      const changed = !baseline || order.indexOf(type) !== savedOrder.indexOf(type) ||
        componentValue(source, source.value) !== componentValue(source, baseline[source.name] || '');
      section.querySelector('[data-component-unsaved]').hidden = section.hidden || !changed;
    }
  }

  const choice = type => sections.get(type).dataset.componentChoice || type;

  function removeComponent(type) {
    removed.set(type, {index: order.indexOf(type), value: entries.get(type).snapshot()});
    entries.get(type).clear();
    order = order.filter(current => current !== type);
  }

  function render() {
    const busy = Boolean(form.dataset.uploading);
    for (const [type, section] of sections) {
      const index = order.indexOf(type);
      section.hidden = index < 0;
      section.querySelectorAll('[data-component-fields]').forEach(fields => { fields.disabled = index < 0 || fields.hidden; });
      const add = root.querySelector(`[data-component-add="${type}"]`);
      if (add) add.hidden = order.some(kind => choice(kind) === type);
      root.querySelector(`[data-component-recovery="${type}"]`).hidden = index >= 0 || !removed.has(type);
      section.querySelector('[data-component-remove]').hidden = false;
      section.querySelector('[data-component-remove]').disabled = busy;
      section.querySelector('[data-component-order-controls]').hidden = order.length < 2;
      section.querySelector('[data-component-move="up"]').disabled = busy || index <= 0;
      section.querySelector('[data-component-move="down"]').disabled = busy || index >= order.length - 1;
    }
    let next = picker;
    const placement = [...order, ...[...sections.keys()].filter(type => !order.includes(type))];
    for (const type of placement.reverse()) {
      const section = sections.get(type);
      if (section.nextElementSibling !== next) {
        // Keep loaded frames intact. Older browsers must rebuild live previews
        // around insertBefore, which destroys an iframe's browsing context.
        if (root.moveBefore) root.moveBefore(section, next);
        else {
          entries.get(type)?.beforeMove?.();
          root.insertBefore(section, next);
          entries.get(type)?.afterMove?.();
        }
      }
      next = section;
    }
    picker.hidden = ![...picker.querySelectorAll('[data-component-add]')].some(button => !button.hidden);
    field.value = JSON.stringify(order);
    updateRequired();
    updateUnsaved();
  }

  function sync() {
    try { order = JSON.parse(field.value || '[]'); } catch { order = []; }
    if (!Array.isArray(order) || order.some(type => !sections.has(type)) || new Set(order).size !== order.length) order = [];
    for (const [type, section] of sections) {
      if (section.querySelector('textarea[name]').value.trim() && !order.includes(type)) order.push(type);
    }
    render();
  }

  function changed(type) {
    render();
    field.dispatchEvent(new Event('input', {bubbles: true}));
    if (type) sections.get(type).querySelector('textarea[name]').dispatchEvent(new Event('input', {bubbles: true}));
  }

  function updateRequired() {
    const required = order.length === 0 && ![...sections.values()].some(section => section.querySelector('textarea[name]').value.trim());
    const content = form.elements.content;
    const editor = getMarkdownEditor(content);
    if (editor) editor.setRequired(required);
    else content.required = required;
  }

  root.addEventListener('click', event => {
    if (form.dataset.uploading) return;
    const add = event.target.closest('[data-component-add]');
    const undo = event.target.closest('[data-component-undo]');
    const remove = event.target.closest('[data-component-remove]');
    const move = event.target.closest('[data-component-move]');
    const type = add?.dataset.componentAdd || undo?.dataset.componentUndo || remove?.dataset.componentRemove || move?.closest('[data-post-component]').dataset.postComponent;
    const entry = entries.get(type);
    if (!entry) return;
    if ((add || undo) && !order.includes(type)) {
      const conflicts = order.filter(kind => choice(kind) === choice(type));
      if (add && conflicts.length) return;
      const copy = removed.get(type);
      if (undo && copy) {
        // Undo restores the old choice. Keep its replacement in Undo as well,
        // so switching between stored aliases cannot discard either version.
        for (const kind of conflicts) removeComponent(kind);
        entry.restore(copy.value);
        order.splice(Math.min(copy.index, order.length), 0, type);
      } else {
        order.push(type);
      }
      removed.delete(type);
      picker.open = false;
      changed(type);
      for (const kind of conflicts) sections.get(kind).querySelector('textarea[name]').dispatchEvent(new Event('input', {bubbles: true}));
      entry.focus();
    } else if (remove && order.includes(type)) {
      removeComponent(type);
      changed(type);
      picker.querySelector('summary').focus();
    } else if (move && order.includes(type)) {
      const index = order.indexOf(type);
      const target = index + (move.dataset.componentMove === 'up' ? -1 : 1);
      if (target < 0 || target >= order.length) return;
      [order[index], order[target]] = [order[target], order[index]];
      changed();
      const section = sections.get(type);
      (move.disabled ? section.querySelector('[data-component-move]:not(:disabled)') : move).focus({preventScroll: true});
      section.scrollIntoView({block: 'nearest'});
    }
  });
  root.addEventListener('change', event => {
    if (event.target === field || event.target.matches('[data-post-component] textarea[name]')) {
      removed.clear();
      sync();
    }
  });
  root.addEventListener('input', event => {
    const section = event.target.closest('[data-post-component]');
    if (section && event.target.matches('textarea[name]') && event.target.value.trim() && !order.includes(section.dataset.postComponent)) {
      order.push(section.dataset.postComponent);
      render();
    }
    updateRequired();
    updateUnsaved();
  });
  // Draft recovery and history keep the server baseline. Only a confirmed save
  // replaces it, including when an HTMX response leaves this editor in place.
  form.addEventListener('blog:editor-baseline', event => {
    baseline = event.detail;
    updateUnsaved();
  });
  form.addEventListener('blog:uploads-finished', render);
  form.addEventListener('blog:field-validation', event => {
    const section = event.target.closest('[data-post-component]');
    if (section && event.target.matches('textarea[name]')) updateError(section);
  });
  sync();
  return {register(type, callbacks) {
    entries.set(type, callbacks);
    updateError(sections.get(type));
  }};
}

function dismissPickers(event) {
  for (const picker of document.querySelectorAll('[data-component-picker][open]')) {
    if (!picker.contains(event.target)) picker.open = false;
  }
}
document.addEventListener('click', dismissPickers);
document.addEventListener('focusin', dismissPickers);
document.addEventListener('keydown', event => {
  const picker = event.target.closest('[data-component-picker][open]');
  if (event.key !== 'Escape' || !picker) return;
  event.preventDefault();
  picker.open = false;
  picker.querySelector('summary').focus();
});
