// Exercise the real confirmation UI rather than replacing it with automatic consent.
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
  const edit = async (path = '/post/create') => {
    await htmx.ajax('GET', path, '#dialog-host');
    await wait(() => document.querySelector('.editor-dialog .CodeMirror') && document.querySelector('[data-draft-status]:not([hidden])'));
    await wait(() => !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
    return document.querySelector('#post-form');
  };
  const prompt = title => {
    const dialog = document.querySelector('.editor-confirmation[open]');
    assert(dialog && dialog.querySelector('h2').textContent === title, 'Missing confirmation: ' + title);
    assert(document.activeElement === dialog.querySelector('[data-confirm-cancel]'), 'Cancel must receive initial focus.');
    assert(dialog.getAttribute('aria-labelledby') && dialog.getAttribute('aria-describedby'), 'Confirmation must have an accessible title and description.');
    const box = dialog.getBoundingClientRect();
    assert(box.left >= 0 && box.right <= innerWidth && box.top >= 0 && box.bottom <= innerHeight && dialog.scrollWidth <= dialog.clientWidth, 'Confirmation must fit the viewport.');
    if (innerWidth < 768) {
      for (const button of dialog.querySelectorAll('button')) {
        assert(button.getBoundingClientRect().height >= 44 && button.getBoundingClientRect().width > box.width / 2, 'Mobile confirmation buttons need full-width touch targets.');
      }
    }
    return dialog;
  };
  const answer = (dialog, accepted) => dialog.querySelector(accepted ? '[data-confirm-accept]' : '[data-confirm-cancel]').click();
  window.confirm = () => { throw new Error('Editor used a browser confirmation.'); };
  window.addEventListener('error', event => report(event.message));
  try {
    const stage = sessionStorage.getItem('confirmations-stage');
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('confirmations-stage', 'editor');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'saved') {
      assert(location.pathname === '/post/s/changed-confirmation-url', 'Accepted URL change did not save.');
      await report('PASS');
      return;
    }

    let form = await edit();
    form.querySelector('.CodeMirror').CodeMirror.setValue('Recover this writing.');
    const close = form.querySelector('[data-close-dialog]');
    close.focus();
    close.click();
    let dialog = prompt('Leave without saving?');
    assert(form.closest('dialog').open, 'The editor must stay open while confirmation is pending.');
    window.blogConfirm({title: 'Duplicate', message: 'Ignored', action: 'Ignore'}, () => { throw new Error('Duplicate confirmation ran'); });
    assert(document.querySelectorAll('.editor-confirmation').length === 1, 'Repeated actions must not stack confirmations.');
    answer(dialog, false);
    assert(form.closest('dialog').open && document.activeElement === close, 'Cancel must keep the editor and return focus.');
    close.click();
    dialog = prompt('Leave without saving?');
    dialog.dispatchEvent(new Event('cancel', {cancelable: true}));
    assert(!document.querySelector('.editor-confirmation') && form.closest('dialog').open, 'Escape must cancel without closing the editor.');
    const unload = new Event('beforeunload', {cancelable: true});
    window.dispatchEvent(unload);
    assert(unload.defaultPrevented, 'Cancel must not suppress the browser unload warning.');
    close.click();
    answer(prompt('Leave without saving?'), true);
    assert(!form.closest('dialog').open, 'Accepting leave must close the editor.');

    const copyKey = Object.keys(localStorage).find(key => key.startsWith('hyperserver.blog.draft.v1:' + form.dataset.draftOwner + ':/post:'));
    const originalCopy = JSON.parse(localStorage.getItem(copyKey));
    const olderKey = copyKey + '-older';
    const olderCopy = JSON.stringify({...originalCopy, saved: originalCopy.saved - 1000, fields: {...originalCopy.fields, content: 'An older copy.'}});
    localStorage.setItem(olderKey, olderCopy);
    form = await edit();
    assert(!form.querySelector('[data-draft-recovery]').hidden, 'Leaving must preserve a recoverable copy.');
    form.querySelector('[data-draft-discard]').click();
    answer(prompt('Discard this browser copy?'), false);
    assert(!form.querySelector('[data-draft-recovery]').hidden, 'Canceling discard must keep the copy.');
    form.querySelector('.CodeMirror').CodeMirror.setValue('Current edits.');
    form.querySelector('[data-draft-restore]').click();
    answer(prompt('Replace your current edits?'), false);
    assert(form.elements.content.value === 'Current edits.', 'Canceling restore changed the editor.');
    form.querySelector('[data-draft-restore]').click();
    answer(prompt('Replace your current edits?'), true);
    assert(form.elements.content.value === 'Recover this writing.', 'Accepting restore did not load the copy.');
    const recovery = form.querySelector('[data-draft-recovery]');
    assert(recovery.hidden && recovery.getClientRects().length === 0, 'Restoring must dismiss the recovery panel even when other copies exist.');
    assert(form.querySelector('[data-draft-status]').textContent === 'Copy restored. Save to apply changes.', 'Restore must explain that the recovered content still needs saving.');
    assert(localStorage.getItem(olderKey) === olderCopy, 'Restore must preserve other browser copies.');
    form.querySelector('[data-close-dialog]').click();
    answer(prompt('Leave without saving?'), true);
    form = await edit();
    assert(!form.querySelector('[data-draft-recovery]').hidden && localStorage.getItem(olderKey) === olderCopy, 'Reopening must keep the remaining copies available.');
    form.querySelector('[data-draft-discard]').click();
    answer(prompt('Discard this browser copy?'), true);
    assert(form.querySelector('[data-draft-recovery]').hidden && form.elements.content.value === '', 'Discard must dismiss recovery without changing the current editor.');
    form.querySelector('[data-close-dialog]').click();

    const response = await fetch('/post', {method: 'POST', headers: {'HX-Request': 'true'}, body: new URLSearchParams({title: 'Confirmation target', slug: 'confirmation-target', content: 'Published body.', published: 'on'})});
    assert(response.ok, 'Unable to create a published post.');
    const page = new DOMParser().parseFromString(await (await fetch(response.headers.get('HX-Redirect'))).text(), 'text/html');
    form = await edit(page.querySelector('a[href^="/post/update/"]').getAttribute('href'));
    form.elements.slug.value = 'changed-confirmation-url';
    form.requestSubmit();
    answer(prompt('Change this URL?'), false);
    assert(form.isConnected && form.elements.slug.value === 'changed-confirmation-url', 'Canceling a URL change lost editor fields.');
    form.requestSubmit();
    sessionStorage.setItem('confirmations-stage', 'saved');
    answer(prompt('Change this URL?'), true);
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
