(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const root = document.documentElement;
  const toggle = document.querySelector('[data-appearance-toggle]');
  const setting = 'blog-color-mode';
  const siteDark = siteTheme.endsWith('-dark');
  const base = siteTheme.replace(/-dark$/, '');
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assertMode = dark => {
    assert(root.dataset.theme === base + (dark ? '-dark' : ''), 'Reader mode changed the theme or failed to take effect.');
    assert(getComputedStyle(root).colorScheme === (dark ? 'dark' : 'light'), 'The styles did not follow the reader mode.');
    assert(toggle.getAttribute('aria-pressed') === String(dark), 'The toggle did not report the active mode.');
    const channels = getComputedStyle(toggle).backgroundColor.match(/\d+/g).slice(0, 3).map(Number);
    assert(channels.every(channel => dark ? channel > 180 : channel < 90), 'The button should reflect the mode it switches to.');
  };
  try {
    assert(!document.querySelector('.account-menu'), 'Reader test must run signed out.');
    assert(toggle instanceof HTMLButtonElement && !toggle.hidden, 'Readers need an appearance button.');
    const box = toggle.getBoundingClientRect();
    const rightInset = root.clientWidth - box.right;
    assert(rightInset >= 0 && rightInset < 24 && innerHeight - box.bottom >= 0 && innerHeight - box.bottom < 24 && box.width >= 44 && box.height >= 44,
      'The button should be a touch-sized control in the lower-right corner.');
    assert(document.querySelector('#notifications').getBoundingClientRect().bottom < box.top, 'Notifications should leave the appearance control clear.');

    if (!sessionStorage.getItem('appearance-reload')) {
      assert(localStorage.getItem(setting) === null, 'A fresh reader should start with the site default.');
      assertMode(siteDark);
      toggle.click();
      assertMode(!siteDark);
      assert(localStorage.getItem(setting) === (siteDark ? 'light' : 'dark'), 'The button did not remember the choice.');
      sessionStorage.setItem('appearance-reload', 'yes');
      location.reload();
      return;
    }
    assertMode(!siteDark);
    const link = [...document.querySelectorAll('.archive-nav a[href="/post/s/appearance"]')].find(a => a.getClientRects().length);
    assert(link, 'The post link is missing.');
    link.click();
    const deadline = Date.now() + 8000;
    while (location.pathname !== '/post/s/appearance') {
      assert(Date.now() < deadline, 'Recent Posts did not open the article.');
      await new Promise(resolve => setTimeout(resolve, 25));
    }
    assert(toggle.isConnected, 'HTMX navigation replaced the appearance control.');
    assertMode(!siteDark);
    toggle.click();
    assertMode(siteDark);
    await report('PASS');
  } catch (error) {
    await report(error.message + '\n' + error.stack);
  }
})();
