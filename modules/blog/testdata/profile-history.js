// Profile history uses the same navigation as writing history, without publishing on selection.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate);
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  try {
    let stage = sessionStorage.getItem('profile-history-stage');
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('profile-history-stage', 'profile');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'profile') {
      if (innerWidth < 768) document.querySelector('.account-menu summary').click();
      document.querySelector('.account-nav a[href="/profile"]').click();
      await wait(() => document.querySelector('#profile-form') && !document.querySelector('#article-content').classList.contains('htmx-settling'));
      sessionStorage.setItem('profile-history-stage', 'history');
      const history = document.querySelector('.profile-editor a[href="/profile/history"]');
      const style = getComputedStyle(history);
      assert(style.display.includes('flex') && style.borderTopWidth === '1px' && style.textDecorationLine === 'none', 'History must be styled as a button.');
      history.click();
      return;
    }
    if (stage === 'saved') {
      assert(location.pathname === '/' && !location.search, 'Save did not return to the site root.');
      assert(document.querySelector('#notifications').textContent.includes('Profile saved'), 'Missing profile confirmation.');
      assert(document.querySelector('.profile-name').textContent === 'Profile 00', 'Save did not publish the restored name.');
      sessionStorage.setItem('profile-history-stage', 'saved-editor');
      location.href = '/profile';
      return;
    }
    assert(document.documentElement.scrollWidth <= innerWidth, 'Profile history overflows the viewport.');
    assert(!document.querySelector('.reader-search-form'), 'Profile editing and history must not contain reader search markup.');
    if (stage === 'history') {
      const shell = document.querySelector('.history-page');
      const links = [...document.querySelectorAll('.revision-list a')];
      const preview = document.querySelector('#revision-preview');
      assert(links.length === 13 && preview.querySelector('h2').textContent === 'Profile 11', 'History must start with the latest profile.');
      assert(!preview.querySelector('button[type=submit]'), 'Current version must not offer restoration.');
      assert(!preview.querySelector('details'), 'Profile history must not show Markdown controls.');
      const selected = links[links.length - 2];
      selected.scrollIntoView({block: 'center'});
      const scroll = window.scrollY;
      selected.click();
      await wait(() => preview.querySelector('[data-revision]')?.dataset.revision === selected.dataset.revision && !preview.classList.contains('htmx-settling'));
      assert(document.querySelector('.history-page') === shell, 'Selecting a version replaced the history page.');
      assert(Math.abs(window.scrollY - scroll) < 2, 'Selecting a profile version changed scroll position.');
      assert(selected.getAttribute('aria-current') === 'true', 'Selected revision is not identified.');
      assert(preview.querySelectorAll('.missing-image').length === 3, 'History must explain deleted images for all three roles.');
      assert(preview.querySelector('.form-actions a').textContent === 'Edit' && preview.querySelector('button[type=submit]').textContent === 'Restore this version', 'Previous versions must offer Edit and Restore.');
      assert(Array.from(preview.querySelectorAll('.form-actions button, .form-actions a'), el => el.textContent).join('|') === 'Restore this version|Edit|Delete revision', 'Profile history actions must place Restore first and Delete last.');
      assert(document.querySelector('.profile-name').textContent === 'Profile 11', 'Preview changed the public profile.');
      assert(new URLSearchParams(location.search).get('revision') === selected.dataset.revision, 'Selection did not update the URL.');
      sessionStorage.setItem('profile-history-stage', 'edit');
      preview.querySelector('.form-actions a').click();
      return;
    }
    const form = document.querySelector('#profile-form');
    if (stage === 'edit') {
      assert(form.elements.name.value === 'Profile 00', 'History did not load the selected content.');
      assert(form.elements.version.value === '13', 'History reused an old edit token.');
      assert(document.querySelector('.profile-name').textContent === 'Profile 11', 'Loading history published it.');
      assert(document.querySelector('.revision-notice').textContent.includes('Save to apply'), 'Editor lacks an unsaved-history notice.');
      assert(form.querySelectorAll('[data-image-missing]').length === 3, 'Editor must mark missing images.');
      form.requestSubmit();
      await wait(() => document.querySelector('#profile-errors').textContent.includes('no longer available'));
      for (const reset of form.querySelectorAll('[data-image-reset]')) reset.click();
      assert(!form.querySelector('[data-image-missing]'), 'Reset left a stale deleted-image placeholder.');
      assert([...form.querySelectorAll('[data-image-value]')].every(input => input.value === ''), 'Reset did not remove missing references.');
      sessionStorage.setItem('profile-history-stage', 'saved');
      form.requestSubmit();
      return;
    }
    if (stage === 'saved-editor') {
      assert(location.pathname === '/profile' && !location.search, 'Save did not return to the current profile.');
      assert(document.querySelector('.profile-name').textContent === 'Profile 00', 'Save did not publish the restored name.');
      assert(form.elements.version.value === '14' && !document.querySelector('.revision-notice'), 'Save did not advance the edit token or clear the restore notice.');
      sessionStorage.setItem('profile-history-stage', 'restore');
      document.querySelector('.profile-editor a[href="/profile/history"]').click();
      return;
    }
    if (stage === 'restore') {
      const selected = document.querySelectorAll('.revision-list a')[1];
      selected.click();
      const preview = document.querySelector('#revision-preview');
      await wait(() => preview.querySelector('[data-revision]')?.dataset.revision === selected.dataset.revision && !preview.classList.contains('htmx-settling'));
      assert(preview.querySelector('h2').textContent === 'Profile 11', 'Wrong version selected for restore.');
      sessionStorage.setItem('profile-history-stage', 'restored');
      preview.querySelector('button[type=submit]').click();
      return;
    }
    assert(stage === 'restored' && location.pathname === '/profile/history' && !location.search, 'Direct restore did not return to history.');
    const links = [...document.querySelectorAll('.revision-list a')];
    const preview = document.querySelector('#revision-preview');
    assert(document.querySelector('.profile-name').textContent === 'Profile 11' && preview.querySelector('h2').textContent === 'Profile 11', 'Direct restore did not update the profile and header.');
    assert(links.length === 15, 'Restore did not add a new entry to the version stack.');
    assert(links[0].getAttribute('aria-current') === 'true' && preview.querySelector('[data-revision]').dataset.revision === links[0].dataset.revision, 'Restore did not select the newest version.');
    assert(!preview.querySelector('button[type=submit]'), 'Newly restored current version offers restoration.');
    const previous = links[2];
    previous.click();
    await wait(() => preview.querySelector('[data-revision]')?.dataset.revision === previous.dataset.revision && !preview.classList.contains('htmx-settling'));
    assert(preview.querySelector('h2').textContent === 'Profile 11', 'Expected an identical older version.');
    assert(!preview.querySelector('button[type=submit]') && preview.textContent.includes('This version matches the current content.'), 'Identical history must not offer restoration.');
    const shell = document.querySelector('.history-page');
    previous.scrollIntoView({block: 'center'});
    const scroll = window.scrollY;
    const remove = preview.querySelector('a[href*="/delete/"]');
    assert(remove, 'An older matching version must still allow deletion.');
    remove.click();
    await wait(() => document.querySelector('#dialog-host dialog[open]') && !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
    document.querySelector('#dialog-host [data-close-dialog]').click();
    assert(previous.isConnected, 'Cancel removed history.');
    remove.click();
    await wait(() => document.querySelector('#dialog-host dialog[open]') && !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
    document.querySelector('#dialog-host form').requestSubmit();
    await wait(() => !previous.isConnected && !document.querySelector('dialog[open]') && preview.querySelector('[data-revision]')?.dataset.revision !== previous.dataset.revision && !preview.classList.contains('htmx-settling'));
    assert(document.querySelector('.history-page') === shell && document.querySelectorAll('.revision-list a').length === 14, 'Deletion must update history in place.');
    assert(Math.abs(window.scrollY - scroll) < 2, 'Deletion scrolled profile history.');
    assert(document.querySelector('.profile-name').textContent === 'Profile 11', 'Deleting history changed the live profile.');
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
