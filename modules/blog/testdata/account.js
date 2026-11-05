// Account should match the compact method list on full loads and partial swaps.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate);
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  try {
    await document.fonts.ready;
    const article = document.querySelector('#article-content');
    const checkLayout = () => {
      const methods = article.querySelector('.account-methods');
      assert(getComputedStyle(methods).listStyleType === 'none', 'Account must not render as a bulleted list.');
      assert(parseFloat(getComputedStyle(article.querySelector('.account-methods-heading')).fontSize) === 16, 'Account section heading is too large.');
      const heading = article.querySelector('.account-method-heading');
      const status = heading.querySelector('.account-method-status');
      assert(getComputedStyle(heading).display === 'flex', 'Method name and status must share a row.');
      assert(status.getBoundingClientRect().left > heading.querySelector('h3').getBoundingClientRect().right, 'Status must align to the right of the method name.');
      assert(getComputedStyle(status).backgroundColor === 'rgb(239, 245, 240)', 'Connected badge lost its background.');
      assert(getComputedStyle(methods.firstElementChild).borderBottomStyle === 'solid', 'Methods need subtle separators.');
      assert(article.textContent.includes('You can sign in with Google.'), 'Connected method needs an explanation.');
      for (const element of article.querySelectorAll('.auth-account, .account-method-action, .account-action-panel')) {
        const box = element.getBoundingClientRect();
        assert(box.left >= 0 && box.right <= innerWidth, 'Account overflows the viewport.');
      }
      const account = article.querySelector('.auth-account').getBoundingClientRect();
      assert(account.width <= 440, 'Account should retain a readable width.');
    };
    checkLayout();
    const originalAccount = article.querySelector('.auth-account');
    const originalTitle = document.title;
    article.querySelector('a[href="/auth/password/setup/email"]').click();
    await wait(() => article.querySelector('.account-action-panel form') && !article.querySelector('.htmx-settling'));
    assert(article.querySelector('.auth-account') === originalAccount, 'Add password must expand in place.');
    assert(location.pathname === '/auth/account' && document.title === originalTitle, 'Inline setup must preserve Account navigation.');
    assert(article.querySelectorAll('h1').length === 1, 'Inline setup must not nest a page heading.');
    checkLayout();
    article.querySelector('.account-action-panel form').requestSubmit();
    await wait(() => article.querySelector('.account-action-panel [role=status]') && !article.querySelector('.htmx-settling'));
    assert(article.querySelector('.account-action-panel').textContent.includes('Check your email.'), 'Setup must show delivery confirmation inline.');
    assert(article.querySelector('.auth-account') === originalAccount, 'Sending setup mail must preserve Account.');
    checkLayout();

    const setupLink = await (await fetch('/test/password-setup-link')).text();
    const fullPage = new DOMParser().parseFromString(await (await fetch(setupLink)).text(), 'text/html');
    assert(fullPage.querySelector('#reset-password.auth-form-panel'), 'The email link must open the shared auth panel.');
    await htmx.ajax('GET', setupLink, {target: article, swap: 'innerHTML'});
    await wait(() => article.querySelector('#reset-password-form') && !article.classList.contains('htmx-settling'));
    const checkSetup = () => {
      const panel = article.querySelector('.auth-form-panel');
      const bounds = panel.getBoundingClientRect();
      assert(bounds.width <= 440 && bounds.left >= 0 && bounds.right <= innerWidth, 'Password setup must fit a compact panel.');
      assert(parseFloat(getComputedStyle(panel.querySelector('h1')).fontSize) === 28, 'Password setup heading must match other auth panels.');
      assert(getComputedStyle(panel).borderTopWidth === '0px', 'Auth panels must not have a container border.');
      assert(getComputedStyle(panel).boxShadow === 'none', 'Auth panels must retain the flat site style.');
      assert(panel.querySelector('h1').textContent === 'Create a password', 'Password enrollment must use creation wording.');
      assert(panel.querySelector('button[type=submit]').textContent === 'Create password', 'The password action must match its heading.');
      const form = panel.querySelector('form');
      assert(getComputedStyle(form).paddingTop === '0px', 'Password setup has excess space above its fields.');
      assert(form.elements.password.getAttribute('aria-describedby').includes('password-hint'), 'Password guidance must be associated with its field.');
      assert(panel.querySelector('a[href="/auth/account"]')?.textContent === 'Cancel', 'Password setup needs a way back to Account.');
      for (const control of form.querySelectorAll('input, button')) {
        const box = control.getBoundingClientRect();
        assert(box.height >= 44 && box.right <= bounds.right, 'Password controls must remain touch-friendly and fit the panel.');
      }
      if (innerWidth >= 768 && innerWidth < 1680) {
        const content = article.getBoundingClientRect();
        assert(Math.abs((bounds.left + bounds.right) - (content.left + content.right)) < 2, 'Password setup must be centered at medium widths.');
      }
    };
    checkSetup();
    const form = article.querySelector('#reset-password-form');
    form.elements.password.value = 'short';
    form.elements.passwordMatch.value = 'short';
    form.requestSubmit();
    await wait(() => article.querySelector('#password-error') && !article.querySelector('.htmx-settling'));
    checkSetup();
    assert(article.querySelectorAll('.auth-form-panel').length === 1, 'Validation must replace the panel without nesting it.');
    assert(article.querySelector('#password').value === '', 'Validation must not echo the password.');
    assert(article.querySelector('#password').getAttribute('aria-describedby').includes('password-error'), 'Validation must retain the error association.');
    await fetch('/test/result', {method: 'POST', body: 'PASS'});
  } catch (error) {
    await fetch('/test/result', {method: 'POST', body: error.stack || String(error)});
  }
})();
