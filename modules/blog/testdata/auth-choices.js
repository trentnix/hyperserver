// Check login and registration controls without sending a browser to Google.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  try {
    await document.fonts.ready;
    const root = document.documentElement;
    assert(getComputedStyle(root).scrollbarGutter === (innerWidth < 768 ? 'auto' : 'stable'), 'Only desktop layouts should reserve scrollbar space.');
    if (innerWidth < 768) {
      // Exercise both states even when the browser uses overlay scrollbars.
      root.style.overflowY = 'hidden';
      assert(document.body.getBoundingClientRect().width === innerWidth, 'Mobile pages without a scrollbar must fill the viewport.');
      root.style.overflowY = 'scroll';
      assert(document.body.getBoundingClientRect().width === Math.max(320, root.clientWidth), 'Mobile pages must use the available width, subject to the 320px layout minimum.');
      root.style.removeProperty('overflow-y');
    }
    const email = new URLSearchParams(location.search).get('email') === 'true';
    const registering = location.pathname === '/register';
    const flow = registering ? 'register' : 'login';
    const form = document.querySelector('#' + flow + '-form');
    assert(!!form === email, 'Email visibility does not match configuration.');
    if (!registering && innerWidth < 1680) {
      const panel = document.querySelector('.auth-login-panel, .auth-provider').getBoundingClientRect();
      const article = document.querySelector('#article-content').getBoundingClientRect();
      assert(Math.abs((panel.left + panel.right) / 2 - (article.left + article.right) / 2) < 1, 'Login cards must stay horizontally centered, including when their maximum width takes effect.');
    }
    const googleSelector = 'form:has(input[name="provider"][value="google"])';
    const google = document.querySelector(googleSelector);
    assert(google && google.method === 'post', 'Google sign-in control missing.');
    assert(google.getAttribute('hx-boost') === 'false', 'Google must use full browser navigation.');
    const button = google.querySelector('button');
    assert(button.textContent.trim() === (registering ? 'Sign up with Google' : 'Sign in with Google'), 'Google button has the wrong label.');
    const icon = button.querySelector('img');
    assert(icon && icon.getAttribute('alt') === '', 'Google button needs a decorative brand icon.');
    await icon.decode();
    assert(icon.naturalWidth > 0, 'Google icon failed to load.');
    assert(getComputedStyle(button).backgroundColor === 'rgb(255, 255, 255)', 'Google button should use the light brand style.');
    assert(getComputedStyle(button).display === 'flex', 'Google icon and label should share a row.');
    const bounds = button.getBoundingClientRect();
    assert(bounds.height >= 44 && bounds.width >= 120, 'Google button is too small for touch.');
    assert(bounds.left >= 0 && bounds.right <= innerWidth, 'Google button overflows.');
    assert(document.documentElement.scrollWidth <= innerWidth, 'Login causes horizontal scrolling.');
    if (!email && !registering) {
      const panel = google.closest('.auth-provider');
      const panelBounds = panel.getBoundingClientRect();
      const heading = panel.querySelector('h1').getBoundingClientRect();
      assert(panelBounds.width <= 400, 'Standalone login panel is too wide.');
      assert(panel.querySelector('h1').textContent === 'Welcome back', 'Login should have a welcoming heading rather than repeating the button.');
      assert(panel.querySelector('.auth-introduction').textContent.includes('manage your site'), 'Login should explain its purpose.');
      assert(bounds.top - heading.bottom >= 18, 'Login heading crowds the Google button.');
      assert(bounds.bottom <= panelBounds.bottom - 20, 'Login panel needs space below its button.');
      assert(bounds.height <= 48, 'Google button wraps in the compact panel.');
      if (innerWidth >= 768 && innerWidth < 1680) {
        const article = document.querySelector('#article-content').getBoundingClientRect();
        assert(Math.abs((panelBounds.left + panelBounds.right) / 2 - (article.left + article.right) / 2) < 1, 'Login panel should be centered at medium widths.');
      }
    }
    if (registering) {
      const panel = document.querySelector('.setup-panel');
      assert(panel, 'Registration should exercise the onboarding layout.');
      assert(panel.getBoundingClientRect().height <= 640, 'Setup has excessive vertical spacing.');
      if (form) {
        for (const control of form.querySelectorAll('input:not([type=hidden]), button')) {
          assert(control.getBoundingClientRect().height >= 44, 'Compact setup controls must remain touch-friendly.');
        }
        assert(parseFloat(getComputedStyle(form.querySelector('label[for=password]')).marginTop) <= 16, 'Setup field spacing is too large.');
        const headingBottom = panel.querySelector('header').getBoundingClientRect().bottom;
        assert(form.querySelector('label').getBoundingClientRect().top - headingBottom <= 24, 'Too much space above the registration fields.');
      }
    }
    if (email) {
      assert(!form.contains(google), 'Google form must not be nested inside the email form.');
      const formBounds = form.getBoundingClientRect();
      assert(Math.abs(formBounds.left - bounds.left) < 1, 'Login methods are misaligned.');
      assert(Math.abs(formBounds.width - bounds.width) < 1, 'Login methods have inconsistent widths.');
      if (!registering) {
        const panel = form.closest('.auth-login-panel');
        assert(panel.querySelector('h1')?.textContent === 'Welcome back', 'Email and external login should share the welcome heading.');
        assert(panel.getBoundingClientRect().width <= 440, 'Combined login panel is too wide.');
        const actions = panel.querySelector('.auth-email-actions');
        const password = form.querySelector('input[name=password]').getBoundingClientRect();
        const submit = actions.querySelector('button').getBoundingClientRect();
        assert(submit.top - password.bottom >= 16 && submit.top - password.bottom <= 24, 'Email action has excessive vertical spacing.');
        assert(bounds.bottom <= panel.getBoundingClientRect().bottom - 16, 'Login panel needs space below provider buttons.');
      }
      form.elements.email.value = registering ? 'invalid' : 'trentnix@gmail.com';
      form.elements.password.value = 'invalid';
      const response = await fetch(form.action, {method: 'POST', body: new URLSearchParams(new FormData(form)), headers: {'HX-Request': 'true'}});
      const fragment = new DOMParser().parseFromString(await response.text(), 'text/html');
      assert(fragment.querySelector(googleSelector), 'Validation removes the Google alternative.');
      if (registering) {
        form.elements.passwordMatch.value = 'different';
        form.requestSubmit();
        const deadline = Date.now() + 12000;
        while (!document.querySelector('#email-error') || document.querySelector('.htmx-settling, .htmx-request')) {
          if (Date.now() > deadline) throw new Error('Registration validation did not render.');
          await new Promise(resolve => setTimeout(resolve, 25));
        }
        const input = document.querySelector('#email');
        assert(input.getAttribute('aria-invalid') === 'true' && input.getAttribute('aria-describedby') === 'email-error', 'Registration must associate field errors.');
        input.focus();
        const style = getComputedStyle(input);
        assert(style.borderTopColor === 'rgb(161, 38, 38)' && style.outlineColor === 'rgb(161, 38, 38)' && style.outlineOffset === '0px', 'Registration must use the shared red error and focus styling.');
        input.value = 'corrected@example.com';
        input.dispatchEvent(new Event('input', {bubbles: true}));
        assert(!input.hasAttribute('aria-invalid') && !document.querySelector('#email-error'), 'Editing must clear registration feedback.');
        assert(document.documentElement.scrollWidth <= innerWidth, 'Registration errors must not overflow.');
      }
    }
    await fetch('/test/result', {method: 'POST', body: 'PASS'});
  } catch (error) {
    await fetch('/test/result', {method: 'POST', body: String(error.stack || error)});
  }
})();
