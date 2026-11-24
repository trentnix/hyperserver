// Select the enhanced navigation styles before the browser lays out the body.
// Without JavaScript, the ordinary navigation links remain available.
document.documentElement.classList.add('js');

// Apply the reader's choice before the body paints. The saved site default
// supplies the appearance until the reader chooses a mode.
{
  const root = document.documentElement;
  const siteTheme = root.dataset.theme;
  const theme = siteTheme.replace(/-dark$/, '');
  const key = 'blog-color-mode';
  let toggle;

  function updateToggle() {
    if (!toggle) return;
    const dark = root.dataset.theme.endsWith('-dark');
    toggle.setAttribute('aria-pressed', String(dark));
    toggle.title = dark ? 'Switch to light mode' : 'Switch to dark mode';
  }

  function applyMode(mode) {
    root.dataset.theme = mode === 'dark' ? theme + '-dark' : mode === 'light' ? theme : siteTheme;
    updateToggle();
  }

  function readMode() {
    try {
      applyMode(localStorage.getItem(key));
    } catch {
      // A blocked storage API still allows changes for the current document.
    }
  }

  readMode();
  document.addEventListener('DOMContentLoaded', () => {
    toggle = document.querySelector('[data-appearance-toggle]');
    if (!toggle) return;
    updateToggle();
    toggle.hidden = false;
    toggle.addEventListener('click', () => {
      const mode = root.dataset.theme.endsWith('-dark') ? 'light' : 'dark';
      applyMode(mode);
      try {
        localStorage.setItem(key, mode);
      } catch {
        // Keep the choice for this document when storage is unavailable.
      }
    });
  }, {once: true});
  window.addEventListener('storage', event => {
    if (event.key === key || event.key === null) readMode();
  });
  window.addEventListener('pageshow', event => {
    if (event.persisted) readMode();
  });
}

// Apply a tab's theme preview before the body paints.
if (document.querySelector('link[data-blog-theme]')) {
  const root = document.documentElement;
  const key = 'preview-theme';
  const themes = new Set(Array.from(document.querySelectorAll('link[data-blog-theme]')).flatMap(link => {
    const theme = link.dataset.blogTheme;
    return [theme, theme + '-dark'];
  }));
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
