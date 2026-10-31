// Management links replace the article without reloading the shared page shell.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate);
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  window.addEventListener('error', event => report(event.message));
  try {
    if (!sessionStorage.getItem('admin-navigation-login')) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('admin-navigation-login', 'true');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    assert(!sessionStorage.getItem('admin-navigation-started'), 'Management navigation reloaded the document.');
    sessionStorage.setItem('admin-navigation-started', 'true');
    await wait(() => document.querySelector('#post-archive .post-archive') && !document.querySelector('#post-archive').classList.contains('htmx-settling'));
    const shell = ['body', '.site-sidebar', '.account-panel', '#article-content', '#post-archive', '#post-archive .post-archive'].map(selector => [selector, document.querySelector(selector)]);
    const article = document.querySelector('#article-content');
    const account = document.querySelector('.account-menu');
    const archiveRequests = () => performance.getEntriesByType('resource').filter(entry => new URL(entry.name).pathname === '/posts/recent').length;
    const initialRequests = archiveRequests();
    const unchanged = () => {
      for (const [selector, element] of shell) assert(document.querySelector(selector) === element, 'Navigation replaced ' + selector);
      assert(archiveRequests() === initialRequests, 'Management navigation reloaded Recent Posts.');
      assert(!document.querySelector('dialog[open]'), 'Management navigation opened a dialog.');
      assert(!article.querySelector('html, body, .site-layout'), 'Management fragment included the page shell.');
    };
    const ready = async (path, title) => {
      await wait(() => location.pathname === path && article.querySelector('h1')?.textContent === title && !article.classList.contains('htmx-settling'));
      assert(document.title === 'Trent Nix | ' + title, 'Management navigation lost the document title.');
      unchanged();
    };
    for (const [path, title] of [['/post/manage', 'Posts'], ['/pages', 'Pages'], ['/profile', 'Site Profile'], ['/media', 'Media']]) {
      if (innerWidth < 768) account.querySelector('summary').click();
      account.querySelector(`a[href="${path}"]`).click();
      await ready(path, title);
      if (innerWidth < 768) assert(!account.open, 'Management navigation left the mobile menu open.');
      if (path === '/profile') assert(!article.querySelector('.image-upload-controls').hidden, 'Profile image controls were not initialized after navigation.');
      if (path === '/media') assert(!article.querySelector('[data-media-upload]').hidden, 'Media upload was not initialized after navigation.');
    }
    history.back();
    await ready('/profile', 'Site Profile');
    assert(!article.querySelector('.image-upload-controls').hidden, 'History navigation did not initialize profile controls.');
    history.forward();
    await ready('/media', 'Media');
    assert(!article.querySelector('[data-media-upload]').hidden, 'History navigation did not initialize media upload.');
    assert(!document.querySelector('.account-nav form').closest('[hx-boost="true"]'), 'Logout must retain ordinary navigation.');
    await report('PASS');
  } catch (error) {
    await report(error.stack || String(error));
  }
})();
