import {componentPlacement} from './component-placement.js';
import {registerFieldEditor} from './forms.js';
import {uploadStorageFull} from './media.js';

// EasyMDE stays behind this blog-local boundary. The textarea remains the form
// field. Every editor change synchronizes it before notifying application code.
const editors = new WeakMap();

export const getMarkdownEditor = textarea => editors.get(textarea);

// CodeMirror wraps form.submit and restores it when its editor is removed.
// Each textarea already stays in sync, and editors can be removed in any order.
function preserveFormSubmit(form, update) {
  const submit = Object.getOwnPropertyDescriptor(form, 'submit');
  try {
    return update();
  } finally {
    if (submit) Object.defineProperty(form, 'submit', submit);
    else delete form.submit;
  }
}

// Callbacks own upload requests, media selection, previews, and their cleanup.
// Without EasyMDE, leave the native textarea and its validation in place.
export function createMarkdownEditor(textarea, {compact = false, upload, chooseImage, preview, onDestroy}) {
  if (editors.has(textarea)) return editors.get(textarea);
  if (!window.EasyMDE) return;

  let required = textarea.required;
  const label = textarea.labels[0];
  const labelFor = label?.htmlFor;
  const form = textarea.form;
  let destroyed = false;
  const storageFull = !compact && uploadStorageFull(form);
  const editor = preserveFormSubmit(form, () => new EasyMDE({
    element: textarea,
    minHeight: compact ? '4.5rem' : '450px',
    autoDownloadFontAwesome: false,
    spellChecker: false,
    forceSync: true,
    status: false,
    shortcuts: compact ? {togglePreview: null, toggleSideBySide: null, toggleFullScreen: null} : undefined,
    uploadImage: !compact && !storageFull,
    imageUploadEndpoint: '/post/images/upload',
    imageUploadFunction(file, onSuccess) {
      if (uploadStorageFull(form)) return;
      const status = document.createElement('p');
      status.setAttribute('role', 'status');
      editor.codemirror.getWrapperElement().after(status);
      upload(file, path => {
        if (!destroyed && textarea.isConnected) onSuccess(path);
      }, status);
    },
    imagePathAbsolute: true,
    imageMaxSize: 8 * 1024 * 1024,
    imageAccept: 'image/png,image/jpeg,image/gif',
    toolbar: [
      {name: 'bold', action: EasyMDE.toggleBold, className: 'tool-bold', title: 'Bold'},
      {name: 'italic', action: EasyMDE.toggleItalic, className: 'tool-italic', title: 'Italic'},
      {name: 'heading', action: EasyMDE.toggleHeadingSmaller, className: 'tool-heading', title: 'Heading'},
      '|',
      {name: 'quote', action: EasyMDE.toggleBlockquote, className: 'tool-quote', title: 'Quote'},
      {name: 'unordered-list', action: EasyMDE.toggleUnorderedList, className: 'tool-list', title: 'List'},
      {name: 'link', action: EasyMDE.drawLink, className: 'tool-link', title: 'Link'},
      ...(compact ? [
        {name: 'code', action: EasyMDE.toggleCodeBlock, className: 'tool-code', title: 'Code block'}
      ] : [
        {name: 'upload-image', action: EasyMDE.drawUploadedImage, className: 'tool-image', title: 'Upload image'},
        {name: 'media-library', action: () => chooseImage(path => api.insertImage(path)), className: 'tool-library', title: 'Choose existing image'},
        '|',
        {name: 'preview', action: EasyMDE.togglePreview, className: 'tool-preview', title: 'Preview'}
      ])
    ].filter(item => form.dataset.mediaLibrary !== 'false' || item.name !== 'media-library'),
    previewRender: preview
  }));
  if (storageFull) {
    editor.gui.toolbar.querySelector('.tool-image').disabled = true;
    editor.gui.toolbar.querySelector('.imageInput').disabled = true;
  }
  const container = editor.codemirror.getWrapperElement().parentElement;
  if (compact) container.classList.add('compact-markdown-editor');
  // Component validation redirects focus to the visible editor. Content has
  // its own validation flow. Mirror semantics onto each editor's keyboard input.
  if (!compact) textarea.required = false;
  const input = editor.codemirror.getInputField();
  input.id = compact ? textarea.id + '-editor-input' : 'markdown-input';
  if (compact && label) label.id ||= textarea.id + '-editor-label';
  const labelID = compact ? label?.id : 'markdown-label';
  if (labelID) input.setAttribute('aria-labelledby', labelID);
  const needsLabelFocus = compact && !input.matches('textarea');
  if (needsLabelFocus) {
    input.setAttribute('role', 'textbox');
    input.setAttribute('aria-multiline', 'true');
  }
  if (required) input.setAttribute('aria-required', 'true');
  if (label) label.htmlFor = input.id;
  const keys = {...editor.codemirror.getOption('extraKeys'), Tab: false, 'Shift-Tab': false};
  if (compact) keys.Esc = false;
  editor.codemirror.setOption('extraKeys', keys);

  const showSource = () => { if (editor.isPreviewActive()) editor.togglePreview(); };
  const maxLength = textarea.maxLength;
  const limit = compact && maxLength >= 0 ? document.createElement('p') : null;
  if (limit) {
    limit.className = 'compact-markdown-limit';
    limit.setAttribute('role', 'status');
    container.append(limit);
    editor.codemirror.on('beforeChange', (cm, change) => {
      const inserted = change.text.join('\n');
      const replaced = cm.getRange(change.from, change.to);
      const current = cm.getValue();
      if (current.length + inserted.length - replaced.length <= maxLength) return;
      const growth = [...inserted].length - [...replaced].length;
      if (growth <= 0 || [...current].length + growth <= maxLength) return;
      change.cancel();
      limit.textContent = `That edit exceeds the ${maxLength.toLocaleString('en-US')}-character limit.`;
    });
  }
  const picker = compact ? null : form.querySelector('[data-component-picker]');
  if (picker) editor.gui.toolbar.append(picker);
  const changed = () => {
    if (limit?.textContent) limit.textContent = '';
    textarea.value = editor.value();
    textarea.dispatchEvent(new Event('input', {bubbles: true}));
  };
  const api = {
    container,
    placement: !compact && form.elements.componentOrder ? componentPlacement(editor.codemirror) : null,
    showSource,
    getMarkdown: () => destroyed ? textarea.value : editor.value(),
    setMarkdown(markdown) {
      if (!destroyed) editor.value(markdown);
    },
    insertImage(path) {
      if (!destroyed && textarea.isConnected) editor.codemirror.replaceSelection(`![](${path})`);
    },
    focus() {
      if (!destroyed && textarea.isConnected) {
        showSource();
        editor.codemirror.focus();
      }
    },
    setRequired(value) {
      if (destroyed) return;
      required = value;
      textarea.required = false;
      if (required) input.setAttribute('aria-required', 'true');
      else input.removeAttribute('aria-required');
    },
    syncValidation() {
      for (const attribute of ['aria-invalid', 'aria-describedby']) {
        if (textarea.hasAttribute(attribute)) input.setAttribute(attribute, textarea.getAttribute(attribute));
        else input.removeAttribute(attribute);
      }
    },
    destroy() {
      if (destroyed) return;
      destroyed = true;
      api.placement?.destroy();
      unregisterField?.();
      if (needsLabelFocus) label?.removeEventListener('click', api.focus);
      onDestroy?.();
      if (picker) textarea.before(picker);
      editor.codemirror.off('change', changed);
      editor.cleanup();
      const value = textarea.value;
      preserveFormSubmit(form, () => editor.toTextArea());
      // A component can replace the textarea value before refreshing its editor.
      if (compact) textarea.value = value;
      textarea.required = required;
      if (label) label.htmlFor = labelFor;
      editors.delete(textarea);
      if (!compact) textarea.dispatchEvent(new Event('blog:markdown-editor', {bubbles: true}));
    }
  };
  const unregisterField = compact ? registerFieldEditor(textarea, api) : undefined;
  if (needsLabelFocus) label?.addEventListener('click', api.focus);
  editors.set(textarea, api);
  api.syncValidation();
  editor.codemirror.on('change', changed);
  if (!compact) textarea.dispatchEvent(new Event('blog:markdown-editor', {bubbles: true}));
  return api;
}

export function refreshCompactMarkdownEditors(root) {
  for (const textarea of root.querySelectorAll('textarea[data-compact-markdown]')) {
    const visible = textarea.closest('.component-dialog[open] .component-panel-active') && !textarea.closest('[hidden]');
    const editor = getMarkdownEditor(textarea);
    if (editor && (!visible || editor.getMarkdown() !== textarea.value)) editor.destroy();
    if (visible) createMarkdownEditor(textarea, {compact: true});
  }
}

export function destroyMarkdownEditors(root) {
  getMarkdownEditor(root)?.destroy();
  for (const textarea of root.querySelectorAll('textarea')) getMarkdownEditor(textarea)?.destroy();
}

document.addEventListener('blog:field-validation', event => getMarkdownEditor(event.target)?.syncValidation());
document.addEventListener('htmx:before:cleanup', event => destroyMarkdownEditors(event.target));
