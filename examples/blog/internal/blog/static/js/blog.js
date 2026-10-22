/* EasyMDE owns editing, toolbar actions, preview presentation, and image uploads.
 * The server validates posts and renders Markdown for both preview and publication. */
(() => {
  const editors = new WeakMap();

  function initialize(root) {
    root.querySelectorAll('dialog:not([open])').forEach(dialog => dialog.showModal());
    const textarea = root.querySelector('#post-content');
    if (textarea && window.EasyMDE && !editors.has(textarea)) {
      // EasyMDE replaces native textarea validation. The server remains authoritative.
      textarea.required = false;
      let previewRequest;
      const editor = new EasyMDE({
        element: textarea,
        autoDownloadFontAwesome: false,
        spellChecker: false,
        forceSync: true,
        status: false,
        uploadImage: true,
        imageUploadEndpoint: '/post/images/upload',
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
          '|',
          {name: 'preview', action: EasyMDE.togglePreview, className: 'tool-preview', title: 'Preview'}
        ],
        previewRender(markdown, preview) {
          previewRequest?.abort();
          previewRequest = new AbortController();
          fetch('/post/preview', {
            method: 'POST', body: new URLSearchParams({content: markdown}), signal: previewRequest.signal
          }).then(async response => {
            const html = await response.text();
            if (!response.ok) throw new Error('Unable to preview this post.');
            // The server uses the same safe Markdown renderer as published posts.
            preview.innerHTML = html;
          }).catch(error => {
            if (error.name !== 'AbortError') preview.textContent = 'Unable to preview this post.';
          });
          return 'Loading preview…';
        }
      });
      editors.set(textarea, {editor, cancelPreview: () => previewRequest?.abort()});
    }
  }

  document.addEventListener('click', event => {
    const close = event.target.closest('[data-close-dialog]');
    if (close) close.closest('dialog').close();
  });
  document.addEventListener('htmx:beforeCleanupElement', event => {
    const textarea = event.detail.elt;
    const state = editors.get(textarea);
    if (state) {
      state.cancelPreview();
      state.editor.cleanup();
      state.editor.toTextArea();
      editors.delete(textarea);
    }
  });

  function showRequestError(detail, message) {
    // Keep failures inside an open modal or the submitting form without replacing its inputs.
    const container = detail.elt?.closest('form') || detail.target?.closest('form') || detail.target?.querySelector('form') ||
      document.querySelector('dialog[open]') || document.querySelector('#notifications');
    let notice = container.querySelector('[data-request-error]');
    if (!notice) {
      notice = document.createElement('p');
      notice.dataset.requestError = '';
      notice.className = 'field-error';
      notice.setAttribute('role', 'alert');
      container.prepend(notice);
    }
    notice.textContent = message;
  }

  // Render styled HTML errors in their existing target. Never insert plain-text errors as HTML.
  document.addEventListener('htmx:beforeSwap', event => {
    const {xhr} = event.detail;
    if (xhr.status < 400 || xhr.status > 599) return;
    const type = (xhr.getResponseHeader('Content-Type') || '').split(';')[0].trim().toLowerCase();
    if (type === 'text/html') {
      event.detail.shouldSwap = true;
      event.detail.isError = false;
    } else {
      event.detail.shouldSwap = false;
      const message = type === 'text/plain' ? xhr.responseText.trim() : '';
      showRequestError(event.detail, message || `Request failed (HTTP ${xhr.status}). Please try again.`);
    }
  });
  for (const name of ['htmx:sendError', 'htmx:timeout']) {
    document.addEventListener(name, event => showRequestError(event.detail,
      'The connection failed. Check whether the operation completed before retrying.'));
  }
  document.addEventListener('htmx:afterSwap', event => initialize(event.detail.target));
  initialize(document);
})();
