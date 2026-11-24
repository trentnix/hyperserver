// Select the enhanced navigation styles before the browser lays out the body.
// Without JavaScript, the ordinary navigation links remain available.
document.documentElement.classList.add('js');

// Apply a tab's theme preview before the body paints.
if (document.querySelector('link[data-blog-theme]')) {
  const root = document.documentElement;
  const key = 'preview-theme';
  const themes = new Set(Array.from(document.querySelectorAll('link[data-blog-theme]'), link => link.dataset.blogTheme));
  function applyThemePreview() {
    const url = new URL(location.href);
    const requested = url.searchParams.get(key);
    let preview = requested;
    try {
      if (themes.has(requested)) {
        sessionStorage.setItem(key, requested);
      } else if (requested === 'off') {
        sessionStorage.removeItem(key);
      } else {
        preview = sessionStorage.getItem(key);
      }
    } catch {
      // An explicit preview also works when tab storage is unavailable.
    }
    if (themes.has(requested) || requested === 'off') {
      url.searchParams.delete(key);
      history.replaceState(history.state, '', url);
    }
    if (themes.has(preview)) {
      root.dataset.themePreview = preview;
    } else {
      delete root.dataset.themePreview;
    }
  }
  applyThemePreview();
  window.addEventListener('pageshow', (event) => {
    if (event.persisted) applyThemePreview();
  });
  document.addEventListener('click', event => {
    if (!event.target.closest('[data-exit-theme-preview]')) return;
    try {
      sessionStorage.removeItem(key);
    } catch {
      // Reloading also drops a preview that could not be stored.
    }
    location.reload();
  });
}

// Mobile backgrounds use the full viewport. Reserve only the content width,
// using zero extra space on browsers with overlay scrollbars.
{
  const probe = document.createElement('div');
  probe.style.cssText = 'position:absolute;visibility:hidden;width:100px;height:100px;overflow:scroll';
  document.documentElement.append(probe);
  document.documentElement.style.setProperty('--scrollbar-width', `${probe.offsetWidth - probe.clientWidth}px`);
  probe.remove();
}
