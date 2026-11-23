import {componentRow, hasComponentMarkers, nameComponentRow} from './component-placement.js';
import {focusField, markField, showFieldValidationError} from './forms.js';
import {destroyMarkdownEditors, getMarkdownEditor, refreshCompactMarkdownEditors} from './markdown-editor.js';

const editors = new WeakMap();

const comparisons = new WeakMap();
const instanceID = /^([a-z-]+):([0-9a-f]{32})$/;
const sectionID = section => section.dataset.componentId || section.dataset.postComponent;

export function componentElement(section, id) {
  return section.querySelector('#' + componentDOMID(section, id));
}

export function componentDOMID(section, id) {
  const suffix = section.dataset.componentId?.split(':')[1];
  return suffix ? id + '-' + suffix : id;
}

export function initializeComponentPanels(initialize) {
  document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
  document.addEventListener('blog:component-panel', event => initialize(event.target));
  initialize(document);
}

export function restoreComponentInstances(form, fields) {
  const root = form.querySelector('[data-post-components]');
  if (root) editors.get(root)?.restoreInstances(fields);
}

export function clearPostComponents(form) {
  const root = form.querySelector('[data-post-components]');
  editors.get(root)?.clear();
}

export function componentValue(source, value) {
  return comparisons.get(source)?.(value) ?? value;
}

export function registerSourceComparison(source, normalize) {
  comparisons.set(source, normalize);
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
  registerSourceComparison(source, callbacks.normalize);
  const root = section.closest('[data-post-components]');
  let editor = editors.get(root);
  if (!editor) {
    editor = prepareEditor(root);
  }
  editor.register(sectionID(section), callbacks);
}

function prepareEditor(root) {
  if (!editors.has(root)) editors.set(root, createEditor(root));
  return editors.get(root);
}

function prepareEditors(root) {
  for (const editor of root.querySelectorAll('[data-post-components]')) prepareEditor(editor);
}
document.addEventListener('htmx:after:process', event => prepareEditors(event.target.parentElement || event.target));

