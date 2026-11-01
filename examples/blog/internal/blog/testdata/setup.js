// Exercise actual forms and full-page redirects, including HTMX validation swaps.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 10000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate);
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  try {
    assert(document.documentElement.scrollWidth <= innerWidth, 'Setup overflows the viewport.');
    const stage = sessionStorage.getItem('setup-stage');
    if (!stage) {
      assert(location.pathname === '/auth/register/email', 'Fresh site must start at registration.');
      assert(document.querySelector('.setup-panel') && !document.querySelector('.site-sidebar'), 'Registration must use the setup layout.');
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      document.querySelector('#passwordMatch').value = 'Does-not-match-123!';
      document.querySelector('#register-form').requestSubmit();
      await wait(() => document.querySelector('.field-error'));
      const form = document.querySelector('#register-form');
      for (const input of form.querySelectorAll('input')) {
        assert(input.getBoundingClientRect().right <= innerWidth, 'Registration field overflows.');
        assert(input.getBoundingClientRect().height >= 44, 'Registration field is too small.');
      }
      form.elements.password.value = form.elements.passwordMatch.value = 'Sample-Password-123!';
      sessionStorage.setItem('setup-stage', 'registered');
      form.requestSubmit();
      return;
    }
    if (stage === 'registered') {
      assert(location.pathname === '/auth/login/email', 'Registration must lead to login.');
      assert(document.body.textContent.includes('Check your email'), 'Missing verification guidance.');
      const login = document.querySelector('.setup-login');
      assert(login && !login.open, 'Login should be secondary until verification.');
      login.querySelector('summary').click();
      assert(login.open && document.querySelector('#login-form').getBoundingClientRect().height > 0, 'Login must remain available for recovery.');
      const response = await fetch('/test/verification-link');
      assert(response.ok, 'Verification mail missing.');
      sessionStorage.setItem('setup-stage', 'verify');
      location.href = await response.text();
      return;
    }
    if (stage === 'verify') {
      sessionStorage.setItem('setup-stage', 'login');
      document.querySelector('#verify-account form').requestSubmit();
      return;
    }
    if (stage === 'login') {
      assert(location.pathname === '/auth/login/email', 'Verification must return to login.');
      for (const path of ['/', '/posts']) {
        const publicPage = await fetch(path);
        assert(publicPage.ok && new URL(publicPage.url).pathname === path, 'Verification must open public content: ' + path);
      }
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('setup-stage', 'profile');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'profile') {
      assert(location.pathname === '/profile', 'First login must open Site Profile.');
      const form = document.querySelector('#profile-form');
      assert(form.elements.name.value === 'My Blog', 'Default profile is missing.');
      assert(!document.querySelector('.setup-panel'), 'Wizard remains after login.');
      assert(document.body.textContent.includes('Images are optional.'), 'Missing initial profile guidance.');
      form.elements.name.value = 'A new blog';
      sessionStorage.setItem('setup-stage', 'saved');
      form.requestSubmit();
      return;
    }
    assert(stage === 'saved' && location.pathname === '/profile', 'First save must return to Site Profile.');
    const completion = document.querySelector('.setup-complete');
    assert(completion && completion.textContent.includes('Your site profile is saved.'), 'Missing profile confirmation.');
    assert(completion.querySelector('a[href="/"]') && completion.querySelector('a[href="/post/create"]'), 'Missing next steps.');
    assert(!document.body.textContent.includes('Images are optional.'), 'Initial guidance remains after saving.');
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
