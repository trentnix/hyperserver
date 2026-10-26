// Compare real authentication responses in the blog layout without submitting credentials.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  try {
    const article = document.querySelector('#article-content');
    const baseline = getComputedStyle(document.querySelector('#login-form'));
    const properties = ['fontSize', 'lineHeight', 'padding', 'border', 'borderRadius', 'backgroundColor', 'maxWidth'];
    const expected = Object.fromEntries(properties.map(property => [property, baseline[property]]));
    assert(expected.fontSize === '16px' && expected.lineHeight === '24px', 'Auth typography must not inherit article sizing.');
    assert(expected.padding === (innerWidth < 768 ? '20px' : '28px'), 'Auth panel padding is incorrect.');
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
        article.replaceChildren(form);
        const style = getComputedStyle(form);
        for (const property of properties) {
          assert(style[property] === expected[property], selector + ' differs in ' + property);
        }
        assert(form.getBoundingClientRect().right <= innerWidth, 'Auth panel overflows the viewport.');
        const button = form.querySelector('button[type=submit]');
        assert(getComputedStyle(button).marginTop === '22px', 'Auth action spacing differs.');
      }
    }
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
