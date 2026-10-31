// Direct restoration returns to History with a new latest version, without opening the editor.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const targets = __REVISION_TARGETS__;
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
    const stage = sessionStorage.getItem('restore-stage');
    const index = Number(sessionStorage.getItem('restore-index') || 0);
    const target = targets[index];
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('restore-stage', 'navigate');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'navigate') {
      sessionStorage.setItem('restore-stage', 'history');
      location.assign(target.history);
      return;
    }
    const preview = document.querySelector('#revision-preview');
    assert(preview && !document.querySelector('.editor-panel'), 'Restore must remain in History.');
    assert(document.documentElement.scrollWidth <= innerWidth, 'History controls overflow the viewport.');
    assert(!preview.querySelector('button[type=submit]'), 'Current writing must not offer restoration.');
    if (stage === 'history') {
      const previous = document.querySelector('.revision-list a[data-revision="' + target.revision + '"]');
      previous.click();
      await wait(() => preview.querySelector('[data-revision]')?.dataset.revision === String(target.revision) && !preview.classList.contains('htmx-settling'));
      const restore = preview.querySelector('button[type=submit]');
      assert(restore?.textContent === 'Restore this version', 'Older writing must offer Restore.');
      assert(Array.from(preview.querySelectorAll('.form-actions button, .form-actions a'), el => el.textContent).join('|') === 'Restore this version|Edit this version|Delete revision', 'History actions must place Restore first and Delete last.');
      assert(preview.querySelector('a').textContent === 'Edit this version', 'Editing must remain available.');
      sessionStorage.setItem('restore-stage', 'restored');
      restore.click();
      return;
    }
    assert(location.pathname === target.history && !location.search, 'Restore must return to the latest history entry.');
    assert(preview.querySelector('h2').textContent === 'Original title', 'Restore did not apply the saved writing.');
    const links = document.querySelectorAll('.revision-list a');
    assert(links.length === 3 && links[0].getAttribute('aria-current') === 'true', 'Restore must append and select a new version.');
    links[2].click();
    await wait(() => preview.querySelector('[data-revision]')?.dataset.revision === String(target.revision) && !preview.classList.contains('htmx-settling'));
    assert(!preview.querySelector('button[type=submit]') && preview.textContent.includes('matches the current content'), 'Identical older writing must not offer Restore.');
    if (index + 1 < targets.length) {
      sessionStorage.setItem('restore-index', String(index + 1));
      sessionStorage.setItem('restore-stage', 'history');
      location.assign(targets[index + 1].history);
      return;
    }
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
