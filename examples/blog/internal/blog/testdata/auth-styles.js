// Compare real authentication responses in the blog layout without submitting credentials.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  try {
    document.cookie = 'blog_returning_user=1; Path=/; SameSite=Lax';
    const article = document.querySelector('#article-content');
    const baseline = getComputedStyle(document.querySelector('#login-form'));
    const properties = ['fontSize', 'lineHeight', 'border', 'borderRadius', 'backgroundColor', 'maxWidth'];
    const expected = Object.fromEntries(properties.map(property => [property, baseline[property]]));
    assert(expected.fontSize === '16px' && expected.lineHeight === '24px', 'Auth typography must not inherit article sizing.');
    assert(expected.backgroundColor === 'rgba(0, 0, 0, 0)', 'Auth forms must not use a separate card.');
    assert(expected.maxWidth === '440px', 'Auth fields must stay comfortably bounded.');
    for (const hx of [false, true]) {
      for (const [path, selector] of [
        ['/auth/login/email', '#login-form'],
        ['/auth/register/email', '#register-form'],
        ['/auth/reset/request/email', '#reset-request-form'],
        [__RESET_URL__, '#reset-password-form']
      ]) {
        const response = await fetch(path, {headers: hx ? {'HX-Request': 'true'} : {}});
        assert(response.ok, 'Auth response failed: ' + path);
        const document = new DOMParser().parseFromString(await response.text(), 'text/html');
        const form = document.querySelector(selector);
        assert(form, 'Auth form missing: ' + selector);
        if (selector === '#login-form' || selector === '#register-form') {
          assert(!document.querySelector('.account-nav a[href="/auth/login/email"]'), 'Login must not appear in the top panel on auth forms.');
          const links = document.querySelectorAll('a[href="/auth/login/email"]');
          assert(links.length === (selector === '#register-form' ? 1 : 0), 'Incorrect number of login links.');
        }
        article.replaceChildren(selector === '#register-form' ? form.parentElement : form);
        const style = getComputedStyle(form);
        assert(style.paddingTop === (form.matches('.auth-email-form') ? '32px' : '0px'), 'Incorrect space above the auth form.');
        for (const property of properties) {
          assert(style[property] === expected[property], selector + ' differs in ' + property);
        }
        assert(form.getBoundingClientRect().right <= innerWidth, 'Auth panel overflows the viewport.');
        const contentLeft = article.getBoundingClientRect().left + parseFloat(getComputedStyle(article).paddingLeft);
        const centered = form.matches('.auth-email-form') && innerWidth >= 768 && innerWidth < 1680;
        const expectedLeft = centered ? article.getBoundingClientRect().left + (article.getBoundingClientRect().width - form.getBoundingClientRect().width) / 2 : contentLeft;
        assert(Math.abs(form.getBoundingClientRect().left - expectedLeft) < 1, 'Incorrect auth form alignment for this viewport.');
        for (const input of form.querySelectorAll('input:not([type=hidden])')) {
          assert(input.getBoundingClientRect().height >= 44, 'Auth field is too small for touch.');
          assert(input.getBoundingClientRect().right <= form.getBoundingClientRect().right + 1, 'Auth field overflows its form.');
        }
        const button = form.querySelector('button[type=submit]');
        assert(getComputedStyle(button).marginTop === '28px', 'Auth action spacing differs.');
        assert(button.getBoundingClientRect().height >= 44, 'Auth button is too small for touch.');
        if (selector === '#register-form') {
          const link = article.querySelector('a[href="/auth/login/email"]');
          assert(!form.contains(link), 'Registration login link belongs below the form.');
          assert(Math.abs(link.getBoundingClientRect().left - form.getBoundingClientRect().left) < 1, 'Registration footer must align with its form.');
          assert(link.getBoundingClientRect().top >= button.getBoundingClientRect().bottom, 'Registration login link must sit below its button.');
        }
      }
    }
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
