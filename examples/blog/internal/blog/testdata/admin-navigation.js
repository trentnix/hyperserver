// Management links replace the article without reloading the shared page shell.
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
  window.addEventListener('error', event => report(event.message));
  try {
    if (!sessionStorage.getItem('admin-navigation-login')) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('admin-navigation-login', 'true');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    assert(!sessionStorage.getItem('admin-navigation-started'), 'Management navigation reloaded the document.');
    sessionStorage.setItem('admin-navigation-started', 'true');
    await wait(() => document.querySelector('#post-archive .post-archive') && !document.querySelector('#post-archive').classList.contains('htmx-settling'));
    const shell = ['body', '.site-sidebar', '.account-panel', '#article-content', '#post-archive', '#post-archive .post-archive'].map(selector => [selector, document.querySelector(selector)]);
    const article = document.querySelector('#article-content');
    const account = document.querySelector('.account-menu');
    const archiveRequests = () => performance.getEntriesByType('resource').filter(entry => new URL(entry.name).pathname === '/posts/recent').length;
    const initialRequests = archiveRequests();
    const unchanged = () => {
      for (const [selector, element] of shell) assert(document.querySelector(selector) === element, 'Navigation replaced ' + selector);
      assert(archiveRequests() === initialRequests, 'Management navigation reloaded Recent Posts.');
      assert(!document.querySelector('dialog[open]'), 'Management navigation opened a dialog.');
      assert(!article.querySelector('html, body, .site-layout'), 'Management fragment included the page shell.');
    };
    const ready = async (path, title) => {
      await wait(() => location.pathname === path && article.querySelector('h1')?.textContent === title && !article.classList.contains('htmx-settling'));
      assert(document.title === 'Trent Nix | ' + title, 'Management navigation lost the document title.');
      unchanged();
    };
    for (const [path, title] of [['/post/manage', 'Posts'], ['/pages', 'Pages'], ['/profile', 'Site Profile'], ['/media', 'Media']]) {
      if (innerWidth < 768) account.querySelector('summary').click();
      account.querySelector(`a[href="${path}"]`).click();
      await ready(path, title);
      if (innerWidth < 768) assert(!account.open, 'Management navigation left the mobile menu open.');
      if (path === '/post/manage' || path === '/pages') {
        const form = article.querySelector('.management-search');
        const summaryHeight = article.querySelector('.management-search-results').getBoundingClientRect().height;
        const initialCount = article.querySelector('[data-result-count]');
        assert(Number(initialCount.dataset.resultCount) === article.querySelectorAll('[data-content-id]').length, 'Unfiltered lists must count all items, including subpages.');
        const field = form.elements.q.getBoundingClientRect();
        const button = form.querySelector('button').getBoundingClientRect();
        assert(field.left >= 0 && button.right <= innerWidth && Math.abs(field.height - button.height) < 1 && Math.abs(field.top - button.top) < 1 && button.left > field.right, 'Content search controls must share one row, align, and fit the viewport.');
        assert(Math.abs(form.getBoundingClientRect().width - article.querySelector('.page-manager').getBoundingClientRect().width) < 1, 'Search must fill the management content width.');
        form.elements.q.value = 'needle';
        form.requestSubmit();
        const resultsReady = () => location.search === '?q=needle' && article.querySelector('[role=status]')?.textContent.includes('3 ') && !article.classList.contains('htmx-settling');
        await wait(resultsReady);
        const count = article.querySelector('.management-search-results [role=status]').getBoundingClientRect();
        const clear = article.querySelector('.management-search-clear').getBoundingClientRect();
        assert(Math.abs(article.querySelector('.management-search-results').getBoundingClientRect().height - summaryHeight) < 1, 'Searching must not change the result summary height.');
        assert(Math.abs(count.top - clear.top) < 1 && clear.left > count.right && clear.left - count.right <= 21, 'Clear search must sit beside the result count, not at the far edge.');
        const updatedField = article.querySelector('.management-search input').getBoundingClientRect();
        assert(Math.abs(updatedField.width - field.width) < 1, 'Showing results must not resize the search field.');
        const titles = [...article.querySelectorAll('.page-manager-title > a')].map(link => link.textContent);
        const expected = path === '/post/manage' ? ['Summary result', 'Body result', 'Needle title'] : ['Needle title', 'Summary result', 'Body result'];
        assert(JSON.stringify(titles) === JSON.stringify(expected), 'Post search must preserve publication groups. Page search must rank title, summary, then content.');
        if (path === '/post/manage') {
          for (const id of ['draft-posts', 'scheduled-posts', 'published-posts']) assert(article.querySelector('#' + id + ' .page-manager-row'), 'Search lost the ' + id + ' section.');
        }
        unchanged();
        article.querySelector('.management-search-clear a').click();
        await wait(() => location.search === '' && !article.querySelector('.management-search-clear') && !article.classList.contains('htmx-settling'));
        assert(article.textContent.includes('Other result'), 'Clearing search must restore the full list.');
        assert(Math.abs(article.querySelector('.management-search-results').getBoundingClientRect().height - summaryHeight) < 1, 'Clearing search must preserve the result summary height.');
        unchanged();
        history.back();
        await wait(resultsReady);
        assert(article.querySelector('.management-search').elements.q.value === 'needle', 'Back must restore the search query.');
        unchanged();
        history.forward();
        await wait(() => location.search === '' && !article.querySelector('.management-search-clear') && !article.classList.contains('htmx-settling'));
        unchanged();
      }
      if (path === '/profile') assert(!article.querySelector('.image-upload-controls').hidden, 'Profile image controls were not initialized after navigation.');
      if (path === '/media') assert(!article.querySelector('[data-media-upload]').hidden, 'Media upload was not initialized after navigation.');
    }
    history.back();
    await ready('/profile', 'Site Profile');
    assert(!article.querySelector('.image-upload-controls').hidden, 'History navigation did not initialize profile controls.');
    history.forward();
    await ready('/media', 'Media');
    assert(!article.querySelector('[data-media-upload]').hidden, 'History navigation did not initialize media upload.');
    account.querySelector('a[href="/pages"]').click();
    await ready('/pages', 'Pages');
    const removePage = async row => {
      const list = article.querySelector('[data-content-list]');
      const rows = [...list.querySelectorAll('[data-content-id]')];
      const index = rows.indexOf(row);
      const next = rows[index + 1] || rows[index - 1];
      const focus = next?.querySelector('.page-manager-actions a') || list.querySelector('input[type=search]');
      const url = row.querySelector('.page-manager-title a').getAttribute('href');
      row.scrollIntoView({block: 'center'});
      row.querySelector('a[href^="/pages/delete/"]').click();
      await wait(() => document.querySelector('dialog[open] form[action^="/pages/delete/"]') && !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
      const before = scrollY;
      document.querySelector('dialog[open] form').requestSubmit();
      await wait(() => !row.isConnected && !document.querySelector('dialog[open]'));
      await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
      const expected = Math.min(before, Math.max(0, document.scrollingElement.scrollHeight - document.scrollingElement.clientHeight));
      assert(Math.abs(scrollY - expected) <= 1, 'Page deletion lost scroll position: ' + JSON.stringify({before, expected, actual: scrollY}));
      assert(article.querySelector('[data-content-list]') === list && document.activeElement === focus, 'Page deletion replaced the list or lost adjacent focus.');
      assert(![...document.querySelectorAll('#site-menu a, .profile-links a')].some(link => link.getAttribute('href') === url), 'Deleted page remains in navigation.');
      assert((await fetch(url)).status === 404, 'Deleted page remains readable.');
      const count = list.querySelector('[data-result-count]');
      if (count) assert(Number(count.dataset.resultCount) === list.querySelectorAll('[data-content-id]').length, 'Page deletion left a stale result count.');
      unchanged();
    };
    const search = article.querySelector('.management-search');
    search.elements.q.value = 'needle';
    search.requestSubmit();
    await wait(() => article.querySelector('[data-result-count]')?.dataset.resultCount === '3' && !article.classList.contains('htmx-settling'));
    while (article.querySelector('[data-content-id]')) await removePage(article.querySelector('[data-content-id]'));
    assert(location.search === '?q=needle' && article.querySelector('.page-manager-empty').textContent === 'No pages match your search.', 'Deleting page matches lost the query or empty state.');
    article.querySelector('.management-search-clear a').click();
    await wait(() => location.search === '' && !article.querySelector('.management-search-clear') && !article.classList.contains('htmx-settling'));
    while (article.querySelector('[data-content-id]')) {
      const leaf = [...article.querySelectorAll('[data-content-id]')].find(row => !row.querySelector('[data-content-id]'));
      await removePage(leaf);
    }
    assert(article.querySelector('.page-manager-empty').textContent === 'No pages yet. Create a page to get started.', 'Deleting the last page must show the normal empty state.');
    assert(!document.querySelector('.child-toggle, .profile-links .child-links'), 'Deleting final subpages must remove empty navigation branches.');
    const width = window.frameElement.style.width;
    window.frameElement.style.width = innerWidth < 768 ? '1100px' : '320px';
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    window.frameElement.style.width = width;
    assert(!document.querySelector('.account-nav form').closest('[hx-boost="true"]'), 'Logout must retain ordinary navigation.');
    await report('PASS');
  } catch (error) {
    await report(error.stack || String(error));
  }
})();
