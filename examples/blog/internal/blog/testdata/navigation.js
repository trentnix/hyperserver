// Run against real page, fragment, and history responses from the blog.
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
  try {
    assert(!sessionStorage.getItem('navigation-started'), 'Post navigation reloaded the document.');
    sessionStorage.setItem('navigation-started', 'true');
    // HTMX inserts content before its settle phase attaches the new link handlers.
    await wait(() => document.querySelector('#post-archive a[href="/post/s/second"]') &&
      !document.querySelector('#post-archive').classList.contains('htmx-settling'));
    const body = document.body;
    const sidebar = document.querySelector('.site-sidebar');
    const archive = document.querySelector('.post-archive');
    const article = document.querySelector('#article-content');
    const background = getComputedStyle(body).backgroundImage;
    const archiveRequests = () => performance.getEntriesByType('resource').filter(entry => new URL(entry.name).pathname === '/posts').length;
    const initialRequests = archiveRequests();
    assert(initialRequests === 1, 'Expected one initial archive request.');
    const unchanged = () => {
      assert(document.body === body && document.querySelector('.site-sidebar') === sidebar, 'Navigation replaced the page shell.');
      assert(document.querySelector('.post-archive') === archive, 'Navigation replaced Recent Posts.');
      assert(document.querySelector('#article-content') === article, 'Navigation replaced the article container.');
      assert(getComputedStyle(body).backgroundImage === background, 'Navigation changed the background.');
      assert(archiveRequests() === initialRequests, 'Navigation fetched Recent Posts again.');
    };
    const postVisible = slug => location.pathname === '/post/s/' + slug &&
      article.querySelector('h1')?.textContent === slug + ' <post>' && document.title === 'Trent Nix | ' + slug + ' <post>';

    assert(postVisible('first'), 'Direct post load has the wrong title or content.');
    archive.querySelector('a[href="/post/s/second"]').click();
    await wait(() => postVisible('second'));
    unchanged();
    history.back();
    await wait(() => postVisible('first'));
    unchanged();
    history.forward();
    await wait(() => postVisible('second'));
    unchanged();

    document.querySelector('footer a[href="/about"]').click();
    await wait(() => location.pathname === '/about' && document.title === 'Trent Nix | About Me');
    assert(article.querySelector('h1')?.textContent === 'About Me', 'About navigation missed the article.');
    unchanged();
    document.querySelector('.profile a[href="/projects"]').click();
    await wait(() => location.pathname === '/projects' && document.title === 'Trent Nix | Projects');
    unchanged();
    document.querySelector('footer a[href="/"]').click();
    await wait(() => location.pathname === '/' && article.querySelector('h1')?.textContent === 'second <post>');
    unchanged();
    assert(document.querySelector('.profile a[href="/rss"]').getAttribute('hx-boost') === 'false', 'RSS must use ordinary navigation.');

    // A stale archive link must show a styled error without discarding the sidebar.
    const missing = document.createElement('a');
    missing.href = '/post/s/missing';
    missing.textContent = 'Removed post';
    archive.append(missing);
    htmx.process(missing);
    missing.click();
    await wait(() => article.textContent.includes('That page could not be found.'));
    unchanged();
    archive.querySelector('a[href="/post/s/first"]').click();
    await wait(() => postVisible('first'));
    unchanged();
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
