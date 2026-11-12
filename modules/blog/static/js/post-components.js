const editors = new WeakMap();

// One controller owns component placement and removal. Each component owns its
// fields and supplies a snapshot so Undo also restores its unsaved local state.
export function registerComponent(section, callbacks) {
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

  function render() {
    const busy = Boolean(form.dataset.uploading);
    for (const [type, section] of sections) {
      const index = order.indexOf(type);
      section.hidden = index < 0;
      root.querySelector(`[data-component-add="${type}"]`).hidden = index >= 0;
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
      if (section.nextElementSibling !== next) root.insertBefore(section, next);
      next = section;
    }
    picker.hidden = order.length === sections.size;
    field.value = JSON.stringify(order);
    updateRequired();
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
    form.elements.content.required = ![...sections.values()].some(section => section.querySelector('textarea[name]').value.trim());
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
      const copy = removed.get(type);
      if (undo && copy) {
        entry.restore(copy.value);
        order.splice(Math.min(copy.index, order.length), 0, type);
      } else {
        order.push(type);
      }
      removed.delete(type);
      picker.open = false;
      changed(type);
      entry.focus();
    } else if (remove && order.includes(type)) {
      removed.set(type, {index: order.indexOf(type), value: entry.snapshot()});
      entry.clear();
      order = order.filter(current => current !== type);
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
  });
  form.addEventListener('blog:uploads-finished', render);
  sync();
  return {register(type, callbacks) { entries.set(type, callbacks); }};
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
