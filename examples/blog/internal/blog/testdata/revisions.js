// History is a page. Selecting a version replaces only its preview.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const targets = __REVISION_TARGETS__;
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate.toString());
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  const stage = value => sessionStorage.setItem('revision-stage', value);
  const index = Number(sessionStorage.getItem('revision-index') || 0);
  const target = targets[index];
  const phase = sessionStorage.getItem('revision-stage');
  let consent = false;
  const prompts = [];
  window.blogConfirm = (options, proceed) => { prompts.push(options.message); if (consent) queueMicrotask(proceed); };
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    if (!phase) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      stage('logged-in');
      document.querySelector('#login-form').requestSubmit();
      await Promise.resolve();
      return;
    }
    if (phase === 'logged-in') {
      stage('list');
      location.assign(target.manager);
      return;
    }
    if (phase === 'list') {
      const history = document.querySelector('.page-manager-actions a[href="' + target.history + '"]');
      assert(history && history.getAttribute('hx-boost') === 'false', 'Management list must offer full-page History.');
      const edit = history.parentElement.querySelector('a[hx-get]');
      assert(getComputedStyle(history).padding === getComputedStyle(edit).padding && getComputedStyle(history).color === getComputedStyle(edit).color, 'History must match the other list actions.');
      assert(document.documentElement.scrollWidth <= innerWidth + 1, 'Management actions overflow the viewport.');
      stage('view');
      location.assign(target.view);
      return;
    }
    if (phase === 'view') {
      const history = document.querySelector('.content-actions a[href="' + target.history + '"]');
      assert(history && history.getAttribute('hx-boost') === 'false', 'Content actions must offer full-page History.');
      const edit = document.querySelector('.content-actions .action-edit');
      const menu = history.closest('details');
      const more = menu.querySelector('summary');
      assert(!menu.open && history.getBoundingClientRect().height === 0, 'Secondary actions must start collapsed.');
      assert(Math.abs(edit.getBoundingClientRect().top - more.getBoundingClientRect().top) < 1, 'Edit and More must stay on one row.');
      more.click();
      await Promise.resolve();
      assert(menu.open && history.getBoundingClientRect().height >= (innerWidth < 768 ? 44 : 36), 'History must be an accessible menu action.');
      const panel = menu.querySelector('.action-menu-items').getBoundingClientRect();
      assert(panel.left >= 0 && panel.right <= innerWidth, 'Action menu overflows the viewport.');
      assert(getComputedStyle(menu.querySelector('.action-delete')).borderTopWidth === '1px', 'Delete needs a divider after other actions.');
      history.focus();
      history.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
      assert(!menu.open && document.activeElement === more, 'Escape must close More and restore focus.');
      more.click();
      await Promise.resolve();
      document.querySelector('.post-body').click();
      await Promise.resolve();
      assert(!menu.open, 'Clicking outside must close More.');
      stage('editor');
      edit.click();
      await Promise.resolve();
    }
    if (phase === 'editor' || phase === 'view') {
      await wait(() => document.querySelector('.CodeMirror') && document.querySelector('[data-draft-status]:not([hidden])'));
      await wait(() => !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
      const editorPath = location.pathname;
      const form = document.querySelector('form[data-draft-owner]');
      form.elements.title.value = 'Unsaved local writing';
      form.elements.title.dispatchEvent(new Event('input', {bubbles: true}));
      await wait(() => form.querySelector('[data-draft-status]').textContent.includes('Saved locally'));
      assert(!form.querySelector('a[href^="/history/"]'), 'Editors must not offer History.');
      const cancel = form.querySelector('[data-close-dialog]');
      cancel.click();
      await Promise.resolve();
      assert(prompts.length === 1 && location.pathname === editorPath, 'Closing the editor lost unsaved writing without confirmation.');
      consent = true;
      cancel.click();
      await Promise.resolve();
      stage('history');
      document.querySelector('.content-actions .action-menu summary').click();
      await Promise.resolve();
      document.querySelector('.content-actions a[href="' + target.history + '"]').click();
      await Promise.resolve();
      return;
    }
    if (phase === 'current-editor') {
      await wait(() => document.querySelector('.CodeMirror') && document.querySelector('[data-draft-status]:not([hidden])'));
      const form = document.querySelector('form[data-draft-owner]');
      assert(form.elements.title.value === 'Current title', 'Edit must open the current saved writing.');
      const cancel = form.querySelector('.form-actions a');
      assert(cancel.getAttribute('href') === target.history + location.search, 'Cancel lost the selected current version.');
      stage('current-return');
      cancel.click();
      await Promise.resolve();
      return;
    }
    if (phase === 'history' || phase === 'current-return' || phase === 'selected') {
      assert(location.pathname === target.history && !document.querySelector('dialog'), 'History opened as a dialog.');
      const list = document.querySelector('.revision-list');
      const preview = document.querySelector('.revision-preview');
      assert(document.querySelector('.page-manager-heading > a').getAttribute('href') === target.view, 'History must return to the content, not the editor.');
      assert(list.children.length === 2, 'History must list both saved versions.');
      assert(list.querySelectorAll('.revision-latest').length === 1 && !preview.querySelector('.revision-latest, .revision-badge'), 'Show the latest-version label only in the history list.');
      assert(document.documentElement.scrollWidth <= innerWidth + 1, 'History overflows the viewport.');
      if (innerWidth < 768) assert(preview.getBoundingClientRect().top >= list.getBoundingClientRect().bottom, 'Mobile preview must follow the version list.');
      else assert(preview.getBoundingClientRect().left >= list.getBoundingClientRect().right, 'Desktop history must use side-by-side navigation and preview.');
      if (phase === 'history') {
        assert(preview.querySelector('h2').textContent === 'Current title' && preview.querySelector('.form-actions a').textContent === 'Edit', 'Current saved version must offer Edit.');
        stage('current-editor');
        preview.querySelector('.form-actions a').click();
        await Promise.resolve();
      } else if (phase === 'current-return') {
        stage('selected');
        list.querySelector('a[href$="revision=' + target.revision + '"]').click();
        await wait(() => preview.querySelector('h2').textContent === 'Original title');
      }
      if (phase === 'current-return' || phase === 'selected') {
        assert(preview.querySelector('h2').textContent === 'Original title', 'Selected preview has the wrong title.');
        assert(preview.querySelector('.form-actions a').textContent === 'Edit this version', 'Earlier writing must offer Edit this version.');
        // Older browser copies must never overwrite the newly opened editor token.
        for (const key of Object.keys(localStorage)) {
          if (!key.startsWith('hyperserver.blog.draft.v1:')) continue;
          const copy = JSON.parse(localStorage.getItem(key));
          copy.fields.version = '999';
          localStorage.setItem(key, JSON.stringify(copy));
        }
        preview.querySelector('summary').click();
        await Promise.resolve();
        assert(preview.querySelector('details').open && preview.querySelector('pre').textContent === '# Original body', 'Markdown source is unavailable.');
        stage('loaded');
        preview.querySelector('a').click();
        await Promise.resolve();
      }
      return;
    }
    if (phase === 'loaded') {
      await wait(() => document.querySelector('.CodeMirror') && document.querySelector('[data-draft-status]:not([hidden])'));
      const form = document.querySelector('form[data-draft-owner]');
      const editor = form.querySelector('.CodeMirror').CodeMirror;
      assert(!document.querySelector('dialog') && location.pathname === target.editor, 'Loaded writing must open the ordinary editor page.');
      assert(form.elements.title.value === 'Original title' && editor.getValue() === '# Original body', 'Old content was not loaded.');
      const published = form.elements.published;
      assert(form.elements.slug.value === 'history' && (published.type === 'checkbox' ? published.checked : published.value === 'on') && form.elements.version.value === '2', 'Loading changed current settings or edit version.');
      assert(form.querySelector('.revision-notice').textContent.includes('Editing a version saved') && form.querySelector('.revision-notice').textContent.includes('Nothing changes until you save.'), 'Historical editor lacks context.');
      assert(!form.querySelector('[data-draft-recovery]').hidden, 'Loading history discarded the unsaved local copy.');
      // Native form submissions can store CRLF. Browser controls normalize it
      // to LF, which must not make otherwise identical writing look unsaved.
      const current = JSON.parse(form.dataset.currentContent);
      form.elements.title.value = current.title;
      form.elements.summary.value = current.summary;
      editor.setValue(current.content);
      form.dispatchEvent(new Event('input', {bubbles: true}));
      assert(form.querySelector('[data-draft-status]').textContent === 'No unsaved changes.', 'Browser line-ending normalization made unchanged writing look dirty.');
      form.elements.title.value = 'Original title';
      form.elements.summary.value = 'Original summary';
      editor.setValue('# Original body');
      form.dispatchEvent(new Event('input', {bubbles: true}));
      assert(form.querySelector('.form-actions a').getAttribute('href') === target.history + '?revision=' + target.revision, 'Cancel must return to the selected earlier version.');
      form.querySelector('.form-actions a').click();
      await Promise.resolve();
      assert(prompts.length === 1 && location.pathname === target.editor, 'Loaded writing must count as unsaved before any typing.');
      consent = true;
      form.querySelector('[data-draft-restore]').click();
      await Promise.resolve();
      assert(form.elements.title.value === 'Unsaved local writing' && form.elements.version.value === '2', 'Recovery lost writing or replaced the fresh edit token.');
      form.elements.title.value = 'Original title';
      form.elements.summary.value = 'Original summary';
      editor.setValue('# Original body');
      form.dispatchEvent(new Event('input', {bubbles: true}));
      stage('saved');
      form.requestSubmit();
      await Promise.resolve();
      return;
    }
    if (phase === 'saved') {
      assert(!location.pathname.includes('/update/'), 'Saving did not return to the content.');
      const response = await fetch(target.history);
      assert(response.ok, 'Could not read history after saving.');
      const doc = new DOMParser().parseFromString(await response.text(), 'text/html');
      assert(doc.querySelectorAll('.revision-list li').length === 3, 'Restoring must append a version and preserve intervening history.');
      assert(doc.querySelector('.revision-preview h2').textContent === 'Original title', 'Restoration did not become the current content.');
      stage('conflict-editor');
      location.assign(target.editor + '?revision=' + target.revision);
      return;
    }
    if (phase === 'conflict-editor') {
      await wait(() => document.querySelector('.CodeMirror') && document.querySelector('[data-draft-status]:not([hidden])'));
      const editor = document.querySelector('form[data-draft-owner]');
      // Submit without HTMX, as a native form would. Keep the open editor clean
      // so this test does not dismiss a real browser beforeunload dialog.
      const submission = document.createElement('form');
      submission.method = 'post';
      submission.action = editor.action;
      const values = new FormData(editor);
      values.set('version', '1');
      values.set('title', 'Rejected writing');
      for (const [name, value] of values) {
        if (typeof value !== 'string') continue;
        const field = document.createElement('input');
        field.type = 'hidden';
        field.name = name;
        field.value = value;
        submission.append(field);
      }
      document.body.append(submission);
      stage('conflict');
      submission.submit();
      return;
    }
    if (phase === 'conflict') {
      await wait(() => document.querySelector('.CodeMirror') && document.querySelector('[data-draft-status]:not([hidden])'));
      const form = document.querySelector('form[data-draft-owner]');
      assert(form.querySelector('#editor-errors').textContent.includes('A newer version was saved'), 'Ordinary stale save did not return a conflict.');
      assert(form.elements.title.value === 'Rejected writing' && form.elements.version.value === '1', 'Conflict replaced unsaved input or the stale version token.');
      assert(form.querySelector('[data-draft-status]').textContent.includes('Saved locally'), 'Rejected writing did not receive a recovery copy.');
      const cancel = form.querySelector('.form-actions a');
      assert(cancel.getAttribute('href') === target.history + '?revision=' + target.revision, 'Failed save lost its history return link.');
      cancel.click();
      await Promise.resolve();
      assert(prompts.length === 1, 'Rejected writing lost the unsaved-navigation warning.');
      consent = true;
      stage('conflict-list');
      cancel.click();
      await Promise.resolve();
      return;
    }
    if (phase === 'conflict-list') {
      stage('conflict-history');
      location.assign(target.history);
      return;
    }
    if (phase === 'conflict-history') {
      assert(document.querySelectorAll('.revision-list li').length === 3, 'Rejected save added a history entry.');
      assert(!document.querySelector('.revision-preview a[hx-get]'), 'Current version must not offer deletion.');
      stage('delete-middle');
      document.querySelectorAll('.revision-list a')[1].click();
      await wait(() => document.querySelector('.revision-preview h2').textContent === 'Current title' && !document.querySelector('.revision-preview').classList.contains('htmx-settling'));
    }
    if (phase === 'conflict-history' || phase === 'delete-middle' || phase === 'delete-last') {
      for (let remaining = document.querySelectorAll('.revision-list li').length; remaining > 1; remaining--) {
        const remove = document.querySelector('.revision-preview a[hx-get]');
        assert(remove?.textContent === 'Delete revision', 'Earlier version must offer deletion.');
        remove.click();
        await wait(() => document.querySelector('#dialog-host dialog[open]') && !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
        let dialog = document.querySelector('#dialog-host dialog');
        const cancel = dialog.querySelector('[data-close-dialog]');
        assert(document.activeElement === cancel, 'Revision deletion must focus Cancel by default.');
        assert(dialog.textContent.includes('This cannot be undone.'), 'Deletion needs a permanent-removal warning.');
        assert(dialog.getBoundingClientRect().left >= 0 && dialog.getBoundingClientRect().right <= innerWidth, 'Confirmation overflows the viewport.');
        cancel.click();
        await wait(() => !document.querySelector('#dialog-host dialog[open]'));
        assert(document.querySelectorAll('.revision-list li').length === remaining, 'Cancel removed history.');
        remove.click();
        await wait(() => document.querySelector('#dialog-host dialog[open]') && !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
        dialog = document.querySelector('#dialog-host dialog');
        stage(remaining === 2 ? 'deleted-history' : 'delete-last');
        dialog.querySelector('button[type="submit"]').click();
        if (remaining > 2) await wait(() => document.querySelectorAll('.revision-list li').length === remaining - 1 && !document.querySelector('dialog[open]') && document.querySelector('.revision-preview h2').textContent === 'Original title' && !document.querySelector('.revision-preview').classList.contains('htmx-settling'));
      }
      return;
    }
    if (phase === 'deleted-history') {
      assert(location.pathname === target.view, 'Deleting the last older revision must return to the content.');
      assert(document.querySelector('.post-title, .page-title, h1')?.textContent.includes('Original title'), 'History deletion changed current writing.');
      assert(!document.querySelector('.content-actions a[href="' + target.history + '"]'), 'History action remains without earlier versions.');
      const menu = document.querySelector('.content-actions .action-menu');
      menu.querySelector('summary').click();
      const remove = menu.querySelector('.action-delete');
      const style = getComputedStyle(remove);
      assert(style.borderTopWidth === (remove.previousElementSibling ? '1px' : '0px'), 'A lone Delete action must not have a divider.');
      assert(style.marginTop === (remove.previousElementSibling ? '4px' : '0px'), 'A lone Delete action must not have separator spacing.');
      if (index + 1 < targets.length) {
        sessionStorage.setItem('revision-index', String(index + 1));
        stage('list');
        location.assign(targets[index + 1].manager);
        return;
      }
      await report('PASS');
      return;
    }
    throw new Error('Unknown revision stage: ' + phase);
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
