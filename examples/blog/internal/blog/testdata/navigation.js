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
    await document.fonts.ready;
    assert(!sessionStorage.getItem('navigation-started'), 'Post navigation reloaded the document.');
    sessionStorage.setItem('navigation-started', 'true');
    // HTMX inserts content before its settle phase attaches the new link handlers.
    await wait(() => document.querySelector('#post-archive a[href="/post/s/second"]') &&
      !document.querySelector('#post-archive').classList.contains('htmx-settling'));
    const body = document.body;
    const sidebar = document.querySelector('.site-sidebar');
    const archive = document.querySelector('#post-archive .post-archive');
    for (const link of archive.querySelectorAll('li a')) {
      const style = getComputedStyle(link);
      assert(style.display === 'block' && link.getBoundingClientRect().height >= 24, 'Post links need full-row click targets.');
      assert(style.paddingTop === '2px' && style.paddingBottom === '2px' && style.fontWeight === '400', 'Post links must use compact spacing and regular-weight text.');
      assert(style.textDecorationLine.includes('underline'), 'Post links must remain visibly identifiable as links.');
      assert(link.getBoundingClientRect().width <= archive.getBoundingClientRect().width, 'Post links must stay within the reading list.');
    }
    const article = document.querySelector('#article-content');
    const background = getComputedStyle(body).backgroundImage;
    const pageTop = document.querySelector('.page-top');
    const readingBackground = getComputedStyle(pageTop).backgroundImage;
    const main = document.querySelector('#main-content');
    const contentBackground = getComputedStyle(main).backgroundImage;
    const sidebarWidth = sidebar.getBoundingClientRect().width;
    const contentLeft = main.getBoundingClientRect().left;
    const contentWidth = article.getBoundingClientRect().width;
    const bioWidth = document.querySelector('.profile-details').getBoundingClientRect().width;
    const menuRight = document.querySelector('.menu-toggle').getBoundingClientRect().right;
    const titleTop = document.querySelector('.post-heading').getBoundingClientRect().top;
    const archiveRequests = () => performance.getEntriesByType('resource').filter(entry => new URL(entry.name).pathname === '/posts/recent').length;
    const initialRequests = archiveRequests();
    assert(initialRequests === 1, 'Expected one initial archive request.');
    const unchanged = () => {
      assert(document.body === body && document.querySelector('.site-sidebar') === sidebar, 'Navigation replaced the page shell.');
      assert(document.querySelector('#post-archive .post-archive') === archive, 'Navigation replaced Recent Posts.');
      assert(document.querySelector('#article-content') === article, 'Navigation replaced the article container.');
      assert(getComputedStyle(body).backgroundImage === background, 'Navigation changed the background.');
      assert(document.querySelector('#main-content') === main && getComputedStyle(main).backgroundImage === contentBackground, 'Navigation replaced the content background.');
      assert(archiveRequests() === initialRequests, 'Navigation fetched Recent Posts again.');
      if (innerWidth >= 1680) {
        assert(Math.abs(sidebar.getBoundingClientRect().width - sidebarWidth) < 1, 'Post length changed the sidebar width: ' + sidebarWidth + ' to ' + sidebar.getBoundingClientRect().width);
        assert(Math.abs(main.getBoundingClientRect().left - contentLeft) < 1, 'Post length shifted the content column.');
      } else {
        assert(Math.abs(article.getBoundingClientRect().width - contentWidth) < 1, 'Page height changed the text wrapping width.');
        assert(Math.abs(document.querySelector('.profile-details').getBoundingClientRect().width - bioWidth) < 1, 'Page height changed the bio wrapping width.');
        if (innerWidth < 768) assert(Math.abs(document.querySelector('.menu-toggle').getBoundingClientRect().right - menuRight) < 1, 'Page height moved the mobile menu.');
      }
    };
    const postVisible = slug => location.pathname === '/post/s/' + slug &&
      article.querySelector('h1')?.textContent === slug + ' <post>' && document.title === 'Trent Nix | ' + slug + ' <post>';
    const headerVisible = async () => {
      await wait(() => !article.classList.contains('htmx-settling'));
      assert(scrollY === 0, 'Page navigation must return to the top of the document.');
      if (innerWidth < 1680) assert(document.querySelector('.site-header').getBoundingClientRect().top === 0, 'Navigation must show the full header.');
    };
    const archiveVisible = expected => {
      assert((archive.getClientRects().length > 0) === expected, 'Recent Posts has the wrong visibility for this page.');
    };

    assert(postVisible('first'), 'Direct post load has the wrong title or content.');
    const assertHeading = () => {
      const style = getComputedStyle(article.querySelector('h1'));
      assert(Math.abs(parseFloat(style.lineHeight) / parseFloat(style.fontSize) - 1.2) < .01, 'Page titles must use consistent line spacing.');
    };
    assertHeading();
    archiveVisible(true);
    assert(document.documentElement.scrollHeight > innerHeight, 'First post must require page scrolling.');
    if (innerWidth < 1680) {
      archive.querySelector('a[href="/post/s/second"]').scrollIntoView();
      assert(scrollY > 0, 'Footer navigation test must start below the header.');
    }
    archive.querySelector('a[href="/post/s/second"]').click();
    await wait(() => postVisible('second'));
    await headerVisible();
    if (innerWidth >= 1680) assert(document.documentElement.scrollHeight <= innerHeight, 'Second post must fit without page scrolling.');
    unchanged();
    history.back();
    await wait(() => postVisible('first'));
    unchanged();
    history.forward();
    await wait(() => postVisible('second'));
    unchanged();

    const menu = document.querySelector('#site-menu');
    const menuToggle = document.querySelector('.menu-toggle');
    const pageLinks = innerWidth < 768 ? menu : document.querySelector('.profile-links');
    if (innerWidth < 768) menuToggle.click();
    pageLinks.querySelector('a[href="/about"]').click();
    await wait(() => location.pathname === '/about' && document.title === 'Trent Nix | About Me');
    await headerVisible();
    assert(article.querySelector('h1')?.textContent === 'About Me', 'About navigation missed the article.');
    assertHeading();
    archiveVisible(innerWidth >= 1680);
    const aboutProfileLeft = document.querySelector('.profile-name').getBoundingClientRect().left;
    const aboutTitleLeft = article.querySelector('h1').getBoundingClientRect().left;
    if (innerWidth < 768) {
      assert(menu.hidden && menuToggle.getAttribute('aria-expanded') === 'false', 'Navigation must close the mobile menu.');
    }
    unchanged();
    if (innerWidth < 768) menuToggle.click();
    pageLinks.querySelector('a[href="/projects"]').click();
    await wait(() => location.pathname === '/projects' && document.title === 'Trent Nix | Projects');
    await headerVisible();
    assert(document.documentElement.scrollHeight > innerHeight, 'Projects fixture must require scrolling.');
    assert(Math.abs(document.querySelector('.profile-name').getBoundingClientRect().left - aboutProfileLeft) < 1, 'About-to-Projects navigation shifted the profile.');
    assert(Math.abs(article.querySelector('h1').getBoundingClientRect().left - aboutTitleLeft) < 1, 'About-to-Projects navigation shifted the content.');
    archiveVisible(innerWidth >= 1680);
    unchanged();
    if (innerWidth < 768) menuToggle.click();
    const homeLink = innerWidth < 768 ? menu.querySelector('a[href="/"]') : document.querySelector('.profile-name');
    homeLink.click();
    await wait(() => location.pathname === '/' && article.querySelector('h1')?.textContent === 'second <post>');
    await headerVisible();
    archiveVisible(true);
    unchanged();
    assert(archive.querySelector('a[href="/rss"]').getAttribute('hx-boost') === 'false', 'Archive RSS link must use ordinary navigation.');
    assert(!document.querySelector('.profile a[href="/rss"]'), 'RSS must not clutter the profile links.');
    assert(document.querySelector('link[rel="alternate"][type="application/rss+xml"]').getAttribute('href') === '/rss', 'Automatic feed discovery must remain available.');

    archive.querySelector('a[href="/posts"]').click();
    await wait(() => location.pathname === '/posts' && document.title === 'Trent Nix | All Posts');
    await headerVisible();
    assert(article.querySelector('h1')?.textContent === 'All Posts', 'All posts link must open the full archive.');
    assertHeading();
    if (innerWidth < 1680) assert(getComputedStyle(pageTop).backgroundImage === 'none', 'The full archive must keep its links clear of the illustration.');
    archiveVisible(innerWidth >= 1680);
    unchanged();
    article.querySelector('a[href="/post/s/second"]').click();
    await wait(() => postVisible('second'));
    await headerVisible();
    assert(getComputedStyle(pageTop).backgroundImage === readingBackground, 'Returning to a post must restore its illustration.');
    archiveVisible(true);
    unchanged();

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
    await headerVisible();
    if (innerWidth >= 1680) {
      const top = document.querySelector('.post-heading').getBoundingClientRect().top;
      assert(Math.abs(top - titleTop) < 1, 'Navigation changed the space above the post: ' + titleTop + ' to ' + top);
    }
    unchanged();
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
