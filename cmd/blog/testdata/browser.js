(async () => {
  // Deferred scripts can run before HTMX's DOMContentLoaded initialization.
  if (document.readyState !== 'complete') {
    await new Promise(resolve => window.addEventListener('load', resolve, {once: true}));
  }
  const report = text => fetch('/test/result', {method: 'POST', body: text});
  const check = (value, message) => { if (!value) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 15000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out waiting for the page');
      await new Promise(resolve => setTimeout(resolve, 30));
    }
  };
  const step = Number(sessionStorage.getItem('blog-test-step') || 0);
  const watchdog = setTimeout(() => report('No navigation or result after step ' + step), 20000);
  const next = () => sessionStorage.setItem('blog-test-step', String(step + 1));
  try {
    if (step === 0) {
      await fetch('/test/ready', {method: 'POST'});
      await wait(() => window.htmx);
      const email = document.querySelector('#email');
      check(getComputedStyle(email).paddingLeft === '10px', 'Login input lost its styling');
      check(getComputedStyle(document.body).fontFamily.includes('Source Sans 3'), 'Original font is missing');
      await document.fonts.ready;
      check(document.fonts.check('16px "Source Sans 3"'), 'Local font failed to load');
      const portrait = document.querySelector('.header-profile img');
      check(portrait.complete && portrait.naturalWidth > 0, 'Original profile photo failed to load');
      if (innerWidth < 1024) {
        check(getComputedStyle(document.querySelector('#posts-list')).display === 'none', 'Desktop menu visible on mobile');
        check(getComputedStyle(document.querySelector('#content-menu')).display !== 'none', 'Mobile menu hidden');
        check(getComputedStyle(document.querySelector('.navigation')).overflowY === 'visible', 'Mobile recent-post link clipped');
      } else {
        check(getComputedStyle(document.querySelector('.navigation')).position === 'fixed', 'Desktop sidebar is not fixed');
        check(getComputedStyle(document.querySelector('#content-menu')).display === 'none', 'Mobile menu visible on desktop');
      }
      email.value = 'writer@example.com';
      document.querySelector('#password').value = 'Blog-test-password1!';
      next(); document.querySelector('#login-form').requestSubmit();
    } else if (step === 1) {
      check(document.querySelector('.account-nav a').textContent === 'New Post', 'Admin navigation missing');
      next(); location.href = '/post/create';
    } else if (step === 2) {
      await wait(() => window.htmx);
      const editor = document.querySelector('.editor');
      editor.querySelector('#title').value = 'Browser post';
      editor.querySelector('#slug').value = '--';
      editor.querySelector('#markdown').value = '**Browser content**';
      const before = document.querySelector('.sidebar');
      const settled = new Promise(resolve => {
        const listener = event => {
          if (event.detail.target.id !== 'main') return;
          document.removeEventListener('htmx:afterSettle', listener);
          resolve();
        };
        document.addEventListener('htmx:afterSettle', listener);
      });
      editor.requestSubmit();
      await settled;
      await wait(() => document.querySelector('.field-error'));
      check(document.querySelector('.sidebar') === before, 'Invalid form replaced the page shell');
      document.querySelector('#slug').value = 'browser-post';
      document.querySelector('#markdown').value = 'x'.repeat(256 * 1024 + 1);
      document.querySelector('.editor').requestSubmit();
      await wait(() => document.querySelector('#request-status')?.textContent.includes('413'));
      check(document.querySelector('#markdown').value.length === 256 * 1024 + 1, 'Rejected request lost editor input');
      document.querySelector('#markdown').value = '**Browser content**';
      next(); document.querySelector('.editor').requestSubmit();
    } else if (step === 3) {
      check(document.querySelector('.prose strong').textContent === 'Browser content', 'Published Markdown missing');
      await wait(() => document.querySelector('#posts-list a[href="/post/s/browser-post"]'));
      check(document.querySelector('#content-menu a[href="/post/s/browser-post"]'), 'Mobile recent posts missing');
      next(); location.href = document.querySelector('.post-actions a').href;
    } else if (step === 4) {
      await wait(() => window.htmx);
      document.querySelector('#title').value = 'Revised browser post';
      next(); document.querySelector('.editor').requestSubmit();
    } else if (step === 5) {
      check(document.querySelector('h1').textContent === 'Revised browser post', 'Edit was not saved');
      next(); location.href = document.querySelector('.post-actions a:nth-child(2)').href;
    } else if (step === 6) {
      next(); document.querySelector('main form').requestSubmit();
    } else if (step === 7) {
      check(document.querySelector('h1').textContent.includes('first post'), 'Delete did not remove post');
      next(); document.querySelector('.account-nav form').requestSubmit();
    } else if (step === 8) {
      check(document.querySelector('#footer a[href="/auth/login/email"]').textContent === 'Log in', 'Logout navigation incorrect');
      next(); location.href = '/post/create';
    } else if (step === 9) {
      check(location.pathname === '/auth/login/email', 'Logged-out editor access was not rejected');
      clearTimeout(watchdog);
      await report('PASS');
    } else {
      throw new Error('Unexpected browser step ' + step);
    }
  } catch (error) {
    clearTimeout(watchdog);
    await report('Step ' + step + ': ' + error.message);
  }
})();
