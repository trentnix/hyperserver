// Use the real editor, HTMX, and browser storage to exercise recovery and failures.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  if (location.search.includes('draft-peer')) return;
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async (predicate, diagnostics = () => '') => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate.toString() + '\n' + diagnostics());
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  const change = (form, name, value) => {
    const field = form.elements[name];
    if (field.type === 'checkbox') field.checked = value;
    else field.value = value;
    field.dispatchEvent(new Event('input', {bubbles: true}));
  };
  const ready = async () => {
    await wait(() => document.querySelector('.CodeMirror') && document.querySelector('[data-draft-status]:not([hidden])'));
    await wait(() => !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
    const form = document.querySelector('form[data-draft-owner]');
    const status = form.querySelector('[data-draft-status]');
    const controls = form.querySelector('[type="submit"]').closest('.form-actions');
    assert(status.getBoundingClientRect().top >= controls.getBoundingClientRect().bottom, 'Routine recovery status must sit below the save controls.');
    assert(parseFloat(getComputedStyle(status).fontSize) < parseFloat(getComputedStyle(form).fontSize), 'Routine recovery status must use quieter typography than the form.');
    return form;
  };
  const copies = (form, storage = localStorage) => Object.keys(storage).filter(key => key.startsWith('hyperserver.blog.draft.v1:' + form.dataset.draftOwner + ':' + new URL(form.action).pathname + ':'));
  const recoveryDetails = form => JSON.stringify({
    status: form.querySelector('[data-draft-status]')?.textContent,
    keys: copies(form, form.ownerDocument.defaultView.localStorage)
  });
  const saved = async form => wait(() => form.querySelector('[data-draft-status]').textContent === 'Saved locally. Use Save to apply changes.', () => recoveryDetails(form));
  let prompts = [];
  let consent = false;
  window.blogConfirm = (options, proceed) => { prompts.push(options.message); if (consent) queueMicrotask(proceed); };
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    const stage = sessionStorage.getItem('recovery-stage');
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('recovery-stage', 'logged-in');
      document.querySelector('#login-form').requestSubmit();
      await Promise.resolve();
      return;
    }
    if (stage === 'logged-in') {
      sessionStorage.setItem('recovery-stage', 'editor');
      location.assign('/post/create');
      return;
    }
    if (stage === 'editor') {
      const form = await ready();
      assert(form.querySelector('[data-draft-recovery]').hidden, 'An empty editor must not show a recovery banner.');
      change(form, 'title', 'Recovered post');
      change(form, 'summary', 'Recovered summary');
      change(form, 'slug', 'invalid slug');
      form.querySelector('.CodeMirror').CodeMirror.setValue('Unsaved **writing**.');
      await saved(form);
      assert(copies(form).length === 1, 'Editing must create one recovery copy.');
      const key = copies(form)[0];
      sessionStorage.setItem('copy-key', key);
      const draft = JSON.parse(localStorage.getItem(key));
      assert(draft.fields.content === 'Unsaved **writing**.' && draft.fields.summary === 'Recovered summary', 'Recovery omitted Markdown or metadata.');

      // Reject ordinary navigation, HTMX navigation, logout, and browser unload.
      document.querySelector('.account-nav a[href="/pages"]').click();
      await Promise.resolve();
      document.querySelector('.profile-links a[href="/about"]').click();
      await Promise.resolve();
      document.querySelector('.account-nav form').requestSubmit();
      await Promise.resolve();
      assert(prompts.length === 3 && location.pathname === '/post/create', 'Unsaved navigation did not ask for consent.');
      const unload = new Event('beforeunload', {cancelable: true});
      window.dispatchEvent(unload);
      assert(unload.defaultPrevented, 'Unsaved reload or close must request the native warning.');

      form.noValidate = true;
      form.requestSubmit();
      await Promise.resolve();
      const pendingUnload = new Event('beforeunload', {cancelable: true});
      window.dispatchEvent(pendingUnload);
      assert(pendingUnload.defaultPrevented, 'An in-flight save suppressed the unsaved-edit warning.');
      await wait(() => form.querySelector('#editor-errors').textContent.includes('slug'));
      assert(localStorage.getItem(key), 'Failed save removed its recovery copy.');

      // A second live editor must own a different key, even for the same new post.
      const peer = document.createElement('iframe');
      peer.src = '/post/create?draft-peer=1';
      document.body.append(peer);
      // CodeMirror appears before DOMContentLoaded initializes recovery. Wait
      // for the peer document and recovery before simulating its first edit.
      await wait(
        () => peer.contentDocument?.readyState === 'complete' &&
          peer.contentDocument.querySelector('.CodeMirror')?.CodeMirror &&
          peer.contentDocument.querySelector('[data-draft-status]:not([hidden])'),
        () => JSON.stringify({
          readyState: peer.contentDocument?.readyState,
          status: peer.contentDocument?.querySelector('[data-draft-status]')?.textContent
        })
      );
      const peerForm = peer.contentDocument.querySelector('#post-form');
      peerForm.querySelector('.CodeMirror').CodeMirror.setValue('Writing in another tab.');
      await saved(peerForm);
      await wait(() => copies(form).length === 2,
        () => 'Original: ' + recoveryDetails(form) + '\nPeer: ' + recoveryDetails(peerForm));
      assert(JSON.parse(localStorage.getItem(key)).fields.content === draft.fields.content, 'Another editor overwrote this copy.');
      const peerKey = copies(form).find(candidate => candidate !== key);
      assert(JSON.parse(localStorage.getItem(peerKey)).fields.content === 'Writing in another tab.', 'The peer recovery copy must contain its own edits.');
      peer.remove();
      localStorage.removeItem(peerKey);

      const scope = key.slice(0, key.lastIndexOf(':') + 1);
      localStorage.setItem(scope + 'expired', JSON.stringify({...draft, saved: 1}));
      localStorage.setItem(scope + 'broken', '{invalid');
      const foreign = 'hyperserver.blog.draft.v1:other-account:/post:copy';
      localStorage.setItem(foreign, JSON.stringify({...draft, saved: Date.now() + 1000, fields: {...draft.fields, title: 'Someone else'}}));
      // Copies from before publication and preview controls must remain recoverable.
      const setItem = Storage.prototype.setItem;
      Storage.prototype.setItem = function(name, value) {
        if (name === key) {
          const data = JSON.parse(value);
          delete data.fields.published;
          delete data.fields.publishDate;
          delete data.fields.publishTime;
          delete data.fields.previewImage;
          value = JSON.stringify(data);
        }
        return setItem.call(this, name, value);
      };
      sessionStorage.setItem('recovery-stage', 'recover');
      consent = true;
      const reload = document.createElement('a');
      reload.href = '/post/create';
      document.body.append(reload);
      reload.click();
      await Promise.resolve();
      return;
    }
    if (stage === 'recover') {
      const form = await ready();
      assert(form.elements.title.value === '', 'Recovery must not silently overwrite editor fields.');
      assert(!form.querySelector('[data-draft-recovery]').hidden, 'Reload did not offer recovery.');
      assert(copies(form).length === 1, 'Expired or malformed copies remain available.');
      form.querySelector('[data-draft-restore]').click();
      await Promise.resolve();
      assert(form.elements.title.value === 'Recovered post' && form.elements.summary.value === 'Recovered summary', 'Restoring lost title or summary, or used another account\'s copy.');
      assert(form.elements.slug.placeholder === 'recovered-post', 'Restoring a title did not refresh the slug suggestion.');
      assert(form.querySelector('.CodeMirror').CodeMirror.getValue() === 'Unsaved **writing**.', 'Restoring did not update EasyMDE.');
      assert(form.elements.published.value === '', 'Older recovery changed the current publication setting.');
      assert(form.elements.previewImage.value === '', 'Older recovery changed the current preview selection.');
      change(form, 'slug', 'recovered-post');
      sessionStorage.setItem('recovery-stage', 'post-saved');
      form.requestSubmit();
      await Promise.resolve();
      return;
    }
    if (stage === 'post-saved') {
      assert(location.pathname === '/post/s/recovered-post', 'Explicit save failed to create the draft.');
      assert(!localStorage.getItem(sessionStorage.getItem('copy-key')), 'Successful save did not remove the restored copy.');
      assert(Object.keys(localStorage).filter(key => key.includes(':/post:') && !key.includes('other-account')).length === 0, 'Successful save retained its own recovery copy.');
      assert((await fetch('/post/s/recovered-post', {credentials: 'omit'})).status === 404, 'Recovery published a private post.');

      const about = new DOMParser().parseFromString(await (await fetch('/about')).text(), 'text/html');
      const editURL = about.querySelector('a[href^="/pages/update/"]').getAttribute('href');
      await htmx.ajax('GET', editURL, '#dialog-host');
      const form = await ready();
      change(form, 'title', 'Recovered About');
      change(form, 'summary', 'Page summary');
      change(form, 'displayOrder', '7');
      change(form, 'childrenDisplay', 'list');
      change(form, 'published', false);
      form.querySelector('.CodeMirror').CodeMirror.setValue('Unpublished changes to an existing page.');
      await saved(form);
      // Image uploads block submission, so pressing Save must not waive the leave warning.
      form.dataset.uploading = 'true';
      form.requestSubmit();
      await Promise.resolve();
      delete form.dataset.uploading;
      const blockedUnload = new Event('beforeunload', {cancelable: true});
      window.dispatchEvent(blockedUnload);
      assert(blockedUnload.defaultPrevented, 'A blocked save suppressed the unsaved-edit warning.');
      const publicAbout = await (await fetch('/about', {credentials: 'omit'})).text();
      assert(!publicAbout.includes('Recovered About'), 'Autosave changed the published page.');
      const dialog = form.closest('dialog');
      form.querySelector('[data-close-dialog]').click();
      await Promise.resolve();
      assert(dialog.open, 'Rejected Cancel closed the editor.');
      const escape = new Event('cancel', {cancelable: true});
      dialog.dispatchEvent(escape);
      assert(escape.defaultPrevented, 'Escape did not protect unsaved edits.');
      consent = true;
      form.querySelector('[data-close-dialog]').click();
      await Promise.resolve();
      assert(!dialog.open, 'Accepted Cancel did not close the editor.');
      const concurrentSave = await fetch(form.action, {method: 'POST', headers: {'HX-Request': 'true'}, body: new URLSearchParams({
        title: 'About changed on the server', slug: 'about', content: 'A separately saved version.', childrenDisplay: 'hidden', published: 'on', version: form.elements.version.value
      })});
      assert(concurrentSave.ok, 'Unable to prepare a changed server version.');
      await htmx.ajax('GET', editURL, '#dialog-host');
      const restored = await ready();
      assert(restored.elements.title.value === 'About changed on the server' && restored.querySelector('[data-draft-description]').textContent.includes('server version or initial settings have changed'), 'Stale recovery must disclose changed server content without replacing it.');
      restored.querySelector('[data-draft-restore]').click();
      await Promise.resolve();
      assert(restored.elements.title.value === 'Recovered About' && restored.elements.summary.value === 'Page summary' && restored.elements.displayOrder.value === '7', 'Page recovery lost metadata.');
      assert(restored.elements.childrenDisplay.value === 'list' && !restored.elements.published.checked, 'Page recovery lost its select or checkbox.');

      // Quota and privacy failures must be visible and leave the editor usable.
      const setItem = Storage.prototype.setItem;
      Storage.prototype.setItem = function(key, value) {
        if (key.startsWith('hyperserver.blog.draft.v1:')) throw new DOMException('Storage full', 'QuotaExceededError');
        return setItem.call(this, key, value);
      };
      change(restored, 'title', 'Storage failure does not lose writing');
      await wait(() => restored.querySelector('[data-draft-status]').textContent.includes('could not be saved'));
      const failure = restored.querySelector('[data-draft-status]');
      assert(failure.classList.contains('draft-status--error') && getComputedStyle(failure).animationName === 'none', 'Recovery failures must remain visible and distinct from routine status.');
      change(restored, 'summary', 'Another edit while storage is unavailable');
      assert(failure.textContent.includes('could not be saved') && failure.classList.contains('draft-status--error'), 'Typing must not hide a recovery failure.');
      assert(restored.elements.title.value === 'Storage failure does not lose writing', 'Storage failure changed the editor.');
      Storage.prototype.setItem = setItem;
      change(restored, 'title', 'Recovered About');
      change(restored, 'published', true);
      await saved(restored);
      assert(!failure.classList.contains('draft-status--error'), 'A successful recovery save must clear the failure style.');
      sessionStorage.setItem('recovery-stage', 'page-saved');
      restored.requestSubmit();
      await Promise.resolve();
      return;
    }
    if (stage === 'page-saved') {
      assert(location.pathname === '/about' && document.querySelector('h1').textContent === 'Recovered About', 'Explicit page save failed.');
      await htmx.ajax('GET', '/post/create', '#dialog-host');
      const form = await ready();
      form.querySelector('.CodeMirror').CodeMirror.setValue('Discard this recovery copy.');
      await saved(form);
      const selectedKey = copies(form)[0];
      const originalCopy = JSON.parse(localStorage.getItem(selectedKey));
      const olderKey = selectedKey + '-older';
      localStorage.setItem(olderKey, JSON.stringify({...originalCopy, saved: originalCopy.saved - 1000}));
      consent = true;
      form.querySelector('[data-close-dialog]').click();
      await Promise.resolve();
      await htmx.ajax('GET', '/post/create', '#dialog-host');
      const clean = await ready();
      consent = false;
      clean.querySelector('[data-draft-discard]').click();
      await Promise.resolve();
      assert(copies(clean).length === 2 && !clean.querySelector('[data-draft-recovery]').hidden, 'Rejected discard changed recovery.');
      consent = true;

      // A concurrent edit must survive discard and leave an explanation.
      const changedCopy = JSON.stringify({...originalCopy, fields: {...originalCopy.fields, content: 'Changed in another tab.'}});
      localStorage.setItem(selectedKey, changedCopy);
      clean.querySelector('[data-draft-discard]').click();
      await Promise.resolve();
      assert(localStorage.getItem(selectedKey) === changedCopy && !clean.querySelector('[data-draft-recovery]').hidden, 'Discard deleted a concurrently changed copy.');
      assert(clean.querySelector('[data-draft-status]').textContent.includes('changed in another tab'), 'Concurrent changes need an explanation.');

      const removeItem = Storage.prototype.removeItem;
      try {
        Storage.prototype.removeItem = function(key) {
          if (key === selectedKey) throw new Error('Storage unavailable');
          return removeItem.call(this, key);
        };
        clean.querySelector('[data-draft-discard]').click();
        await Promise.resolve();
        assert(localStorage.getItem(selectedKey) === changedCopy && !clean.querySelector('[data-draft-recovery]').hidden, 'Failed discard must keep recovery available.');
        assert(clean.querySelector('[data-draft-status]').classList.contains('draft-status--error') && clean.querySelector('[data-draft-status]').textContent.includes('could not be discarded'), 'Failed discard must report a storage error.');
      } finally {
        Storage.prototype.removeItem = removeItem;
      }
      clean.querySelector('[data-draft-discard]').click();
      await Promise.resolve();
      assert(!localStorage.getItem(selectedKey) && localStorage.getItem(olderKey), 'Discard must remove only the offered copy.');
      assert(clean.querySelector('[data-draft-recovery]').hidden && clean.querySelector('[data-draft-recovery]').getClientRects().length === 0, 'Accepted discard must dismiss recovery even when another copy exists.');
      assert(clean.querySelector('[data-draft-status]').textContent === 'Browser copy discarded.' && !clean.querySelector('[data-draft-status]').classList.contains('draft-status--error'), 'Successful discard must clear the error and confirm deletion.');
      assert(clean.querySelector('.CodeMirror').CodeMirror.getValue() === '', 'Discard modified the clean editor.');
      const count = prompts.length;
      clean.querySelector('[data-close-dialog]').click();
      await Promise.resolve();
      assert(prompts.length === count, 'Unchanged editor warned before closing.');
      await htmx.ajax('GET', '/post/create', '#dialog-host');
      const reopened = await ready();
      assert(!reopened.querySelector('[data-draft-recovery]').hidden, 'Other copies must remain available when reopening the editor.');
      // Another tab may already have removed the offered copy.
      localStorage.removeItem(olderKey);
      reopened.querySelector('[data-draft-discard]').click();
      await Promise.resolve();
      assert(copies(reopened).length === 0 && reopened.querySelector('[data-draft-recovery]').hidden, 'An already-removed copy must dismiss cleanly.');
      reopened.querySelector('[data-close-dialog]').click();
      localStorage.setItem(selectedKey, JSON.stringify(originalCopy));
      await htmx.ajax('GET', '/post/create', '#dialog-host');
      const failedRestore = await ready();
      const setItem = Storage.prototype.setItem;
      try {
        Storage.prototype.setItem = () => { throw new Error('Storage unavailable'); };
        failedRestore.querySelector('[data-draft-restore]').click();
        await Promise.resolve();
        assert(failedRestore.elements.content.value === originalCopy.fields.content && failedRestore.querySelector('[data-draft-recovery]').hidden, 'Restoring must load the copy and dismiss recovery even if a new browser save fails.');
        const status = failedRestore.querySelector('[data-draft-status]');
        assert(status.classList.contains('draft-status--error') && status.textContent.includes('could not be saved'), 'The restored status must not hide a storage failure.');
        assert(localStorage.getItem(selectedKey), 'A failed local save must retain the original recovery copy.');
      } finally {
        Storage.prototype.setItem = setItem;
      }
      await report('PASS');
    }
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
