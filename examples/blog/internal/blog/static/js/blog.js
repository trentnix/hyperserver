/* EasyMDE owns editing, toolbar actions, preview presentation, and image uploads.
 * The server validates posts and renders Markdown for both preview and publication. */
(() => {
  const editors = new WeakMap();
  document.documentElement.classList.add('js');

  // Move one archive to keep visual and keyboard order aligned without reloading it.
  // This breakpoint matches the sidebar layout in blog.css.
  const sidebarLayout = matchMedia('(min-width: 1680px)');
  const archive = document.querySelector('#post-archive');
  const sidebar = document.querySelector('.site-sidebar');
  const footer = document.querySelector('.site-footer');
  function positionArchive() {
    if (sidebarLayout.matches) sidebar.append(archive);
    else footer.prepend(archive);
  }
  sidebarLayout.addEventListener('change', positionArchive);
  positionArchive();

  // Keep ordinary links available without JavaScript. Only mobile collapses them.
  const mobileLayout = matchMedia('(max-width: 767px)');
  const menuToggle = document.querySelector('.menu-toggle');
  const menu = document.querySelector('#site-menu');
  const childToggles = [...menu.querySelectorAll('.child-toggle')];
  function closeMenu() {
    menu.hidden = mobileLayout.matches;
    menuToggle.hidden = !mobileLayout.matches;
    menuToggle.setAttribute('aria-expanded', String(!menu.hidden));
    for (const toggle of childToggles) {
      const children = document.getElementById(toggle.getAttribute('aria-controls'));
      children.hidden = mobileLayout.matches;
      toggle.hidden = !mobileLayout.matches;
      toggle.setAttribute('aria-expanded', String(!children.hidden));
    }
  }
  for (const toggle of childToggles) {
    toggle.addEventListener('click', () => {
      const children = document.getElementById(toggle.getAttribute('aria-controls'));
      children.hidden = !children.hidden;
      toggle.setAttribute('aria-expanded', String(!children.hidden));
    });
  }
  menuToggle.addEventListener('click', () => {
    menu.hidden = !menu.hidden;
    menuToggle.setAttribute('aria-expanded', String(!menu.hidden));
  });
  menu.addEventListener('click', event => {
    if (mobileLayout.matches && event.target.closest('a')) {
      closeMenu();
      menuToggle.focus({preventScroll: true});
    }
  });
  document.querySelector('.header-menu').addEventListener('keydown', event => {
    if (event.key === 'Escape' && mobileLayout.matches && !menu.hidden) {
      closeMenu();
      menuToggle.focus();
    }
  });
  mobileLayout.addEventListener('change', closeMenu);
  closeMenu();

  // CSS owns timing, including hover/focus pauses and reduced-motion preferences.
  // Delegation also handles notifications appended by HTMX out-of-band swaps.
  document.addEventListener('animationend', event => {
    if (event.animationName === 'notification-fade' && event.target.matches('.notification--success')) {
      event.target.remove();
    }
  });

  function initialize(root) {
    root.querySelectorAll('dialog:not([open])').forEach(dialog => dialog.showModal());
    root.querySelectorAll('.image-upload-controls').forEach(controls => { controls.hidden = false; });
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
            if (!response.ok) throw new Error('Unable to preview this content.');
            // The server uses the same safe Markdown renderer as published posts.
            preview.innerHTML = html;
          }).catch(error => {
            if (error.name !== 'AbortError') preview.textContent = 'Unable to preview this content.';
          });
          return 'Loading preview…';
        }
      });
      editors.set(textarea, {editor, cancelPreview: () => previewRequest?.abort()});
    }
  }

  // Page previews store an image ID. Profile fields store the complete URL.
  function updateImagePreview(picker) {
    const value = picker.querySelector('[data-image-value]').value.trim();
    const url = value ? (picker.dataset.imagePrefix || '') + value : picker.dataset.imageDefault;
    const preview = picker.querySelector('[data-image-preview]');
    preview.hidden = !url;
    if (url) preview.src = url;
    else preview.removeAttribute('src');
  }

  document.addEventListener('click', event => {
    const remove = event.target.closest('[data-image-reset]');
    if (remove) {
      if (remove.form.dataset.uploading) return;
      const picker = remove.closest('[data-image-picker]');
      picker.querySelector('[data-image-value]').value = '';
      picker.querySelector('input[type="file"]').value = '';
      updateImagePreview(picker);
      picker.querySelector('[data-image-status]').textContent = 'Selection cleared. Save to apply.';
    }
    const dismiss = event.target.closest('[data-dismiss-notification]');
    if (dismiss) dismiss.closest('.notification').remove();
    const close = event.target.closest('[data-close-dialog]');
    if (close) close.closest('dialog').close();
  });

  // Pickers share the editor's validated upload endpoint. Serialize uploads within
  // each form so Save cannot race another image selection or submit incomplete URLs.
  document.addEventListener('change', async event => {
    const input = event.target;
    if (input.matches('[data-image-value]')) {
      updateImagePreview(input.closest('[data-image-picker]'));
      return;
    }
    if (!input.matches('[data-image-picker] input[type="file"]') || !input.files.length) return;
    const picker = input.closest('[data-image-picker]');
    const form = input.form;
    if (form.dataset.uploading) {
      input.value = '';
      return;
    }
    const status = picker.querySelector('[data-image-status]');
    const file = input.files[0];
    if (file.size > 8 * 1024 * 1024) {
      status.textContent = 'Choose an image of at most 8 MiB. The current image is unchanged.';
      input.value = '';
      return;
    }
    form.dataset.uploading = 'true';
    const controls = [...form.querySelectorAll('[data-image-picker] input, [data-image-reset], [type="submit"]')].filter(control => !control.disabled);
    controls.forEach(control => { control.disabled = true; });
    status.textContent = 'Uploading image…';
    try {
      const data = new FormData();
      data.append('image', file);
      const response = await fetch('/post/images/upload', {method: 'POST', body: data});
      if (!(response.headers.get('Content-Type') || '').includes('application/json')) throw new Error('Unable to upload the image.');
      const result = await response.json();
      if (!response.ok) throw new Error(result?.error || 'Unable to upload the image.');
      const path = result?.data?.filePath;
      if (typeof path !== 'string' || !path.startsWith('/images/')) throw new Error('Invalid image upload response.');
      picker.querySelector('[data-image-value]').value = path.slice((picker.dataset.imagePrefix || '').length);
      updateImagePreview(picker);
      status.textContent = 'Image uploaded. Save to apply.';
    } catch (error) {
      status.textContent = error.message + ' The current image is unchanged.';
    } finally {
      delete form.dataset.uploading;
      controls.forEach(control => { control.disabled = false; });
      input.value = '';
    }
  });
  document.addEventListener('submit', event => {
    if (event.target.dataset.uploading) {
      event.preventDefault();
      event.stopImmediatePropagation();
    }
  }, true);
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
  // History swaps omit detail.target but still dispatch on the swapped element.
  document.addEventListener('htmx:afterSwap', event => initialize(event.target));
  initialize(document);
})();
