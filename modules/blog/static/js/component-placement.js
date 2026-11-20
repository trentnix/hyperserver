import {enableComponentDrag} from './component-drag.js';

const marker = type => '<!-- hyperserver:component ' + type + ' -->';
export const hasComponentMarkers = (source = '') => source.includes('<!-- hyperserver:component ');

export function nameComponentRow(row, name) {
  if (row.getAttribute('aria-label') === name) return false;
  row.setAttribute('aria-label', name);
  const title = row.querySelector('strong');
  title.textContent = title.title = name;
  row.querySelector('[data-component-edit]').setAttribute('aria-label', 'Edit ' + name);
  row.querySelector('[data-component-place]').setAttribute('aria-label', 'Move ' + name);
  row.querySelector('[data-component-remove-row]').setAttribute('aria-label', 'Remove ' + name);
  for (const button of row.querySelectorAll('[data-component-shift]')) {
    button.setAttribute('aria-label', 'Move ' + name + ' ' + button.dataset.componentShift);
  }
  return true;
}

export function componentRow(type, name) {
  const row = document.createElement('span');
  row.className = 'component-marker';
  row.dataset.componentRow = type;
  row.setAttribute('role', 'group');
  const title = document.createElement('strong');
  const action = (label, attribute, value) => {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'text-button';
    button.textContent = label;
    button.dataset[attribute] = value;
    return button;
  };
  const move = action('Move', 'componentPlace', type);
  move.setAttribute('aria-expanded', 'false');
  const remove = action('Remove', 'componentRemoveRow', type);
  remove.classList.add('component-remove');
  const actions = document.createElement('span');
  actions.className = 'component-row-actions';
  actions.append(action('Edit', 'componentEdit', type), move, remove);
  const steps = document.createElement('span');
  steps.className = 'component-move-steps';
  steps.hidden = true;
  steps.append(action('Up', 'componentShift', 'up'), action('Down', 'componentShift', 'down'));
  row.append(title, actions, steps);
  nameComponentRow(row, name);
  return row;
}

