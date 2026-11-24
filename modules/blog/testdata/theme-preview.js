(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const root = document.documentElement;
  const preview = document.querySelector('.theme-preview');
  const palettes = {
    default: {page: 'rgb(255, 255, 255)', panel: 'rgb(244, 246, 249)'},
    thanksgiving: {page: 'rgb(255, 250, 242)', panel: 'rgb(248, 238, 224)'},
    christmas: {page: 'rgb(255, 253, 249)', panel: 'rgb(239, 244, 239)'},
    'default-dark': {page: 'rgb(24, 28, 36)', panel: 'rgb(43, 50, 64)'},
    'thanksgiving-dark': {page: 'rgb(33, 27, 23)', panel: 'rgb(54, 43, 36)'},
    'christmas-dark': {page: 'rgb(21, 30, 27)', panel: 'rgb(39, 54, 46)'},
  };
  const palette = palettes[expectedTheme];
  const siteThemeAfterSave = savedTheme === 'default' ? savedTheme : expectedTheme;
  const isDark = theme => theme.endsWith('-dark');
  const baseTheme = theme => theme.replace(/-dark$/, '');
  const backgroundURL = theme => baseTheme(theme) === 'default' ? '/assets/img/site-bg.jpg' : '/assets/img/site-bg-' + baseTheme(theme) + '.webp';
  const artwork = theme => getComputedStyle(document.body, isDark(theme) ? '::before' : null).backgroundImage;
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assertContrast = (foreground, background) => {
    assert(testColorContrast(getComputedStyle(foreground).color, getComputedStyle(background).backgroundColor) >= 4.5,
      'Dark mode text needs 4.5:1 contrast in ' + foreground.tagName);
  };
  const assertPreviewExited = () => {
    assert(location.pathname + location.search + location.hash === '/post/s/theme-preview?source=preview#article-content', 'Exit changed the article URL.');
    assert(!root.dataset.themePreview && getComputedStyle(preview).display === 'none', 'Exit did not restore the configured theme.');
    assert(root.dataset.theme === siteThemeAfterSave, 'Exit did not pick up the latest saved theme.');
    assert(getComputedStyle(root).backgroundColor === palettes[siteThemeAfterSave].page, 'Exit did not restore the saved palette.');
    assert(artwork(siteThemeAfterSave).includes(backgroundURL(siteThemeAfterSave)), 'Exit did not restore the saved background.');
  };
  try {
    if (location.pathname === '/profile') {
      const {theme, darkMode} = document.querySelector('#profile-form').elements;
      theme.value = baseTheme(expectedTheme);
      darkMode.checked = true;
      let destination;
      window.open = url => { destination = url; };
      document.querySelector('[data-preview-profile-theme]').click();
      assert(destination === '/?preview-theme=' + expectedTheme, 'The preview button did not include the selected dark mode.');
      location.assign(destination);
      return;
    }
    if (sessionStorage.getItem('theme-test-exit') === 'signed-out') {
      assert(!preview && !root.dataset.themePreview, 'Signed-out visitor can preview an unpublished theme.');
      assert(root.dataset.theme === siteThemeAfterSave && getComputedStyle(root).backgroundColor === palettes[siteThemeAfterSave].page, 'Signed-out visitor lost the saved theme.');
      assert(document.querySelectorAll('link[href^="/assets/css/themes/"]').length === 2, 'Signed-out visitors only need the site theme and dark mode styles.');
      await report('PASS');
      return;
    }
    assert(!new URL(location.href).searchParams.has('preview-theme'), 'Preview URL should be clean.');
    if (sessionStorage.getItem('theme-test-exit')) {
      assertPreviewExited();
      document.querySelector(`link[data-blog-theme="${baseTheme(siteThemeAfterSave)}"]`).disabled = true;
      const fallback = palettes[isDark(siteThemeAfterSave) ? 'default-dark' : 'default'];
      assert(getComputedStyle(root).backgroundColor === fallback.page, 'Missing theme should fall back to a readable palette.');
      assert(getComputedStyle(document.querySelector('.expandable-details')).backgroundColor === fallback.panel, 'Missing theme should retain component styling.');
      if (siteThemeAfterSave !== savedTheme) {
        const logout = await fetch('/auth/logout', {method: 'POST'});
        assert(logout.ok, 'Could not sign out for the preview access test.');
        sessionStorage.setItem('theme-test-exit', 'signed-out');
        sessionStorage.setItem('preview-theme', savedTheme);
        location.assign('/post/s/theme-preview?preview-theme=' + savedTheme);
        return;
      }
      await report('PASS');
      return;
    }
    assert(root.dataset.theme === savedTheme, 'Preview changed the configured theme.');
    assert(root.dataset.themePreview === expectedTheme && getComputedStyle(preview).display === 'flex', 'The selected preview is missing.');
    assert(getComputedStyle(document.querySelector('[data-appearance-toggle]')).display === 'none', 'The reader toggle should stay out of theme previews.');
    assert(getComputedStyle(root).backgroundColor === palette.page, 'Theme palette is missing.');
    const background = new Image();
    background.src = backgroundURL(expectedTheme);
    assert(artwork(expectedTheme).includes(background.src), 'The selected theme should use its own background.');
    assert(getComputedStyle(root).colorScheme === (isDark(expectedTheme) ? 'dark' : 'light'), 'Native controls have the wrong color scheme.');
    await background.decode();
    if (expectedTheme === 'christmas') {
      assert(getComputedStyle(document.querySelector('.site-sidebar')).backgroundColor === 'rgb(23, 68, 52)', 'Christmas pine header is missing.');
      assert(getComputedStyle(document.querySelector('.profile-name')).color === 'rgb(255, 246, 232)', 'Christmas header text should use ivory.');
    }
    if (location.pathname === '/') {
      const link = Array.from(document.querySelectorAll('[href="/post/s/theme-preview"]')).find(element => element.getClientRects().length);
      assert(link, 'Recent Posts is missing the sample article.');
      const end = Date.now() + 8000;
      link.click();
      while (location.pathname !== '/post/s/theme-preview' || !document.querySelector('.post-body > p')) {
        assert(Date.now() < end, 'Recent Posts link did not open the sample article.');
        await new Promise(resolve => setTimeout(resolve, 25));
      }
      assert(preview.isConnected, 'Recent Posts should use HTMX navigation.');
      location.reload();
      return;
    }
    assert(getComputedStyle(document.querySelector('.post-body > p')).fontFamily.includes('Source Sans 3'), 'Seasonal theme changed the reading font.');
    assert(getComputedStyle(document.querySelector('.expandable-details')).backgroundColor === palette.panel, 'Component palette is missing.');
    if (isDark(expectedTheme)) {
      assertContrast(document.querySelector('.post-body > p > a'), root);
      assertContrast(document.querySelector('.post-body blockquote'), root);
      assertContrast(document.querySelector('.expandable-details > summary'), document.querySelector('.expandable-details'));
      assertContrast(document.querySelector('.profile-name'), document.querySelector('.site-sidebar'));
      const administration = document.querySelector('.account-nav');
      const menu = administration.querySelector('.account-menu');
      const links = menu.querySelector('.action-menu-items');
      assert(getComputedStyle(administration).backgroundColor === palette.panel, 'Administration should use the active palette.');
      if (innerWidth >= 768) {
        assert(getComputedStyle(links).backgroundColor === 'rgba(0, 0, 0, 0)', 'Inline Administration links should share the bar surface.');
        assertContrast(links.querySelector('a'), administration);
      } else {
        const toggle = menu.querySelector('summary');
        toggle.click();
        assert(menu.open && links.getBoundingClientRect().height > 0, 'The Administration menu should open on mobile.');
        assert(getComputedStyle(links).backgroundColor === palette.panel, 'The mobile menu needs an opaque surface.');
        assertContrast(links.querySelector('a'), links);
        toggle.click();
      }
    }
    assert(root.scrollWidth <= innerWidth, 'Theme causes page overflow.');
    if (siteThemeAfterSave !== savedTheme) {
      const saved = await fetch('/test/save-theme', {method: 'POST'});
      assert(saved.ok && root.dataset.theme === savedTheme, 'Could not simulate a change from another tab.');
    }
    history.replaceState(history.state, '', '?source=preview#article-content');
    sessionStorage.setItem('theme-test-exit', 'yes');
    preview.querySelector('button').click();
  } catch (error) {
    await report(error.message + '\n' + error.stack);
  }
})();
