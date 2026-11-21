import {initializeComponentPanels, registerComponent, jsonComponentValue} from './post-components.js';
import {createPostPicker} from './post-picker.js';

const initialized = new WeakSet();
function initialize(root) {
  for (const section of root.querySelectorAll('[data-related-editor]')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('textarea[name]');
    const match = /^\/(post|pages)\/([^/]+)$/.exec(new URL(source.form.action).pathname);
    const exclude = match ? (match[1] === 'pages' ? 'page:' : '') + match[2] : '';
    const picker = createPostPicker(section, {source, exclude, includePages: true});
    registerComponent(section, {
      ...picker,
      label: () => section.querySelector('[data-post-picker-title]').value,
      normalize: value => jsonComponentValue(value, {strings: ['title'], empty: '{"posts":[]}'}),
    });
    picker.read();
  }
}
initializeComponentPanels(initialize);
