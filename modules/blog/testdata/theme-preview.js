(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const root = document.documentElement;
  const preview = document.querySelector('.theme-preview');
  const palette = {
    thanksgiving: {page: 'rgb(255, 250, 242)', panel: 'rgb(248, 238, 224)'},
    christmas: {page: 'rgb(255, 253, 249)', panel: 'rgb(239, 244, 239)'},
  }[expectedTheme];
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  try {
    assert(root.dataset.theme === 'default', 'Preview changed the configured theme.');
    assert(!new URL(location.href).searchParams.has('preview-theme'), 'Preview URL should be clean.');
    if (sessionStorage.getItem('theme-test-exit')) {
      assert(location.pathname === '/post/s/theme-preview', 'Exit left the current article.');
      assert(!root.dataset.themePreview && getComputedStyle(preview).display === 'none', 'Exit did not restore the configured theme.');
      assert(getComputedStyle(root).backgroundColor === 'rgb(255, 255, 255)', 'Exit did not restore the original palette.');
      assert(getComputedStyle(document.body).backgroundImage.includes('/assets/img/site-bg.jpg'), 'Exit did not restore the Default background.');
      await report('PASS');
      return;
    }
    assert(root.dataset.themePreview === expectedTheme && getComputedStyle(preview).display === 'flex', 'The selected preview is missing.');
    assert(getComputedStyle(root).backgroundColor === palette.page, 'Theme palette is missing.');
    const background = new Image();
    background.src = '/assets/img/site-bg-' + expectedTheme + '.webp';
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
    sessionStorage.setItem('theme-test-exit', 'yes');
    preview.querySelector('a').click();
  } catch (error) {
    await report(error.message + '\n' + error.stack);
  }
})();