// Goldmark supplies every marker and destination. CodeMirror follows edits
// with its tracked ranges until the current source has been parsed again.
export function componentPlacement(cm) {
  const widgets = new Map();
  const layouts = new Map();
  const lifetime = new AbortController();
  const cancelDrag = enableComponentDrag(cm, layout, lifetime.signal);
  let usedCursor = cm.hasFocus();
  cm.on('focus', () => { usedCursor = true; });

  function layout(source = cm.getValue()) {
    if (!source) return Promise.resolve({blocks: [], drops: [0], markers: {}, end: 0});
    if (!layouts.has(source)) {
      const request = fetch('/post/component-position', {
        method: 'POST', signal: lifetime.signal, redirect: 'error',
        body: new URLSearchParams({content: source})
      }).then(response => {
        if (!response.ok) throw new Error('Could not read component positions');
        return response.json();
      }).catch(error => {
        if (layouts.get(source) === request) layouts.delete(source);
        throw error;
      });
      layouts.set(source, request);
      // Keep the baseline and recent edits without retaining the whole history.
      if (layouts.size > 4) layouts.delete(layouts.keys().next().value);
    }
    return layouts.get(source);
  }

  function refresh(anchors, nameFor) {
    cm.operation(() => {
      for (const [type, {mark}] of widgets) {
        const range = mark.find();
        if (range && range.from.line === anchors.get(type) && range.from.ch === 0 &&
            range.to.line === range.from.line && range.to.ch === marker(type).length) continue;
        mark.clear();
        widgets.delete(type);
      }
      for (const [type, line] of anchors) {
        if (widgets.has(type)) continue;
        const row = componentRow(type, nameFor(type));
        const handle = row.querySelector('[data-component-place]');
        handle.dataset.componentDrag = '';
        handle.title = 'Drag or show move controls';
        const mark = cm.markText({line, ch: 0}, {line, ch: marker(type).length}, {replacedWith: row});
        widgets.set(type, {mark, row});
      }
    });
  }

  const doc = cm.getDoc();
  let generation = doc.changeGeneration();
  const history = new Map();

  function replay(_, change) {
    const next = doc.changeGeneration();
    if (next === generation) return;
    if (change.origin === 'undo') history.get(generation)?.('before');
    if (change.origin === 'redo') history.get(next)?.('after');
    generation = next;
  }
  cm.on('change', replay);

  function bookmark(line) {
    const mark = cm.setBookmark({line, ch: 0}, {insertLeft: true});
    return {find: () => mark.find(), clear: () => mark.clear()};
  }

  function transact(layout, edit, onHistory) {
    // Update known marker positions immediately. Goldmark refreshes the exact
    // boundaries after the entire change has reached the document and form.
    const markers = {...layout.markers};
    const separators = {...layout.separators};
    let blocks = [...layout.blocks];
    let end = layout.end;
    const shift = (line, count) => {
      for (const type of Object.keys(markers)) {
        if (markers[type] >= line) markers[type] += count;
      }
      blocks = blocks.map(block => block >= line ? block + count : block);
      if (end !== null && end >= line) end += count;
    };
    function remove(type) {
      const line = markers[type];
      if (line === undefined) return;
      const separator = separators[type];
      delete markers[type];
      delete separators[type];
      if (!separator) blocks = blocks.filter(block => block !== line);
      const last = cm.lastLine();
      const removeLine = separator === undefined && line < last;
      const to = removeLine ? {line: line + 1, ch: 0} : {line, ch: cm.getLine(line).length};
      cm.replaceRange(separator ?? '', {line, ch: 0}, to, 'component-placement');
      if (removeLine) shift(line + 1, -1);
      return {position: bookmark(line), separator, removeLine};
    }
    function place(type, line, separator) {
      const current = markers[type];
      if (current === line || current + 1 === line) return;
      if (current !== undefined) {
        const removed = remove(type);
        removed.position.clear();
        if (removed.removeLine && current < line) line--;
      }
      if (separator !== undefined && cm.getLine(line) === separator) {
        cm.replaceRange(marker(type), {line, ch: 0}, {line, ch: separator.length}, 'component-placement');
      } else {
        const last = cm.lastLine();
        const prefix = '\n'.repeat(Math.max(0, line - last));
        const at = prefix ? {line: last, ch: cm.getLine(last).length} : {line, ch: 0};
        cm.replaceRange(prefix + marker(type) + '\n', at, undefined, 'component-placement');
        shift(line, 1);
      }
      markers[type] = line;
      if (!blocks.includes(line)) blocks.push(line);
      blocks.sort((a, b) => a - b);
      cm.setCursor({line: line + 1, ch: 0});
    }

    const before = doc.changeGeneration(true);
    // An edit starts a new branch. Drop redo records and bound retained
    // snapshots to the same depth as the editor history.
    for (const key of history.keys()) if (key > before) history.delete(key);
    cm.operation(() => {
      edit({place, remove, layout: () => ({source: cm.getValue(), blocks, markers, separators, end})});
      if (doc.changeGeneration() === before) {
        // CodeMirror can record a zero-width marker without altering prose.
        // This puts changes to legacy rows on the same Undo stack as typing.
        const at = cm.getCursor();
        doc.markText(at, at, {addToHistory: true, clearWhenEmpty: false}).clear();
      }
      generation = doc.changeGeneration(true);
      history.set(generation, onHistory);
      while (history.size > cm.getOption('undoDepth')) history.delete(history.keys().next().value);
    });
  }

  return {
    layout,
    refresh,
    transact,
    bookmark,
    undoOrRedo(redo) {
      if (redo) cm.redo();
      else cm.undo();
    },
    resetHistory() {
      cm.clearHistory();
      history.clear();
      generation = doc.changeGeneration();
    },
    row: type => widgets.get(type)?.row,
    cursor: () => usedCursor ? cm.getCursor('head') : null,
    scrollToMarker(type) {
      const range = widgets.get(type)?.mark.find();
      if (range) cm.scrollIntoView(range.from);
    },
    measureRow(type) {
      widgets.get(type)?.mark.changed();
    },
    destroy() {
      lifetime.abort();
      cm.off('change', replay);
      cancelDrag();
    }
  };
}
