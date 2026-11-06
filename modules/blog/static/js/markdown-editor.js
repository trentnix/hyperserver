// EasyMDE stays behind this blog-local boundary. The textarea remains the form
// field. Every editor change synchronizes it before notifying application code.
const editors = new WeakMap();

export const getMarkdownEditor = textarea => editors.get(textarea);

// Callbacks own upload requests, media selection, previews, and their cleanup.
// Without EasyMDE, leave the native textarea and its validation in place.
export function createMarkdownEditor(textarea, {upload, chooseImage, preview, onDestroy}) {
  if (editors.has(textarea)) return editors.get(textarea);
  if (!window.EasyMDE) return;

  const required = textarea.required;
  const label = textarea.labels[0];
  const labelFor = label?.htmlFor;
  let destroyed = false;
  const editor = new EasyMDE({
    element: textarea,
    minHeight: '450px',
    autoDownloadFontAwesome: false,
    spellChecker: false,
    forceSync: true,
    status: false,
    uploadImage: true,
    imageUploadEndpoint: '/post/images/upload',
    imageUploadFunction(file, onSuccess) {
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
      {name: 'upload-image', action: EasyMDE.drawUploadedImage, className: 'tool-image', title: 'Upload image'},
      {name: 'media-library', action: () => chooseImage(path => api.insertImage(path)), className: 'tool-library', title: 'Choose existing image'},
      '|',
      {name: 'preview', action: EasyMDE.togglePreview, className: 'tool-preview', title: 'Preview'}
    ].filter(item => textarea.form.dataset.mediaLibrary !== 'false' || item.name !== 'media-library'),
    previewRender: preview
  });
  // Native textarea validation cannot focus a field hidden by the editor.
  // The server remains authoritative. Mirror semantics onto the keyboard input.
  textarea.required = false;
  const input = editor.codemirror.getInputField();
  input.id = 'markdown-input';
  input.setAttribute('aria-labelledby', 'markdown-label');
  if (required) input.setAttribute('aria-required', 'true');
  if (label) label.htmlFor = input.id;
  editor.codemirror.setOption('extraKeys', {...editor.codemirror.getOption('extraKeys'), Tab: false, 'Shift-Tab': false});

  const changed = () => {
    textarea.value = editor.value();
    textarea.dispatchEvent(new Event('input', {bubbles: true}));
  };
  const api = {
    getMarkdown: () => destroyed ? textarea.value : editor.value(),
    setMarkdown(markdown) {
      if (!destroyed) editor.value(markdown);
    },
    insertImage(path) {
      if (!destroyed && textarea.isConnected) editor.codemirror.replaceSelection(`![](${path})`);
    },
    focus() {
      if (!destroyed && textarea.isConnected) editor.codemirror.focus();
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
      onDestroy();
      editor.codemirror.off('change', changed);
      editor.cleanup();
      editor.toTextArea();
      textarea.required = required;
      if (label) label.htmlFor = labelFor;
      editors.delete(textarea);
    }
  };
  editors.set(textarea, api);
  api.syncValidation();
  editor.codemirror.on('change', changed);
  return api;
}
