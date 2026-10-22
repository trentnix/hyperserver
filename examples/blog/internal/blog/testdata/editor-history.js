// Exercise the full-page editor, then restore it through HTMX history navigation.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate.toString());
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    const stage = sessionStorage.getItem('editor-history-stage');
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('editor-history-stage', 'logged-in');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'logged-in') {
      sessionStorage.setItem('editor-history-stage', 'editor');
      location.assign('/post/create');
      return;
    }
    assert(stage === 'editor', 'History navigation unexpectedly reloaded the document.');
    sessionStorage.setItem('editor-history-stage', 'restoring');
    await wait(() => document.querySelector('#article-content .CodeMirror'));
    const article = document.querySelector('#article-content');
    document.querySelector('footer a[href="/about"]').click();
    await wait(() => location.pathname === '/about' && article.querySelector('h1')?.textContent === 'About Me');

    // Repeated restoration must create one working editor, not duplicate instances.
    for (let attempt = 0; attempt < 2; attempt++) {
      history.back();
      await wait(() => location.pathname === '/post/create' && article.querySelector('.CodeMirror'));
      assert(document.querySelector('#article-content') === article, 'History replaced the article container.');
      assert(article.querySelectorAll('.CodeMirror').length === 1, 'History duplicated the editor.');
      const editor = article.querySelector('.CodeMirror').CodeMirror;
      editor.setValue('# Restored preview');
      article.querySelector('.tool-preview').click();
      await wait(() => article.querySelector('.editor-preview h1')?.textContent === 'Restored preview');
      history.forward();
      await wait(() => location.pathname === '/about' && article.querySelector('h1')?.textContent === 'About Me');
      assert(!article.querySelector('.CodeMirror'), 'History retained the previous editor.');
    }
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
