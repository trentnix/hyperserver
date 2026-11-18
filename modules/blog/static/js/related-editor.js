import {registerComponent, jsonComponentValue} from './post-components.js';
import {createPostPicker} from './post-picker.js';

const initialized = new WeakSet();
function initialize(root) {
  for (const section of root.querySelectorAll('[data-related-editor]')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('[name="related"]');
    const exclude = /^\/post\/([^/]+)$/.exec(new URL(source.form.action).pathname)?.[1] || '';
    const picker = createPostPicker(section, {source, exclude});
    registerComponent(section, {
      ...picker,
      normalize: value => jsonComponentValue(value, {strings: ['title'], empty: '{"posts":[]}'}),
    });
    picker.read();
  }
}
document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
