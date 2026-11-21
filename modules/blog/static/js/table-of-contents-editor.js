import {initializeComponentPanels, registerComponent} from './post-components.js';

const initialized = new WeakSet();
const defaultLevels = [2, 3];

function headingLevels(source) {
  let settings;
  try { settings = JSON.parse(source.trim() || '{}'); }
  catch { return null; }
  if (!settings || typeof settings !== 'object' || Array.isArray(settings)) return null;
  const keys = Object.keys(settings);
  if (keys.length === 0) return defaultLevels;
  const {levels} = settings;
  if (keys.length !== 1 || !Array.isArray(levels) || new Set(levels).size !== levels.length ||
      !levels.every(level => Number.isInteger(level) && level >= 1 && level <= 6)) return null;
  return levels.sort((a, b) => a - b);
}

function editorSource(levels) {
  return levels.length === 2 && levels[0] === 2 && levels[1] === 3 ? '{}' : JSON.stringify({levels});
}

function initialize(root) {
  for (const section of root.querySelectorAll('.table-of-contents-editor')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('textarea[name]');
    const label = source.labels[0];
    const fields = section.querySelector('[data-toc-levels]');
    const choices = [...fields.querySelectorAll('input')];
    const help = fields.querySelector('[data-toc-levels-help]');
    const defaultHelp = help.textContent;
    const updateHelp = levels => { help.textContent = levels.length ? defaultHelp : 'Choose at least one heading level.'; };
    const read = () => {
      const levels = headingLevels(source.value);
      source.hidden = label.hidden = levels !== null;
      fields.hidden = levels === null;
      fields.disabled = section.hidden || fields.hidden;
      if (levels === null) return;
      for (const choice of choices) choice.checked = levels.includes(Number(choice.value));
      updateHelp(levels);
    };
    const restore = value => { source.value = value; read(); };
    source.addEventListener('change', read);
    fields.addEventListener('change', () => {
      const levels = choices.filter(choice => choice.checked).map(choice => Number(choice.value));
      source.value = editorSource(levels);
      updateHelp(levels);
      source.dispatchEvent(new Event('input', {bubbles: true}));
    });
    registerComponent(section, {
      add: () => restore('{}'),
      normalize: value => {
        if (!value.trim()) return '';
        const levels = headingLevels(value);
        return levels ? editorSource(levels) : value;
      },
      snapshot: () => source.value,
      clear: () => restore(''),
      restore,
      focus: () => (source.hidden ? choices[0] : source).focus()
    });
    read();
  }
}

initializeComponentPanels(initialize);
