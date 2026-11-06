// Reproduce a browser-restored document after another request changes storage.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const stageKey = 'management-refresh-stage';
  const create = async (kind, title) => {
    const fields = new URLSearchParams({title, content: 'Body', childrenDisplay: 'hidden'});
    const response = await fetch(kind === 'post' ? '/post' : '/pages', {method: 'POST', body: fields});
    assert(response.ok, 'Unable to create ' + kind + ': ' + response.status);
  };
  window.addEventListener('error', event => report(event.message));
  try {
    const stage = sessionStorage.getItem(stageKey);
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem(stageKey, 'setup');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'setup') {
      await create('post', 'Freshness original post');
      await create('page', 'Freshness original page');
      sessionStorage.setItem(stageKey, 'post');
      location.assign('/post/manage?q=Freshness');
      return;
    }
    const kind = stage.startsWith('post') ? 'post' : 'page';
    const path = kind === 'post' ? '/post/manage' : '/pages';
    assert(location.pathname === path && location.search === '?q=Freshness', 'Restoration lost the list or its search query.');
    const titles = [...document.querySelectorAll('.page-manager-title > a')].map(link => link.textContent);
    assert(titles.includes('Freshness original ' + kind), 'The original item is missing.');
    if (stage.endsWith('-restored')) {
      assert(titles.includes('Freshness updated ' + kind), 'The restored list showed stale data.');
      assert(document.querySelector('[data-result-count]').dataset.resultCount === '2', 'Restoration left a stale result count.');
      if (kind === 'post') {
        sessionStorage.setItem(stageKey, 'page');
        location.assign('/pages?q=Freshness');
      } else {
        await report('PASS');
      }
      return;
    }
    await create(kind, 'Freshness updated ' + kind);
    assert(!document.querySelector('#article-content').textContent.includes('Freshness updated ' + kind), 'The test must restore an outdated document.');
    sessionStorage.setItem(stageKey, kind + '-restored');
    // BFCache eligibility varies by browser. Deliver its real restoration signal
    // so the test exercises refreshing both cached lists deterministically.
    window.dispatchEvent(new PageTransitionEvent('pageshow', {persisted: true}));
    setTimeout(() => report('The restored ' + kind + ' list did not refresh.'), 1500);
  } catch (error) {
    await report(error.stack || String(error));
  }
})();
