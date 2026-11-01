(() => {
  // Keep ordinary links available without JavaScript. Only mobile collapses them.
  const mobileLayout = matchMedia('(max-width: 767px)');
  const menuToggle = document.querySelector('.menu-toggle');
  const menu = document.querySelector('#site-menu');
  function closeMenu() {
    if (!menu) return;
    menu.parentElement.open = !mobileLayout.matches;
    menuToggle.hidden = !mobileLayout.matches;
    for (const toggle of menu.querySelectorAll('.child-toggle')) {
      toggle.parentElement.open = !mobileLayout.matches;
      toggle.hidden = !mobileLayout.matches;
    }
  }
  menu?.addEventListener('click', event => {
    if (mobileLayout.matches && event.target.closest('a')) {
      closeMenu();
      menuToggle.focus({preventScroll: true});
    }
  });
  document.querySelector('.header-menu')?.addEventListener('keydown', event => {
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
  if (menu) menu.dataset.ready = '';

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

  // Article navigation moves focus to the new heading. Search submissions keep
  // editing focus. In-place updates and dialogs manage their own focus.
  const navigationFocus = new WeakMap();
  document.addEventListener('htmx:beforeRequest', event => {
    const source = event.detail.elt;
    const field = source.closest('.reader-search-form')?.querySelector('input[name=q]');
    if (field) {
      navigationFocus.set(event.detail.xhr, {start: field.selectionStart, end: field.selectionEnd});
    } else if (source.closest('a[href]') && event.detail.target.id === 'article-content') {
      navigationFocus.set(event.detail.xhr, {heading: true});
    }
  });

  // History swaps omit detail.target but still dispatch on the swapped element.
  document.addEventListener('htmx:afterSwap', event => {
    updatePagePresentation(event.target);
    if (event.target.id === 'article-content') {
      const intent = navigationFocus.get(event.detail.xhr);
      if (intent) {
        navigationFocus.delete(event.detail.xhr);
        const target = intent.heading ? event.target.querySelector('h1') || event.target : event.target.querySelector('.reader-search-form input[name=q]');
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
