// Check focus and accessible control names in the real, enhanced forms.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  window.addEventListener('error', event => report(event.message));
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate);
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  const visible = element => element.getClientRects().length && getComputedStyle(element).visibility !== 'hidden';
  const checkControls = () => {
    const ids = [...document.querySelectorAll('[id]')].map(element => element.id);
    assert(new Set(ids).size === ids.length, 'The document contains duplicate IDs.');
    for (const control of document.querySelectorAll('input, textarea, select, button')) {
      if (!visible(control) || control.type === 'hidden') continue;
      const labelled = (control.getAttribute('aria-labelledby') || '').split(/\s+/).filter(Boolean);
      const name = control.getAttribute('aria-label') || labelled.map(id => document.getElementById(id)?.textContent).join(' ') || [...(control.labels || [])].map(label => label.textContent).join(' ') || (control.tagName === 'BUTTON' ? control.textContent : '');
      assert(name.trim(), 'Control has no accessible name: ' + control.outerHTML);
      for (const id of (control.getAttribute('aria-describedby') || '').split(/\s+/).filter(Boolean)) {
        assert(document.getElementById(id), 'Missing help text: ' + id);
      }
    }
    assert(document.documentElement.scrollWidth <= innerWidth, 'Content overflows the viewport.');
  };
  try {
    const skip = document.querySelector('.skip-link');
    assert(skip && document.querySelector('body > a') === skip, 'Skip link must precede navigation.');
    skip.focus();
    assert(skip.getBoundingClientRect().top >= 0, 'Focused skip link is not visible.');
    if (window.testPressKey) await window.testPressKey('Enter');
    else skip.click();
    await wait(() => document.activeElement.id === 'article-content');
    checkControls();
    if (!sessionStorage.getItem('accessibility-login')) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('accessibility-login', 'true');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    const article = document.querySelector('#article-content');
    const navigate = async (link, path) => {
      link.focus();
      link.click();
      await wait(() => location.pathname === path && !article.classList.contains('htmx-settling'));
      assert(document.activeElement === article.querySelector('h1'), 'Navigation must focus the new heading: ' + path);
      checkControls();
    };
    const account = document.querySelector('.account-menu');
    for (const path of ['/post/manage', '/pages', '/profile', '/media']) {
      if (innerWidth < 768) account.querySelector('summary').click();
      await navigate(account.querySelector(`a[href="${path}"]`), path);
      if (innerWidth < 768) assert(!account.open, 'Mobile account menu must close after navigation.');
    }
    // Both editors share the same Markdown integration.
    for (const [manager, create] of [['/post/manage', '/post/create'], ['/pages', '/pages/create']]) {
      if (innerWidth < 768) account.querySelector('summary').click();
      await navigate(account.querySelector(`a[href="${manager}"]`), manager);
      await navigate(article.querySelector(`a[href="${create}"]`), create);
      await wait(() => document.querySelector('#markdown-input'));
      checkControls();
      const input = document.querySelector('#markdown-input');
      assert(input.getAttribute('aria-required') === (manager === '/post/manage' ? 'true' : null), 'The enhanced editor must preserve required-field semantics.');
      const label = document.querySelector('#markdown-label');
      label.click();
      assert(document.activeElement === input, 'Markdown label must focus the enhanced editor.');
      const editor = document.querySelector('.CodeMirror').CodeMirror;
      const before = editor.getValue();
      for (const shiftKey of [false, true]) {
        const event = new KeyboardEvent('keydown', {key: 'Tab', code: 'Tab', keyCode: 9, which: 9, shiftKey, bubbles: true, cancelable: true});
        input.dispatchEvent(event);
        assert(!event.defaultPrevented, 'Markdown must not capture Tab or Shift+Tab.');
        // A local WebDriver adapter can additionally exercise native tab order.
        if (window.testPressKey) {
          input.focus();
          await window.testPressKey(shiftKey ? 'Shift+Tab' : 'Tab');
          assert(document.activeElement !== input, 'Native Tab must leave the Markdown editor.');
        }
      }
      assert(editor.getValue() === before, 'Tab navigation changed Markdown content.');
      const browse = article.querySelector('[data-media-browse]');
      browse.focus();
      browse.click();
      await wait(() => document.querySelector('.media-browser[open]'));
      const dialog = document.querySelector('.media-browser');
      assert(dialog.contains(document.activeElement), 'Image chooser must receive focus.');
      if (window.testPressKey) {
        await window.testPressKey('Shift+Tab');
        // Native dialogs may cycle through browser chrome, not the inert page.
        assert(document.activeElement === document.body || dialog.contains(document.activeElement), 'Focus escaped to a background control.');
        await window.testPressKey('Tab');
        assert(dialog.contains(document.activeElement), 'Focus must stay inside the image chooser.');
        await window.testPressKey('Escape');
      } else {
        dialog.querySelector('[data-close-dialog]').click();
      }
      await wait(() => !dialog.isConnected);
      assert(document.activeElement === browse, 'Closing the image chooser must restore focus.');
    }
    document.querySelector('.profile-name').click();
    await wait(() => location.pathname === '/' && !article.classList.contains('htmx-settling'));
    assert(!article.querySelector('h1') && document.activeElement === article, 'Headingless responses must focus the article instead.');
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
