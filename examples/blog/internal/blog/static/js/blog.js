(() => {
  // Keep ordinary links available without JavaScript. Only mobile collapses them.
  const mobileLayout = matchMedia('(max-width: 767px)');
  const menuToggle = document.querySelector('.menu-toggle');
  const menu = document.querySelector('#site-menu');
  function closeMenu() {
    menu.parentElement.open = !mobileLayout.matches;
    menuToggle.hidden = !mobileLayout.matches;
    for (const toggle of menu.querySelectorAll('.child-toggle')) {
      toggle.parentElement.open = !mobileLayout.matches;
      toggle.hidden = !mobileLayout.matches;
    }
  }
  menu.addEventListener('click', event => {
    if (mobileLayout.matches && event.target.closest('a')) {
      closeMenu();
      menuToggle.focus({preventScroll: true});
    }
  });
  document.querySelector('.header-menu').addEventListener('keydown', event => {
    if (event.key === 'Escape' && mobileLayout.matches && menu.parentElement.open) {
      closeMenu();
      menuToggle.focus();
    }
  });
  mobileLayout.addEventListener('change', closeMenu);
  document.addEventListener('htmx:oobAfterSwap', event => {
    if (event.detail.target.id === 'site-menu') closeMenu();
  });
  closeMenu();
  menu.dataset.ready = '';

  document.addEventListener('click', event => {
    const dismiss = event.target.closest('[data-dismiss-notification]');
    if (dismiss) dismiss.closest('.notification').remove();
  });

  // CSS owns timing, including hover/focus pauses and reduced-motion preferences.
  // Delegation also handles notifications appended by HTMX out-of-band swaps.
  document.addEventListener('animationend', event => {
    if (event.animationName === 'notification-fade' && event.target.matches('.notification--success')) {
      event.target.remove();
    }
  });

  // Apply article metadata on navigation and history restores, never on dialog swaps.
  function updatePagePresentation(root) {
    const article = root.id === 'article-content' ? root : root.querySelector('#article-content');
    const metadata = article?.querySelector(':scope > template[data-page-metadata]');
    if (metadata) {
      document.head.querySelectorAll('[data-blog-metadata]').forEach(node => node.remove());
      document.head.append(...metadata.content.children);
      metadata.remove();
    }
  }

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
  // Preserve search editing focus, or move to the result heading when following a
  // match. Keep this request-local so unrelated swaps never move focus or scroll.
  const searchFocus = new WeakMap();
  document.addEventListener('htmx:beforeRequest', event => {
    const source = event.detail.elt;
    const field = source.closest('.reader-search-form')?.querySelector('input[name=q]');
    if (field) {
      searchFocus.set(event.detail.xhr, {start: field.selectionStart, end: field.selectionEnd});
    } else if (source.closest('.search-results a')) {
      searchFocus.set(event.detail.xhr, {heading: true});
    }
  });

  // History swaps omit detail.target but still dispatch on the swapped element.
  document.addEventListener('htmx:afterSwap', event => {
    updatePagePresentation(event.target);
    if (event.target.id === 'article-content') {
      const intent = searchFocus.get(event.detail.xhr);
      if (intent) {
        searchFocus.delete(event.detail.xhr);
        const target = event.target.querySelector(intent.heading ? 'h1' : '.reader-search-form input[name=q]');
        if (target) {
          if (intent.heading) target.tabIndex = -1;
          target.focus({preventScroll: true});
          if (!intent.heading) target.setSelectionRange(intent.start, intent.end);
        }
      }
    }
  });
  updatePagePresentation(document);
})();
