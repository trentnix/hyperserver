// Selection must leave the existing document and long revision list in place.
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
  const frame = () => new Promise(resolve => requestAnimationFrame(resolve));
  try {
    const phase = sessionStorage.getItem('revision-navigation');
    if (!phase) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('revision-navigation', 'logged-in');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (phase === 'logged-in') {
      sessionStorage.setItem('revision-navigation', 'history');
      location.assign(targets[0].history);
      return;
    }
    const index = Number(sessionStorage.getItem('revision-navigation-index') || 0);
    const target = targets[index];
    const list = document.querySelector('.revision-list');
    const preview = document.querySelector('#revision-preview');
    const links = [...list.querySelectorAll('a')];
    assert(links.length === 40, 'Long-history fixture needs 40 versions.');
    links[20].scrollIntoView({block: 'center'});
    links[20].focus({preventScroll: true});
    await frame();
    const top = window.scrollY;
    assert(top > 100, 'The long-history test must start scrolled down.');
    let jumped = false;
    const record = () => { jumped ||= Math.abs(window.scrollY - top) > 1; };
    window.addEventListener('scroll', record);
    const selected = link => preview.querySelector('[data-revision]')?.dataset.revision === link.dataset.revision && !preview.classList.contains('htmx-settling');
    links[20].click();
    await wait(() => selected(links[20]));
    assert(document.querySelector('.revision-list') === list && document.querySelector('#revision-preview') === preview, 'Selection replaced the page or revision list.');
    assert(list.querySelectorAll('[aria-current]').length === 1 && links[20].getAttribute('aria-current') === 'true', 'Selected revision marker is incorrect.');
    assert(location.pathname + location.search === links[20].getAttribute('href'), 'Refresh/bookmark URL lost the selected revision.');
    assert(preview.querySelector('.form-actions a').search === links[20].search, 'Edit opens the wrong revision.');

    // Only the last choice should win when requests overlap.
    links[22].click();
    links[24].click();
    await wait(() => selected(links[24]));
    assert(links[24].getAttribute('aria-current') === 'true', 'Rapid selection showed an outdated marker.');
    const selectedURL = location.href;
    await htmx.ajax('GET', target.history + '?revision=999999999', {target: preview, swap: 'innerHTML show:none'});
    assert(location.href === selectedURL && links[24].getAttribute('aria-current') === 'true', 'Failed selection changed the URL or marker.');
    assert(document.querySelector('#revision-preview') === preview, 'An error removed the preview target.');
    links[26].click();
    await wait(() => selected(links[26]));
    assert(links[26].getAttribute('aria-current') === 'true', 'Selection failed after an error.');
    await frame();
    record();
    window.removeEventListener('scroll', record);
    assert(!jumped, 'Revision selection moved the page: ' + JSON.stringify({before: top, after: window.scrollY}));
    assert(list.querySelectorAll('a').length === 40 && list.scrollTop === 0, 'Selection changed the revision list.');

    // Deletion must stay on this document instead of redirecting to the top.
    preview.querySelector('a[hx-get]').scrollIntoView({block: 'center'});
    if (innerWidth >= 768) window.scrollTo(0, 80);
    await frame();
    const deleteTop = window.scrollY;
    assert(deleteTop > 0, 'Delete regression must start scrolled down.');
    preview.querySelector('a[hx-get]').click();
    await wait(() => document.querySelector('#dialog-host dialog[open]') && !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
    document.querySelector('#dialog-host button[type="submit"]').click();
    await wait(() => selected(links[27]) && !document.querySelector('dialog[open]'));
    assert(document.querySelector('.revision-list') === list && list.children.length === 39 && !links[26].isConnected, 'Deleting a revision replaced the document or left its list entry.');
    assert(window.scrollY > 0 && Math.abs(window.scrollY - deleteTop) < 200, 'Deletion jumped away from the history position: ' + JSON.stringify({before: deleteTop, after: window.scrollY}));

    if (index + 1 < targets.length) {
      sessionStorage.setItem('revision-navigation-index', String(index + 1));
      location.assign(targets[index + 1].history);
      return;
    }
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
