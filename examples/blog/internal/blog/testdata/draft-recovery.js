// Use the real editor, HTMX, and browser storage to exercise recovery and failures.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  if (location.search.includes('draft-peer')) return;
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate.toString());
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
    await wait(() => document.querySelector('.CodeMirror') && document.querySelector('.draft-recovery:not([hidden])'));
    await wait(() => !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
    return document.querySelector('form[data-draft-owner]');
  };
  const copies = form => Object.keys(localStorage).filter(key => key.startsWith('hyperserver.blog.draft.v1:' + form.dataset.draftOwner + ':' + new URL(form.action).pathname + ':'));
  const saved = async form => wait(() => form.querySelector('[data-draft-status]').textContent.includes('Recovery copy saved'));
  let prompts = [];
  let consent = false;
  window.confirm = message => { prompts.push(message); return consent; };
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    const stage = sessionStorage.getItem('recovery-stage');
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('recovery-stage', 'logged-in');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'logged-in') {
      sessionStorage.setItem('recovery-stage', 'editor');
      location.assign('/post/create');
      return;
    }
    if (stage === 'editor') {
      const form = await ready();
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
      document.querySelector('.profile-links a[href="/about"]').click();
      document.querySelector('.account-nav form').requestSubmit();
      assert(prompts.length === 3 && location.pathname === '/post/create', 'Unsaved navigation did not ask for consent.');
      const unload = new Event('beforeunload', {cancelable: true});
      window.dispatchEvent(unload);
      assert(unload.defaultPrevented, 'Unsaved reload or close must request the native warning.');

      form.noValidate = true;
      form.requestSubmit();
      const pendingUnload = new Event('beforeunload', {cancelable: true});
      window.dispatchEvent(pendingUnload);
      assert(pendingUnload.defaultPrevented, 'An in-flight save suppressed the unsaved-edit warning.');
      await wait(() => form.querySelector('#editor-errors').textContent.includes('slug'));
      assert(localStorage.getItem(key), 'Failed save removed its recovery copy.');

      // A second live editor must own a different key, even for the same new post.
      const peer = document.createElement('iframe');
      peer.src = '/post/create?draft-peer=1';
      document.body.append(peer);
      await wait(() => peer.contentDocument?.querySelector('.CodeMirror'));
      const peerForm = peer.contentDocument.querySelector('#post-form');
      peerForm.querySelector('.CodeMirror').CodeMirror.setValue('Writing in another tab.');
      await wait(() => copies(form).length === 2);
      assert(JSON.parse(localStorage.getItem(key)).fields.content === draft.fields.content, 'Another editor overwrote this copy.');
      const peerKey = copies(form).find(candidate => candidate !== key);
      peer.remove();
      localStorage.removeItem(peerKey);

      const scope = key.slice(0, key.lastIndexOf(':') + 1);
      localStorage.setItem(scope + 'expired', JSON.stringify({...draft, saved: 1}));
      localStorage.setItem(scope + 'broken', '{invalid');
      const foreign = 'hyperserver.blog.draft.v1:other-account:/post:copy';
      localStorage.setItem(foreign, JSON.stringify({...draft, saved: Date.now() + 1000, fields: {...draft.fields, title: 'Someone else'}}));
      // A copy from before the post publication checkbox must remain recoverable.
      const setItem = Storage.prototype.setItem;
      Storage.prototype.setItem = function(name, value) {
        if (name === key) {
          const data = JSON.parse(value);
          delete data.fields.published;
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
      return;
    }
    if (stage === 'recover') {
      const form = await ready();
      assert(form.elements.title.value === '', 'Recovery must not silently overwrite editor fields.');
      assert(!form.querySelector('[data-draft-recovery]').hidden, 'Reload did not offer recovery.');
      assert(copies(form).length === 1, 'Expired or malformed copies remain available.');
      form.querySelector('[data-draft-restore]').click();
      assert(form.elements.title.value === 'Recovered post' && form.elements.summary.value === 'Recovered summary', 'Restoring lost title or summary, or used another account\'s copy.');
      assert(form.querySelector('.CodeMirror').CodeMirror.getValue() === 'Unsaved **writing**.', 'Restoring did not update EasyMDE.');
      assert(!form.elements.published.checked, 'Older recovery changed the current publication setting.');
      change(form, 'slug', 'recovered-post');
      sessionStorage.setItem('recovery-stage', 'post-saved');
      form.requestSubmit();
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
      delete form.dataset.uploading;
      const blockedUnload = new Event('beforeunload', {cancelable: true});
      window.dispatchEvent(blockedUnload);
      assert(blockedUnload.defaultPrevented, 'A blocked save suppressed the unsaved-edit warning.');
      const publicAbout = await (await fetch('/about', {credentials: 'omit'})).text();
      assert(!publicAbout.includes('Recovered About'), 'Autosave changed the published page.');
      const dialog = form.closest('dialog');
      form.querySelector('[data-close-dialog]').click();
      assert(dialog.open, 'Rejected Cancel closed the editor.');
      const escape = new Event('cancel', {cancelable: true});
      dialog.dispatchEvent(escape);
      assert(escape.defaultPrevented, 'Escape did not protect unsaved edits.');
      consent = true;
      form.querySelector('[data-close-dialog]').click();
      assert(!dialog.open, 'Accepted Cancel did not close the editor.');
      const concurrentSave = await fetch(form.action, {method: 'POST', headers: {'HX-Request': 'true'}, body: new URLSearchParams({
        title: 'About changed on the server', slug: 'about', content: 'A separately saved version.', childrenDisplay: 'hidden', published: 'on'
      })});
      assert(concurrentSave.ok, 'Unable to prepare a changed server version.');
      await htmx.ajax('GET', editURL, '#dialog-host');
      const restored = await ready();
      assert(restored.elements.title.value === 'About changed on the server' && restored.querySelector('[data-draft-description]').textContent.includes('server version or initial settings have changed'), 'Stale recovery must disclose changed server content without replacing it.');
      restored.querySelector('[data-draft-restore]').click();
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
      assert(restored.elements.title.value === 'Storage failure does not lose writing', 'Storage failure changed the editor.');
      Storage.prototype.setItem = setItem;
      change(restored, 'title', 'Recovered About');
      change(restored, 'published', true);
      await saved(restored);
      sessionStorage.setItem('recovery-stage', 'page-saved');
      restored.requestSubmit();
      return;
    }
    if (stage === 'page-saved') {
      assert(location.pathname === '/about' && document.querySelector('h1').textContent === 'Recovered About', 'Explicit page save failed.');
      await htmx.ajax('GET', '/post/create', '#dialog-host');
      const form = await ready();
      form.querySelector('.CodeMirror').CodeMirror.setValue('Discard this recovery copy.');
      await saved(form);
      consent = true;
      form.querySelector('[data-close-dialog]').click();
      await htmx.ajax('GET', '/post/create', '#dialog-host');
      const clean = await ready();
      consent = false;
      clean.querySelector('[data-draft-discard]').click();
      assert(copies(clean).length === 1, 'Rejected discard deleted the recovery copy.');
      consent = true;
      clean.querySelector('[data-draft-discard]').click();
      assert(copies(clean).length === 0 && clean.querySelector('[data-draft-recovery]').hidden, 'Accepted discard did not remove the copy.');
      assert(clean.querySelector('.CodeMirror').CodeMirror.getValue() === '', 'Discard modified the clean editor.');
      const count = prompts.length;
      clean.querySelector('[data-close-dialog]').click();
      assert(prompts.length === count, 'Unchanged editor warned before closing.');
      await report('PASS');
    }
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
