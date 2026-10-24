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
  function closeMenu() {
    menu.hidden = mobileLayout.matches;
    menuToggle.hidden = !mobileLayout.matches;
    menuToggle.setAttribute('aria-expanded', String(!menu.hidden));
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
    const dismiss = event.target.closest('[data-dismiss-notification]');
    if (dismiss) dismiss.closest('.notification').remove();
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
  // History swaps omit detail.target but still dispatch on the swapped element.
  document.addEventListener('htmx:afterSwap', event => initialize(event.target));
  initialize(document);
})();
