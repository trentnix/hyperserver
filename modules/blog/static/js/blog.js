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
  document.addEventListener('htmx:after:settle', event => {
    if (event.detail.task.target.id === 'site-menu') closeMenu();
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
    if (!root) return;
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
  document.addEventListener('htmx:before:request', event => {
    const {ctx} = event.detail;
    const source = ctx.sourceElement;
    const field = source.closest('.reader-search-form')?.querySelector('input[name=q]');
    if (field) {
      navigationFocus.set(ctx, {start: field.selectionStart, end: field.selectionEnd});
    } else if (source.closest('a[href]') && ctx.target.id === 'article-content') {
      navigationFocus.set(ctx, {heading: true});
    }
  });

  document.addEventListener('htmx:after:swap', event => {
    const {ctx} = event.detail;
    const root = typeof ctx.target === 'string' ? document.querySelector(ctx.target) : ctx.target;
    updatePagePresentation(root);
    if (root?.id === 'article-content') {
      const intent = navigationFocus.get(ctx);
      if (intent) {
        navigationFocus.delete(ctx);
        const target = intent.heading ? root.querySelector('h1') || root : root.querySelector('.reader-search-form input[name=q]');
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
