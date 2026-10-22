// Track document listeners independently of editor DOM nodes. Detached editors
// must not remain reachable through EasyMDE's keyboard handler.
const editorKeyListeners = new Set();
const originalAddListener = document.addEventListener.bind(document);
const originalRemoveListener = document.removeEventListener.bind(document);
document.addEventListener = function(type, callback, options) {
  if (type === 'keydown') editorKeyListeners.add(callback);
  return originalAddListener(type, callback, options);
};
document.removeEventListener = function(type, callback, options) {
  if (type === 'keydown') editorKeyListeners.delete(callback);
  return originalRemoveListener(type, callback, options);
};

(async () => {
  // HTMX binds its initial handlers on DOMContentLoaded, after deferred scripts.
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const end = Date.now() + 12000;
    while (Date.now() < end) {
      if (predicate()) return;
      await new Promise(resolve => setTimeout(resolve, 25));
    }
    throw new Error('Timed out at ' + location.pathname + ': ' + predicate.toString());
  };
  window.alert = message => report('Unexpected editor alert: ' + message);
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    const stage = sessionStorage.getItem('blog-test-stage');
    if (!stage) {
      await wait(() => document.querySelector('.archive-nav .post-archive'));
      const profile = document.querySelector('.profile');
      const portrait = profile.querySelector('img').getBoundingClientRect();
      const bio = document.querySelector('.profile-details').getBoundingClientRect();
      const article = document.querySelector('article').getBoundingClientRect();
      const archive = document.querySelector('.archive-nav').getBoundingClientRect();
      assert(portrait.right <= bio.left, 'Profile photo must sit beside the bio.');
      assert(Math.abs(portrait.top - bio.top) <= 5, 'Profile photo and bio are not top-aligned.');
      assert(getComputedStyle(document.body).fontSize === '16px', 'Base typography differs from the original.');
      assert(document.documentElement.scrollWidth <= innerWidth, 'Page overflows horizontally.');
      if (innerWidth < 1024) {
        assert(portrait.width === 80, 'Mobile portrait has the wrong size.');
        assert(archive.top >= article.bottom, 'Mobile archive must follow the article.');
        assert(document.querySelector('footer').getBoundingClientRect().top >= archive.bottom, 'Mobile footer must follow the archive.');
        assert(getComputedStyle(document.querySelector('.archive-jump')).display !== 'none', 'Mobile archive shortcut is hidden.');
      } else {
        const sidebar = document.querySelector('.site-sidebar');
        assert(getComputedStyle(sidebar).position === 'sticky', 'Desktop sidebar must stay visible while reading.');
        assert(archive.right <= article.left, 'Desktop archive is outside the sidebar.');
        assert(portrait.width === (innerWidth < 1280 ? 100 : 120), 'Desktop portrait has the wrong size.');
        if (innerWidth >= 1280 && innerWidth <= 1920) {
          assert(sidebar.getBoundingClientRect().width === Math.max(420, innerWidth - 1280), 'Desktop columns differ from the original.');
        }
      }
      const email = document.querySelector('#email');
      const password = document.querySelector('#password');
      assert(email && password, 'Missing login controls.');
      assert(Math.abs(email.getBoundingClientRect().width - password.getBoundingClientRect().width) < 1, 'Email and password widths differ.');
      assert(getComputedStyle(email).paddingLeft === '12px', 'Email field has no blog styling.');
      email.value = 'draft@example.test';
      password.value = 'unsaved password';
      await htmx.ajax('POST', '/test/plain-error', {target: '#login'});
      const notice = document.querySelector('#login [data-request-error]');
      assert(notice?.textContent.includes('<img'), 'Plain-text error was not displayed.');
      assert(!notice.querySelector('img') && !window.errorInjected, 'Plain text was interpreted as HTML.');
      assert(document.querySelector('#email') === email && email.value === 'draft@example.test' && password.value === 'unsaved password', 'Error discarded login inputs.');
      email.value = 'trentnix@gmail.com';
      password.value = 'Sample-Password-123!';
      sessionStorage.setItem('blog-test-stage', 'editor');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'editor') {
      const link = document.querySelector('a[href="/post/create"]');
      assert(link, 'Verified administrator has no New post link.');
      link.click();
      await wait(() => document.querySelector('dialog[open] .CodeMirror'));
      const cm = document.querySelector('.CodeMirror').CodeMirror;
      assert(document.querySelector('.CodeMirror').getBoundingClientRect().width > 200, 'Editor initialized while hidden.');
      document.querySelector('#post-title').value = 'Browser post';
      document.querySelector('#post-slug').value = 'browser-post';
      cm.setValue('# Preview heading');
      document.querySelector('.tool-preview').click();
      await wait(() => document.querySelector('.editor-preview h1')?.textContent === 'Preview heading');
      document.querySelector('.tool-preview').click();

      // Empty Markdown must be rejected by the server without replacing the editor.
      cm.setValue('');
      document.querySelector('#post-form').requestSubmit();
      await wait(() => document.querySelector('#editor-errors').textContent.includes('valid Markdown'));
      assert(document.querySelector('.CodeMirror').CodeMirror === cm, 'Save failure replaced the editor.');
      cm.setValue('# Saved heading\n\n');
      cm.setCursor(cm.lineCount(), 0);

      // Exercise EasyMDE's actual upload control, not a separate fetch shortcut.
      const canvas = document.createElement('canvas');
      canvas.width = canvas.height = 2;
      const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
      document.querySelector('.tool-image').click();
      const input = document.querySelector('.imageInput');
      const transfer = new DataTransfer();
      transfer.items.add(new File([blob], 'sample.png', {type: 'image/png'}));
      input.files = transfer.files;
      input.dispatchEvent(new Event('change'));
      await wait(() => cm.getValue().includes('/images/'));
      assert(!cm.getValue().includes(location.origin + '//'), 'Image URL has a doubled slash.');
      sessionStorage.setItem('blog-test-stage', 'saved');
      document.querySelector('#post-form').requestSubmit();
      return;
    }
    if (stage === 'saved') {
      assert(location.pathname === '/post/s/browser-post', 'Save did not navigate to the post.');
      assert(document.querySelector('.post-body h1')?.textContent === 'Saved heading', 'Published Markdown differs from preview.');
      const image = document.querySelector('.post-body img');
      assert(image, 'Published post has no image: ' + document.querySelector('.post-body').innerHTML);
      await wait(() => image?.complete);
      assert(image.naturalWidth === 2, 'Uploaded image did not load.');
      const baselineListeners = editorKeyListeners.size;
      document.querySelector('a[href^="/post/update/"]').click();
      await wait(() => document.querySelector('dialog[open] .CodeMirror'));
      document.querySelector('[data-close-dialog]').click();
      assert(!document.querySelector('dialog[open]'), 'Cancel did not close the dialog.');
      await htmx.ajax('GET', '/posts', '.archive-nav');
      assert(!document.querySelector('dialog[open]'), 'An unrelated swap reopened the dialog.');
      document.querySelector('a[href^="/post/update/"]').click();
      await wait(() => document.querySelector('dialog[open] .CodeMirror'));
      assert(document.querySelectorAll('.CodeMirror').length === 1, 'Reopening duplicated the editor.');
      assert(editorKeyListeners.size === baselineListeners + 1, 'Replacing an editor leaked its document listener.');

      const cm = document.querySelector('.CodeMirror').CodeMirror;
      const form = document.querySelector('#post-form');
      cm.setValue('An unsaved draft');
      const title = document.querySelector('#post-title');
      title.value = 'An unsaved title';
      const unchanged = () => {
        assert(document.querySelector('.CodeMirror').CodeMirror === cm && cm.getValue() === 'An unsaved draft', 'Error replaced the editor or lost the draft.');
        assert(document.querySelector('#post-title') === title && title.value === 'An unsaved title', 'Error lost the title.');
      };
      for (let attempt = 0; attempt < 20; attempt++) {
        await fetch('/auth/login/email', {method: 'POST', body: new URLSearchParams({email: 'trentnix@gmail.com', password: 'unused'})});
      }
      await htmx.ajax('POST', '/auth/login/email', {target: '#editor-errors', values: {email: 'trentnix@gmail.com', password: 'unused'}});
      assert(form.querySelector('[data-request-error]')?.textContent.includes('Too many requests'), 'Rate limiting produced no feedback.');
      unchanged();

      await htmx.ajax('POST', '/test/json-error', {target: '#editor-errors'});
      assert(form.querySelector('[data-request-error]').textContent.includes('HTTP 503'), 'Non-text failure lacks a fallback message.');
      assert(!form.textContent.includes('private detail'), 'Non-text response exposed private details.');
      unchanged();

      await htmx.ajax('POST', '/test/disconnect', {target: '#editor-errors'}).catch(() => {});
      await wait(() => form.querySelector('[data-request-error]')?.textContent.includes('connection failed'));
      unchanged();

      form.querySelector('[data-request-error]').remove();
      // Use a real XHR timeout, with a short timeout on this test-only button.
      const button = document.createElement('button');
      button.type = 'button';
      button.setAttribute('hx-post', '/test/timeout');
      button.setAttribute('hx-request', '{"timeout":30}');
      button.setAttribute('hx-target', '#editor-errors');
      form.append(button);
      htmx.process(button);
      button.click();
      await wait(() => form.querySelector('[data-request-error]')?.textContent.includes('connection failed'));
      unchanged();
      document.querySelector('[data-close-dialog]').click();
      document.querySelector('a[href^="/post/delete/"]').click();
      await wait(() => document.querySelector('dialog[open] form[action^="/post/delete/"]'));
      const deletion = document.querySelector('dialog[open] form');
      const confirm = deletion.querySelector('button[type="submit"]');
      const cancel = deletion.querySelector('[data-close-dialog]');
      assert(getComputedStyle(deletion).display === 'flex', 'Delete controls lost their action-row layout.');
      assert(Math.abs(confirm.getBoundingClientRect().top - cancel.getBoundingClientRect().top) < 1, 'Delete controls are not aligned.');
      cancel.click();
      assert(!document.querySelector('dialog[open]'), 'Cancel did not close the delete dialog.');
      await report('PASS');
    }
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
