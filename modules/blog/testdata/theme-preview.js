(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const root = document.documentElement;
  const preview = document.querySelector('.theme-preview');
  const palettes = {
    default: {page: 'rgb(255, 255, 255)', panel: 'rgb(244, 246, 249)'},
    thanksgiving: {page: 'rgb(255, 250, 242)', panel: 'rgb(248, 238, 224)'},
    christmas: {page: 'rgb(255, 253, 249)', panel: 'rgb(239, 244, 239)'},
  };
  const palette = palettes[expectedTheme];
  const siteThemeAfterSave = savedTheme === 'default' ? savedTheme : expectedTheme;
  const backgroundURL = theme => theme === 'default' ? '/assets/img/site-bg.jpg' : '/assets/img/site-bg-' + theme + '.webp';
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assertPreviewExited = () => {
    assert(location.pathname + location.search + location.hash === '/post/s/theme-preview?source=preview#article-content', 'Exit changed the article URL.');
    assert(!root.dataset.themePreview && getComputedStyle(preview).display === 'none', 'Exit did not restore the configured theme.');
    assert(root.dataset.theme === siteThemeAfterSave, 'Exit did not pick up the latest saved theme.');
    assert(getComputedStyle(root).backgroundColor === palettes[siteThemeAfterSave].page, 'Exit did not restore the saved palette.');
    assert(getComputedStyle(document.body).backgroundImage.includes(backgroundURL(siteThemeAfterSave)), 'Exit did not restore the saved background.');
  };
  try {
    if (sessionStorage.getItem('theme-test-exit') === 'signed-out') {
      assert(!preview && !root.dataset.themePreview, 'Signed-out visitor can preview an unpublished theme.');
      assert(root.dataset.theme === siteThemeAfterSave && getComputedStyle(root).backgroundColor === palettes[siteThemeAfterSave].page, 'Signed-out visitor lost the saved theme.');
      assert(document.querySelectorAll('link[href^="/assets/css/themes/"]').length === 1, 'Signed-out visitor got alternate theme stylesheets.');
      await report('PASS');
      return;
    }
    assert(!new URL(location.href).searchParams.has('preview-theme'), 'Preview URL should be clean.');
    if (sessionStorage.getItem('theme-test-exit')) {
      assertPreviewExited();
      document.querySelector(`link[data-blog-theme="${siteThemeAfterSave}"]`).disabled = true;
      assert(getComputedStyle(root).backgroundColor === palettes.default.page && getComputedStyle(root).color === 'rgb(33, 37, 41)', 'Missing theme should fall back to a readable palette.');
      assert(getComputedStyle(document.querySelector('.expandable-details')).backgroundColor === palettes.default.panel, 'Missing theme should retain component styling.');
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
    assert(getComputedStyle(root).backgroundColor === palette.page, 'Theme palette is missing.');
    const background = new Image();
    background.src = backgroundURL(expectedTheme);
    assert(getComputedStyle(document.body).backgroundImage.includes(background.src), 'The selected theme should use its own background.');
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
