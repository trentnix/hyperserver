import {initializeComponentPanels, registerComponent, jsonComponentValue} from './post-components.js';
import {createPostPicker} from './post-picker.js';

const initialized = new WeakSet();
function initialize(root) {
  for (const section of root.querySelectorAll('[data-related-editor]')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('textarea[name]');
    const exclude = /^\/post\/([^/]+)$/.exec(new URL(source.form.action).pathname)?.[1] || '';
    const picker = createPostPicker(section, {source, exclude});
    registerComponent(section, {
      ...picker,
      label: () => section.querySelector('[data-post-picker-title]').value,
      normalize: value => jsonComponentValue(value, {strings: ['title'], empty: '{"posts":[]}'}),
    });
    picker.read();
  }
}
initializeComponentPanels(initialize);
