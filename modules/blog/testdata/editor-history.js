// Open an editor from management, then restore it through HTMX history navigation.
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
      location.assign('/post/manage');
      return;
    }
    if (stage === 'native-saved') {
      assert(location.pathname === '/post/s/native-boundary', 'The native textarea did not submit through the ordinary form.');
      assert(document.querySelector('.post-body strong')?.textContent === 'native writing', 'Ordinary submission lost the native Markdown.');
      await report('PASS');
      return;
    }
    assert(stage === 'editor', 'History navigation unexpectedly reloaded the document.');
    const {getMarkdownEditor} = await import('/assets/js/markdown-editor.js');
    sessionStorage.setItem('editor-history-stage', 'restoring');
    const article = document.querySelector('#article-content');

    // Editing viewed content must return to that content, rather than its list.
    await wait(() => document.querySelector('#post-archive a[href="/post/s/cancel-source"]') && !document.querySelector('#post-archive').classList.contains('htmx-settling'));
    for (const path of ['/post/s/cancel-source', '/about']) {
      const link = document.querySelector(path === '/about' ? '.profile-links a[href="/about"]' : '#post-archive a[href="/post/s/cancel-source"]');
      link.click();
      await wait(() => location.pathname === path && article.querySelector('.action-edit') && !article.classList.contains('htmx-settling'));
      article.querySelector('.action-edit').click();
      await wait(() => article.querySelector('.CodeMirror') && !article.classList.contains('htmx-settling'));
      const cancel = article.querySelector('.form-actions a.button-secondary');
      assert(cancel.getAttribute('href') === path, 'Cancel must return to the viewed content.');
      cancel.click();
      await wait(() => location.pathname === path && article.querySelector('.action-edit') && !article.classList.contains('htmx-settling'));
      assert(document.querySelector('#article-content') === article, 'Cancel replaced the page shell.');
    }
    document.querySelector('.account-menu a[href="/post/manage"]').click();
    await wait(() => location.pathname === '/post/manage' && article.querySelector('.page-manager') && !article.classList.contains('htmx-settling'));

    document.querySelector('a[href="/post/create"]').click();
    await wait(() => location.pathname === '/post/create' && article.querySelector('.CodeMirror') && !article.classList.contains('htmx-settling'));
    assert(document.querySelector('#article-content') === article && !document.querySelector('dialog[open]'), 'New post must replace only the main content with an editor page.');
    assert(document.title.includes('New post'), 'Editor navigation must update the document title.');
    const panelStyle = getComputedStyle(article.querySelector('.editor-panel'));
    assert(panelStyle.fontSize === '16px' && panelStyle.lineHeight === '24px', 'Editor typography is inconsistent.');
    const cancel = article.querySelector('.form-actions a.button-secondary');
    assert(cancel && getComputedStyle(cancel).textDecorationLine === 'none', 'Full-page Cancel must retain its secondary button style.');
    document.querySelector('.profile-links a[href="/about"]').click();
    await wait(() => location.pathname === '/about' && article.querySelector('h1')?.textContent === 'About Me');

    // Repeated restoration must create one working editor, not duplicate instances.
    for (let attempt = 0; attempt < 2; attempt++) {
      history.back();
      await wait(() => location.pathname === '/post/create' && article.querySelector('.CodeMirror'));
      assert(document.querySelector('#article-content') === article, 'History replaced the article container.');
      assert(article.querySelectorAll('.CodeMirror').length === 1, 'History duplicated the editor.');
      const form = article.querySelector('#post-form');
      if (attempt > 0) assert(!form.querySelector('[data-draft-recovery]').hidden, 'History restoration must offer the unsaved writing.');
      const textarea = form.elements.content;
      const editor = getMarkdownEditor(textarea);
      editor.setMarkdown('# Restored preview');
      editor.focus();
      assert(document.activeElement === document.querySelector('#markdown-input'), 'The editor boundary did not focus the writing input.');
      assert(editor.getMarkdown() === textarea.value && textarea.value === '# Restored preview', 'Editor changes did not synchronize the form field.');
      article.querySelector('.tool-preview').click();
      await wait(() => article.querySelector('.editor-preview h1')?.textContent === 'Restored preview');
      // Leave a preview pending so navigation must cancel application-owned work.
      article.querySelector('.tool-preview').click();
      const originalFetch = window.fetch;
      let previewSignal;
      window.fetch = (path, options) => {
        if (path !== '/post/preview') return originalFetch(path, options);
        previewSignal = options.signal;
        return new Promise((resolve, reject) => previewSignal.addEventListener('abort', () => reject(new DOMException('Preview canceled', 'AbortError')), {once: true}));
      };
      article.querySelector('.tool-preview').click();
      await wait(() => previewSignal);
      history.forward();
      await wait(() => location.pathname === '/about' && article.querySelector('h1')?.textContent === 'About Me');
      window.fetch = originalFetch;
      assert(!article.querySelector('.CodeMirror'), 'History retained the previous editor.');
      assert(!getMarkdownEditor(textarea) && previewSignal.aborted, 'History did not release the editor and cancel its pending preview.');
      assert(textarea.required && form.querySelector('#markdown-label').htmlFor === textarea.id, 'Cleanup did not restore native textarea validation and labeling.');
      const prefix = 'hyperserver.blog.draft.v1:' + form.dataset.draftOwner + ':/post:';
      assert(Object.keys(localStorage).some(key => key.startsWith(prefix) && JSON.parse(localStorage.getItem(key)).fields.content === '# Restored preview'), 'History navigation lost unsaved writing.');
    }
    // If the optional editor is unavailable, recovery and ordinary saves still work.
    const EasyMDE = window.EasyMDE;
    window.EasyMDE = undefined;
    await htmx.ajax('GET', '/post/create', {target: '#article-content'});
    const form = article.querySelector('#post-form');
    const textarea = form.elements.content;
    assert(!getMarkdownEditor(textarea) && textarea.required && getComputedStyle(textarea).display !== 'none', 'Unavailable enhancement must retain the native textarea.');
    form.elements.title.value = 'Native boundary';
    textarea.value = '**native writing**';
    textarea.dispatchEvent(new Event('input', {bubbles: true}));
    await wait(() => form.querySelector('[data-draft-status]').textContent === 'Saved locally. Use Save to apply changes.');
    const prefix = 'hyperserver.blog.draft.v1:' + form.dataset.draftOwner + ':/post:';
    assert(Object.keys(localStorage).some(key => key.startsWith(prefix) && JSON.parse(localStorage.getItem(key)).fields.content === '**native writing**'), 'Native editing lost its recovery copy.');
    form.removeAttribute('hx-post');
    form.setAttribute('hx-boost', 'false');
    // Remove the submit listener installed before this form switched to native use.
    htmx.process(form, true);
    window.EasyMDE = EasyMDE;
    sessionStorage.setItem('editor-history-stage', 'native-saved');
    form.requestSubmit();
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