function createEditor(root) {
  const form = root.closest('form');
  const field = form.elements.componentOrder;
  const content = form.elements.content;
  const picker = form.querySelector('[data-component-picker]');
  const pickerSummary = picker.querySelector('summary');
  const status = form.querySelector('[data-component-placement-status]');
  const saveStatus = form.querySelector('[data-component-save-status]');
  const fallback = form.querySelector('[data-component-fallback]');
  const dialog = root.querySelector('[data-component-dialog-template]').content.firstElementChild.cloneNode(true);
  const dialogBody = dialog.querySelector('.component-dialog-body');
  const done = dialog.querySelector('[data-component-done]');
  const endRows = document.createElement('div');
  endRows.className = 'component-end-list';
  const panels = root.querySelectorAll('[data-post-component]');
  const templates = new Map([...panels].map(section => [section.dataset.postComponent, section.cloneNode(true)]));
  const removalStatus = root.querySelector('[data-component-removal-status]');
  const removalTemplate = root.querySelector('[data-component-removal-template]');
  const instances = new Map();
  let removed = new Map();
  let order = [];
  let anchors = new Map();
  let currentLayout;
  let baseline, savedLayout;
  let editing, moving, refreshTimer;
  let pendingAction;

  const editor = () => getMarkdownEditor(content);
  const placement = () => editor()?.placement;
  const canChangePlacement = () => Boolean(placement()) || !hasComponentMarkers(content.value);
  // Removed rows below the writing keep their place in the sequence until
  // Undo or a new editor replaces that sequence.
  const activeOrder = () => order.filter(id => !removed.has(id));
  const isActive = id => order.includes(id) && !removed.has(id);
  const trailing = () => activeOrder().filter(id => !anchors.has(id));
  const rowFor = type => anchors.has(type) ? placement()?.row(type) : instances.get(type)?.row;

  field.after(endRows, dialog);
  for (const section of panels) addInstance(section.dataset.postComponent, section);

  function addInstance(id, section) {
    const name = section.querySelector('h2').firstChild.textContent.trim();
    const source = section.querySelector('textarea[name]');
    const row = componentRow(id, name);
    row.classList.add('component-end-row');
    row.hidden = true;
    endRows.append(row);
    dialogBody.append(section);
    instances.set(id, {section, source, name, row});
    return source;
  }

  function mount(id, value, notify = true) {
    const match = instanceID.exec(id);
    const kind = match?.[1];
    const template = templates.get(kind);
    if (!template) return;
    const section = template.cloneNode(true);
    section.dataset.componentId = id;
    const suffix = '-' + match[2];
    const ids = new Map([...section.querySelectorAll('[id]')].map(node => [node.id, node.id + suffix]));
    for (const node of [section, ...section.querySelectorAll('*')]) {
      if (node.id) node.id = ids.get(node.id);
      for (const attribute of ['for', 'aria-labelledby', 'aria-describedby', 'aria-controls']) {
        if (!node.hasAttribute(attribute)) continue;
        const names = node.getAttribute(attribute).split(/\s+/);
        node.setAttribute(attribute, names.map(name => ids.get(name) || name).join(' '));
      }
    }
    const source = addInstance(id, section);
    source.name = id;
    source.value = value;
    source.removeAttribute('aria-invalid');
    delete source.dataset.errorDescription;
    section.hidden = true;
    if (notify) dialogBody.dispatchEvent(new Event('blog:component-panel', {bubbles: true}));
    return source;
  }
  const savedInstances = root.querySelectorAll('[data-component-instance]');
  for (const field of savedInstances) {
    const source = mount(field.name, field.value, false);
    if (source && field.getAttribute('aria-invalid') === 'true') {
      source.setAttribute('aria-invalid', 'true');
      source.dataset.errorDescription = field.dataset.errorDescription || '';
    }
    field.remove();
  }
  if (savedInstances.length) queueMicrotask(() => dialogBody.dispatchEvent(new Event('blog:component-panel', {bubbles: true})));

  function focusRow(type, selector = '[data-component-edit]') {
    placement()?.scrollToMarker(type);
    rowFor(type)?.querySelector(selector)?.focus();
  }

  function clearSettingsFeedback(session, section) {
    session.validation?.abort();
    done.removeAttribute('aria-busy');
    done.removeAttribute('aria-disabled');
    section.querySelector('[data-component-settings-error]')?.remove();
  }

  function closeSettings({discard = false} = {}) {
    if (!editing || form.dataset.uploading) return;
    const {type, snapshot, addition} = editing;
    const {section, source, callbacks} = instances.get(type);
    // Let a focused field serialize its final input before ending the session.
    if (dialog.contains(document.activeElement)) document.activeElement.blur();
    destroyMarkdownEditors(section);
    clearSettingsFeedback(editing, section);
    editing = undefined;
    cancelInactiveAction();
    dialog.close();
    section.classList.remove('component-panel-active');
    const previous = source.value;
    if (addition) {
      delete section.dataset.componentPending;
      if (discard) {
        callbacks.restore(snapshot);
        render();
      } else {
        const {layout, line} = addition;
        commit(layout, [type], write => {
          order.push(type);
          if (line !== null) write.place(type, line);
        }, {beforeSettings: [{type, value: snapshot}]});
      }
    } else if (discard) {
      callbacks.restore(snapshot);
      if (source.value !== previous) source.dispatchEvent(new Event('input', {bubbles: true}));
    }
    if (discard) {
      for (const control of section.querySelectorAll('[data-error-description]')) {
        const message = document.getElementById(control.dataset.errorDescription);
        if (message?.hasAttribute('data-field-error') && (addition || control.validity?.valid)) markField(control, false);
      }
    }
    if (isActive(type)) focusRow(type);
    else pickerSummary.focus();
  }

  async function finishSettings() {
    const session = editing;
    if (!session || session.validation || form.dataset.uploading) return;
    const {section, source} = instances.get(session.type);
    if (document.activeElement !== done && dialog.contains(document.activeElement)) document.activeElement.blur();
    section.querySelector('[data-component-settings-error]')?.remove();
    const invalid = [...section.querySelectorAll('input, select, textarea')].filter(control => control.willValidate && !control.validity.valid);
    if (invalid.length) {
      for (const control of invalid) {
        if (control.id) showFieldValidationError(control);
        else control.reportValidity();
      }
      focusField(invalid[0]);
      return;
    }

    function showError(text, focus = true) {
      const message = document.createElement('p');
      message.className = 'field-error';
      message.dataset.componentSettingsError = '';
      message.setAttribute('role', 'alert');
      message.tabIndex = -1;
      message.textContent = text;
      section.querySelector('.album-editor-heading').after(message);
      if (focus) message.focus();
    }

    const controller = new AbortController();
    const value = source.value;
    session.validation = controller;
    done.setAttribute('aria-busy', 'true');
    done.setAttribute('aria-disabled', 'true');
    const timeout = setTimeout(() => controller.abort(), 10000);
    let valid = false;
    let error;
    try {
      const response = await fetch('/post/component-settings', {
        method: 'POST', body: new URLSearchParams({id: session.type, source: value}), signal: controller.signal, redirect: 'error'
      });
      valid = response.status === 204;
      if (!valid) error = (await response.json()).error;
    } catch {
      error = 'Could not check the component. Try again.';
    } finally {
      clearTimeout(timeout);
    }
    if (editing !== session || !form.isConnected) return;
    session.validation = undefined;
    clearSettingsFeedback(session, section);
    // An input method can commit text on blur after the request was sent.
    if (valid && source.value === value && !form.dataset.uploading && section.contains(document.activeElement)) {
      document.activeElement.blur();
    }
    if (editing !== session || !form.isConnected) return;
    if (source.value !== value || form.dataset.uploading) {
      showError('The component changed while checking. Choose Done again.', !section.contains(document.activeElement));
    } else if (valid) {
      closeSettings();
    } else {
      showError(error || 'Could not check the component. Try again.');
    }
  }

  function editSettings(type, addition) {
    if (form.dataset.uploading || editing?.addition && editing.type !== type) return;
    if (!addition && editing?.type !== type && !isActive(type)) return;
    editor()?.showSource();
    if (editing?.type !== type) {
      if (editing) {
        const previous = instances.get(editing.type).section;
        destroyMarkdownEditors(previous);
        clearSettingsFeedback(editing, previous);
        previous.classList.remove('component-panel-active');
      }
      const {section, callbacks} = instances.get(type);
      editing = {type, snapshot: callbacks.snapshot(), addition};
      if (addition) {
        section.dataset.componentPending = '';
        render();
        callbacks.add?.();
      }
      section.classList.add('component-panel-active');
      dialog.setAttribute('aria-labelledby', section.getAttribute('aria-labelledby'));
      dialogBody.scrollTo(0, 0);
      if (!dialog.open) dialog.showModal();
      refreshCompactMarkdownEditors(section);
    }
    instances.get(type).callbacks?.focus();
  }

  dialog.querySelector('[data-component-cancel]').addEventListener('click', () => closeSettings({discard: true}));
  done.addEventListener('click', finishSettings);
  dialog.addEventListener('input', event => {
    const control = event.target;
    control.closest('[data-post-component]')?.querySelector('[data-component-settings-error]')?.remove();
    if (control.validity?.valid && control.dataset.errorDescription === control.id + '-error') markField(control, false);
  });
  dialog.addEventListener('cancel', event => {
    event.preventDefault();
    closeSettings({discard: true});
  });
  dialog.addEventListener('keydown', event => {
    // Component fields share the editor form. Enter in a text field must not
    // implicitly save while the dialog is open.
    if (event.key === 'Enter' && !event.isComposing && event.target.matches('input:not([type]), input:is([type=text], [type=url], [type=search], [type=number])')) {
      event.preventDefault();
    }
  });

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
      if (section.isConnected && !section.hidden && source.getAttribute('aria-invalid') === 'true') editSettings(sectionID(section));
    });
  }

  function updateUnsaved() {
    if (baseline === undefined) return;
    const savedOrder = baseline ? JSON.parse(baseline.componentOrder || '[]') || [] : [];
    const savedTrailing = savedOrder.filter(type => savedLayout?.markers[type] === undefined);
    const unplaced = trailing();
    for (const [type, {section, source}] of instances) {
      const line = anchors.get(type);
      const oldLine = savedLayout?.markers[type];
      const moved = savedLayout && (line !== undefined || oldLine !== undefined
        ? currentLayout?.blocks.indexOf(line) !== savedLayout.blocks.indexOf(oldLine)
        : unplaced.indexOf(type) !== savedTrailing.indexOf(type));
      const changed = section.hasAttribute('data-component-pending') || !baseline || moved || isActive(type) !== savedOrder.includes(type) ||
        componentValue(source, source.value) !== componentValue(source, baseline[source.name] || '');
      section.querySelector('[data-component-unsaved]').hidden = section.hidden || !changed;
    }
  }

  function forgetRemoval(type) {
    removed.get(type)?.position?.clear();
    removed.delete(type);
  }

  function showRemovalNotice(id) {
    const {label} = removed.get(id);
    const notice = removalTemplate.content.firstElementChild.cloneNode(true);
    const name = notice.querySelector('[data-removed-component]');
    name.textContent = name.title = label;
    const undo = notice.querySelector('button');
    undo.dataset.componentUndo = id;
    undo.setAttribute('aria-label', `Undo removal of ${label}`);
    removalStatus.replaceChildren(notice);
  }

  removalStatus.addEventListener('animationend', event => {
    if (event.animationName === 'notification-fade') event.target.remove();
  });

  function componentDetail(id) {
    const {source, callbacks} = instances.get(id);
    return source.hidden ? callbacks?.label?.()?.trim() || '' : '';
  }

  function updateNames() {
    const labels = new Map();
    const counts = new Map();
    const mounted = [...instances.keys()];
    const active = activeOrder();
    for (const id of [...active, ...removed.keys()]) {
      const detail = removed.get(id)?.detail ?? componentDetail(id);
      const kind = instances.get(id).name;
      const label = detail ? `${kind} · ${detail}` : kind;
      labels.set(id, {kind, detail, label});
      counts.set(label, (counts.get(label) || 0) + 1);
    }

    for (const id of active) {
      const {kind, detail, label} = labels.get(id);
      let name = label;
      const separator = id.indexOf(':');
      if (separator !== -1 && (!detail || counts.get(label) > 1)) {
        let end = separator + 7;
        while (mounted.some(other => other !== id && other.startsWith(id.slice(0, end)))) end++;
        const reference = '#' + id.slice(separator + 1, end);
        name = detail ? `${kind} ${reference} · ${detail}` : `${kind} ${reference}`;
      }
      const row = rowFor(id);
      if (row && nameComponentRow(row, name) && !row.classList.contains('component-end-row')) placement()?.measureRow(id);
    }
  }

  function removeComponent(type, layout, write, fromRow) {
    const line = layout.markers[type];
    const removals = [...removed];
    // Keep inline removals ordered when deleting a line merges two gaps.
    const next = removals.findIndex(([, item]) => item.position?.find()?.line > line);
    const {callbacks, name} = instances.get(type);
    const detail = componentDetail(type);
    const label = rowFor(type)?.getAttribute('aria-label') ?? (detail ? `${name} · ${detail}` : name);
    const edit = write.remove(type);
    const item = {detail, label, fromRow, value: callbacks.snapshot(), position: edit?.position, separator: edit?.separator};
    removals.splice(next < 0 ? removals.length : next, 0, [type, item]);
    removed = new Map(removals);
    if (line !== undefined) order = order.filter(id => id !== type);
    callbacks.clear();
    if (moving?.type === type) moving = undefined;
  }

  function restoreComponent(type, item, layout, write) {
    const destination = removal => {
      const line = removal.position?.find()?.line;
      return line === undefined ? null : layout.drops.find(drop => drop >= line) ?? layout.end;
    };
    const alreadyPlaced = layout.markers[type] !== undefined;
    const line = destination(item);
    if (!order.includes(type)) order.push(type);
    const {section, callbacks} = instances.get(type);
    section.hidden = false;
    callbacks.restore(item.value);

    if (line !== null && !alreadyPlaced) {
      let after = false;
      const atLine = [];
      for (const [id, other] of removed) {
        if (id === type) after = true;
        else if (destination(other) === line) atLine.push({other, after});
      }
      write.place(type, line, item.position.find()?.line === line ? item.separator : undefined);
      // Make room on the correct side of the restored component for each
      // pending removal that shared its destination.
      for (const {other, after} of atLine) {
        other.position.clear();
        other.position = placement().bookmark(line + Number(after));
      }
    }
    forgetRemoval(type);
  }

  function componentState(types, {markers, blocks, separators, end}) {
    return {
      order: [...order], layout: {markers, blocks, separators, end},
      settings: types.map(type => ({type, value: instances.get(type).callbacks.snapshot()})),
      removals: [...removed].map(([type, {position, ...item}]) => [type, {...item, line: position?.find()?.line}])
    };
  }

  function restoreState(state) {
    order = [...state.order];
    for (const type of removed.keys()) forgetRemoval(type);
    for (const [type, {line, ...item}] of state.removals) {
      removed.set(type, {...item, position: line === undefined ? undefined : placement()?.bookmark(line)});
    }
    for (const {type, value} of state.settings) {
      const {section, callbacks} = instances.get(type);
      section.hidden = !isActive(type);
      callbacks.restore(value);
    }
    if (!isActive(moving?.type)) moving = undefined;
  }

  // A placement edit publishes its Markdown and the matching form fields in
  // one operation. A follow-up layout request only refines the next destinations.
  function commit(layout, types, edit, {reorder = false, beforeSettings} = {}) {
    const controller = placement();
    if (!canChangePlacement()) return;
    editor()?.showSource();
    const publish = (next, sort) => {
      content.value = next.source;
      applyLayout(next);
      if (sort) order = [...anchors.keys(), ...order.filter(id => !anchors.has(id))];
      changed(types);
    };
    if (!controller) {
      edit({place() {}, remove() {}});
      publish(layout, reorder);
      return;
    }

    const before = componentState(types, layout);
    if (beforeSettings) before.settings = beforeSettings;
    let after;
    controller.transact(layout, write => {
      edit(write);
      const next = write.layout();
      publish(next, reorder);
      after = componentState(types, next);
    }, direction => {
      if (pendingAction) finishAction(pendingAction);
      const [from, to] = direction === 'before' ? [after, before] : [before, after];
      // Keep field edits made after the original action for the next replay.
      for (const entry of from.settings) entry.value = instances.get(entry.type).callbacks.snapshot();
      const removedID = types.find(id => isActive(id) && to.removals.some(([removedID]) => removedID === id));
      restoreState(to);
      publish({...to.layout, source: editor().getMarkdown()}, false);
      if (removed.has(removedID)) showRemovalNotice(removedID);
    });
  }

  function moveTargets(type, layout) {
    const unplaced = trailing();
    const current = anchors.get(type);
    if (current === undefined) {
      const index = unplaced.indexOf(type);
      const up = unplaced[index - 1];
      return {
        up: up ? {before: up} : layout?.blocks.length ? {line: layout.blocks.at(-1)} : undefined,
        down: unplaced[index + 1] ? {before: unplaced[index + 2] ?? null} : undefined
      };
    }
    if (!layout) return {};
    const index = layout.blocks.indexOf(current);
    const after = layout.blocks[index + 2] ?? (index + 1 < layout.blocks.length ? layout.end : null);
    const before = layout.blocks[index - 1];
    return {
      up: before === undefined ? undefined : {line: before},
      down: after === null ? (index === layout.blocks.length - 1 && unplaced.length ? {before: unplaced[1] ?? null} : undefined) : {line: after}
    };
  }

  function renderMoveControls() {
    const layout = currentLayout?.source === content.value ? currentLayout : null;
    const targets = moving ? moveTargets(moving.type, layout) : {};
    const uploading = Boolean(form.dataset.uploading);
    for (const type of activeOrder()) {
      const row = rowFor(type);
      if (!row) continue;
      const open = type === moving?.type;
      row.querySelector('[data-component-place]').setAttribute('aria-expanded', String(open));
      row.querySelector('[data-component-remove-row]').disabled = uploading;
      const steps = row.querySelector('.component-move-steps');
      if (steps.hidden === open) {
        steps.hidden = !open;
        if (!row.classList.contains('component-end-row')) placement()?.measureRow(row.dataset.componentRow);
      }
      if (!open) continue;
      for (const direction of ['up', 'down']) {
        row.querySelector('[data-component-shift=' + direction + ']').disabled = !targets[direction] || uploading;
      }
    }
  }

  function render() {
    const canPlace = Boolean(placement());
    const canChange = canChangePlacement();
    fallback.hidden = canChange;
    removalStatus.hidden = !canChange;
    if (!canChange) moving = undefined;
    const unplaced = trailing();
    if (editing && !editing.addition && !isActive(editing.type)) closeSettings();
    const undo = removalStatus.querySelector('[data-component-undo]');
    if (undo && !removed.has(undo.dataset.componentUndo)) removalStatus.replaceChildren();
    for (const [type, {section, source, row}] of instances) {
      const pending = section.hasAttribute('data-component-pending');
      const active = isActive(type);
      section.hidden = !active && !pending;
      row.hidden = !unplaced.includes(type);
      const handle = row.querySelector('[data-component-place]');
      handle.hidden = !canChange;
      handle.toggleAttribute('data-component-drag', canPlace);
      handle.title = canPlace ? 'Drag or show move controls' : '';
      row.querySelector('[data-component-remove-row]').hidden = !canChange;
      section.querySelectorAll('[data-component-fields]').forEach(fields => { fields.disabled = section.hidden || fields.hidden; });
      source.disabled = pending || Boolean(section.dataset.componentId) && !active;
      section.querySelector('[data-component-remove]').hidden = pending || !canChange;
      section.querySelector('[data-component-remove]').disabled = Boolean(form.dataset.uploading);
    }
    let next = endRows.firstElementChild;
    for (const type of unplaced) {
      const {row} = instances.get(type);
      if (row !== next) endRows.insertBefore(row, next);
      next = row.nextElementSibling;
    }
    picker.hidden = !canChange || !picker.querySelector('[data-component-add]:not([hidden])');
    const active = activeOrder();
    field.value = JSON.stringify(active);
    const required = root.hasAttribute('data-content-required') && active.length === 0;
    if (editor()) editor().setRequired(required);
    else content.required = required;
    if (currentLayout?.source === content.value) placement()?.refresh(anchors, id => instances.get(id).name);
    updateNames();
    renderMoveControls();
    updateUnsaved();
  }

  function changed(types) {
    render();
    field.dispatchEvent(new Event('input', {bubbles: true}));
    for (const type of types) instances.get(type).source.dispatchEvent(new Event('input', {bubbles: true}));
  }

  // Only apply an answer to the source it describes. Without a placement
  // controller, changes are safe only for writing without inline markers.
  async function readLayout(source = content.value, isCurrent = () => true) {
    const controller = placement();
    if (!controller) return {source, markers: {}, blocks: [], drops: [], end: null};
    try {
      return {...await controller.layout(source), source};
    } catch {
      if (form.isConnected && content.value === source && isCurrent()) status.textContent = 'Could not read component positions. Try again.';
      return null;
    }
  }

  function applyLayout(layout) {
    currentLayout = layout;
    anchors = new Map(Object.entries(layout.markers)
      .filter(([type]) => isActive(type)).sort((a, b) => a[1] - b[1]));
  }

  async function refreshLayout() {
    clearTimeout(refreshTimer);
    const source = content.value;
    const layout = hasComponentMarkers(source) || moving ? await readLayout(source, () => !pendingAction) : {source, markers: {}, blocks: [], drops: [], end: null};
    if (!layout || !form.isConnected || content.value !== source) return;
    applyLayout(layout);
    render();
  }

  function finishAction(action) {
    if (pendingAction !== action) return;
    pendingAction = undefined;
    form.removeAttribute('data-component-action-pending');
    if (status.textContent === action.message) status.textContent = '';
    saveStatus.textContent = '';
  }

  function cancelInactiveAction() {
    if (pendingAction && !pendingAction.valid()) finishAction(pendingAction);
  }

  async function withComponentLayout(message, retry, apply, valid = () => true) {
    cancelInactiveAction();
    if (pendingAction) return;
    const action = {message, retry, valid, source: content.value};
    pendingAction = action;
    form.setAttribute('data-component-action-pending', '');
    const isCurrent = () => pendingAction === action && valid();
    status.textContent = message;
    try {
      const layout = await readLayout(action.source, isCurrent);
      if (!isCurrent() || !form.isConnected) return;
      if (action.source !== content.value || form.dataset.uploading) {
        status.textContent = retry;
        return;
      }
      if (layout) {
        action.applying = true;
        apply(layout);
      }
    } finally {
      finishAction(action);
    }
  }

  function addComponent(type) {
    if (pendingAction) return;
    picker.open = false;
    const cursor = placement()?.cursor();
    return withComponentLayout('Finding a place for the component…',
      'The editor changed while placing the component. Choose Add component again.', layout => {
        const line = (cursor && layout.blocks.find(block => block > cursor.line || block === cursor.line && cursor.ch === 0)) ?? layout.end;
        if (placement() && layout.end === null) {
          status.textContent = 'Close the unfinished code or HTML block before adding a component.';
          pickerSummary.focus();
          return;
        }
        const sameKind = activeOrder().some(id => {
          const {section} = instances.get(id);
          return (section.dataset.componentChoice || section.dataset.postComponent) === type;
        });
        const id = sameKind || removed.has(type) ? type + ':' + crypto.randomUUID().replaceAll('-', '') : type;
        if (id !== type && !mount(id, '')) return;
        editSettings(id, {layout, line});
      });
  }

  function undoRemoval(type) {
    const item = removed.get(type);
    if (!item || isActive(type)) return;
    return withComponentLayout('Restoring the component…', 'The editor changed. Try restoring the component again.', layout => {
      applyLayout(layout);
      commit(layout, [type], write => restoreComponent(type, item, layout, write), {reorder: true});
      if (item.fromRow) focusRow(type);
      else editSettings(type);
    }, () => removed.get(type) === item);
  }

  function requestRemoval(button) {
    const fromRow = button.hasAttribute('data-component-remove-row');
    const type = fromRow ? button.dataset.componentRemoveRow : sectionID(button.closest('[data-post-component]'));
    const session = editing;
    const valid = () => isActive(type) && editing === session && (fromRow ? !session : session?.type === type);
    if (!valid()) return;
    return withComponentLayout('Removing the component…', 'The editor changed. Try removing the component again.', layout => {
      applyLayout(layout);
      const visibleOrder = fromRow ? [...anchors.keys(), ...trailing()] : [];
      const index = visibleOrder.indexOf(type);
      const neighbor = visibleOrder[index + 1] ?? visibleOrder[index - 1];
      commit(layout, [type], write => removeComponent(type, layout, write, fromRow));
      if (neighbor) focusRow(neighbor);
      else pickerSummary.focus();
      showRemovalNotice(type);
    }, valid);
  }

  function commitMove(type, layout, target) {
    commit(layout, [], write => {
      if (target.line === undefined) {
        write.remove(type)?.position.clear();
        order = order.filter(id => id !== type);
        order.splice(target.before === null ? order.length : order.indexOf(target.before), 0, type);
      } else {
        write.place(type, target.line);
      }
    }, {reorder: true});
  }

  function moveComponent(type, direction) {
    const session = moving;
    if (session?.type !== type) return;
    return withComponentLayout('Moving the component…', 'The editor changed. Try moving the component again.', layout => {
      applyLayout(layout);
      const target = moveTargets(type, layout)[direction];
      if (!target) return;
      commitMove(type, layout, target);
      // Keep the other direction and the Move toggle within keyboard reach.
      const available = rowFor(type)?.querySelector('[data-component-shift=' + direction + ']:not(:disabled)');
      focusRow(type, available ? '[data-component-shift=' + direction + ']' : '[data-component-place]');
    }, () => moving === session);
  }

  form.addEventListener('click', async event => {
    if (form.dataset.uploading) return;
    const add = event.target.closest('[data-component-add]');
    const undo = event.target.closest('[data-component-undo]');
    const edit = event.target.closest('[data-component-edit]');
    const move = event.target.closest('[data-component-place]');
    const step = event.target.closest('[data-component-shift]');
    const remove = event.target.closest('[data-component-remove], [data-component-remove-row]');
    if ((add || undo || move || step || remove) && !canChangePlacement()) return;
    if (add) await addComponent(add.dataset.componentAdd);
    else if (undo) await undoRemoval(undo.dataset.componentUndo);
    else if (edit) {
      moving = undefined;
      cancelInactiveAction();
      if (pendingAction) return;
      renderMoveControls();
      editSettings(edit.dataset.componentEdit);
    } else if (move) {
      const type = move.dataset.componentPlace;
      editor()?.showSource();
      moving = moving?.type === type ? undefined : {type};
      cancelInactiveAction();
      const session = moving;
      renderMoveControls();
      if (session) {
        if (!pendingAction) status.textContent = '';
        const isCurrent = () => moving === session;
        const layout = await readLayout(content.value, isCurrent);
        if (!layout || !form.isConnected || layout.source !== content.value || !isCurrent()) return;
        applyLayout(layout);
        render();
        const targets = moveTargets(type, layout);
        if (!pendingAction && !targets.up && !targets.down) status.textContent = 'There is no other position for this component.';
      } else if (!pendingAction) status.textContent = '';
    } else if (step) {
      await moveComponent(step.closest('[data-component-row]').dataset.componentRow, step.dataset.componentShift);
    } else if (remove) await requestRemoval(remove);
  });

  form.addEventListener('keydown', event => {
    const row = event.target.closest('[data-component-row]');
    const noticeID = event.target.dataset.componentUndo;
    if (event.defaultPrevented || !row && !noticeID && event.target !== pickerSummary) return;
    const key = event.key.toLowerCase();
    const history = (event.ctrlKey || event.metaKey) && !event.altKey && (key === 'z' || key === 'y' && event.ctrlKey);
    if (history && placement()) {
      event.preventDefault();
      if (pendingAction && key === 'z' && !event.shiftKey) {
        finishAction(pendingAction);
        return;
      }
      placement().undoOrRedo(key === 'y' || event.shiftKey);
      const id = row?.dataset.componentRow ?? noticeID;
      if (isActive(id)) focusRow(id, row && !event.target.matches('[data-component-edit]') ? '[data-component-place]' : '[data-component-edit]');
      else pickerSummary.focus();
      return;
    }
    if (event.key !== 'Escape' || !row || row.dataset.componentRow !== moving?.type) return;
    event.preventDefault();
    moving = undefined;
    cancelInactiveAction();
    if (!pendingAction) status.textContent = '';
    row.querySelector('[data-component-place]').focus();
    renderMoveControls();
  });
  form.addEventListener('blog:component-drop', event => {
    const {type, destination, layout} = event.detail;
    if (form.dataset.uploading || pendingAction || !isActive(type) || layout.source !== content.value) return;
    if (typeof destination === 'number') {
      commitMove(type, layout, {line: destination});
    } else {
      if (destination !== null && (destination === type || !isActive(destination) || layout.markers[destination] !== undefined)) return;
      commitMove(type, layout, {before: destination});
    }
    focusRow(type, '[data-component-place]');
  });
  form.addEventListener('blog:component-drag-error', () => {
    status.textContent = 'Could not move the component. Try again or use Move up or Move down.';
  });
  form.addEventListener('blog:markdown-editor', refreshLayout);
  form.addEventListener('submit', event => {
    cancelInactiveAction();
    if (!pendingAction && !editing?.validation) {
      if (editing?.addition) closeSettings();
      return;
    }
    event.preventDefault();
    event.stopImmediatePropagation();
    if (pendingAction) saveStatus.textContent = 'Wait for the component edit to finish, then save.';
  }, true);
  form.addEventListener('invalid', event => {
    const section = event.target.closest('[data-post-component]');
    if (section && form.querySelector('input:invalid, textarea:invalid, select:invalid') === event.target) editSettings(sectionID(section));
  }, true);
  content.addEventListener('input', () => {
    const action = pendingAction;
    if (action && !action.applying && action.source !== content.value) {
      finishAction(action);
      status.textContent = action.retry;
    }
    clearTimeout(refreshTimer);
    refreshTimer = setTimeout(refreshLayout, 150);
  });

  function sync() {
    if (pendingAction) finishAction(pendingAction);
    try { order = JSON.parse(field.value || '[]'); } catch { order = []; }
    if (!Array.isArray(order) || order.some(type => !instances.has(type)) || new Set(order).size !== order.length) order = [];
    for (const [type, {section, source}] of instances) {
      if (!section.dataset.componentId && !section.hasAttribute('data-component-pending') && source.value.trim() && !order.includes(type)) order.push(type);
    }
    render();
    refreshLayout();
  }

  root.addEventListener('change', event => {
    if (event.target.closest('[data-component-pending]')) return;
    if (event.target === field || event.target.matches('[data-post-component] textarea[name]')) {
      for (const type of removed.keys()) forgetRemoval(type);
      sync();
    }
  });
  root.addEventListener('input', event => {
    const section = event.target.closest('[data-post-component]');
    if (section && !section.hasAttribute('data-component-pending') && event.target.matches('textarea[name]')) {
      if (event.target.value.trim() && !order.includes(sectionID(section))) {
        order.push(sectionID(section));
        refreshLayout();
      }
      updateNames();
    }
    updateUnsaved();
  });
  root.addEventListener('blog:component-label', updateNames);
  form.addEventListener('blog:editor-baseline', async event => {
    const value = baseline = event.detail;
    savedLayout = {markers: {}, blocks: []};
    updateUnsaved();
    if (!value || !hasComponentMarkers(value.content)) return;
    const layout = await readLayout(value.content);
    if (baseline === value && layout) {
      savedLayout = layout;
      updateUnsaved();
    }
  });
  form.addEventListener('blog:uploads-finished', render);
  form.addEventListener('blog:field-validation', event => {
    const section = event.target.closest('[data-post-component]');
    if (section && event.target.matches('textarea[name]')) updateError(section);
  });
  sync();
  return {
    clear() {
      if (pendingAction) finishAction(pendingAction);
      for (const id of removed.keys()) forgetRemoval(id);
      order = [];
      moving = undefined;
      status.textContent = '';
      for (const {callbacks} of instances.values()) callbacks.clear();
      changed([]);
      refreshLayout();
    },
    register(type, callbacks) {
      const instance = instances.get(type);
      instance.callbacks = callbacks;
      updateError(instance.section);
      if (isActive(type)) queueMicrotask(updateNames);
    },
    restoreInstances(fields) {
      for (const [id, value] of Object.entries(fields)) {
        if (!instanceID.test(id) || typeof value !== 'string') continue;
        const source = instances.get(id)?.source || mount(id, value);
        if (source) source.value = value;
      }
      for (const [id, {section, source, callbacks}] of instances) {
        if (!section.dataset.componentId) continue;
        if (!Object.hasOwn(fields, id)) callbacks.clear();
        else source.dispatchEvent(new Event('change', {bubbles: true}));
      }
      sync();
    }
  };
}

prepareEditors(document);

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
