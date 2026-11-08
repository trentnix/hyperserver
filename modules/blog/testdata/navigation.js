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
    const signInLinks = [...document.querySelectorAll('.header-sign-in')];
    assert(signInLinks.length === 3 && signInLinks.every(link => link.closest('.site-header, .footer-account')), 'Sign-in controls must belong to the responsive header or footer.');
    assert(signInLinks.every(link => link.getAttribute('hx-boost') === 'false'), 'Sign-in navigation must refresh the account shell.');
    assert(!document.querySelector('.account-panel a[href="/login"]'), 'Sign in remains in the content area.');
    const checkSignIn = () => {
      const visible = signInLinks.filter(link => link.getClientRects().length > 0);
      assert(visible.length === 1, 'Only one responsive sign-in link should be visible.');
      const bounds = visible[0].getBoundingClientRect();
      assert(bounds.left >= 0 && bounds.right <= document.documentElement.clientWidth, 'Sign in overflows the header.');
      if (innerWidth < 768) assert(bounds.height >= 44, 'Mobile sign-in needs a touch-sized target.');
      else if (innerWidth < 1680) {
        const footer = document.querySelector('.site-footer').getBoundingClientRect();
        const archive = document.querySelector('#post-archive');
        assert(visible[0].closest('.footer-account') && bounds.height >= 44, 'Middle widths must put a touch-sized account control in the footer.');
        assert(Math.abs((bounds.left + bounds.right) / 2 - (footer.left + footer.right) / 2) < 1, 'Footer account control must be horizontally centered.');
        if (archive.getClientRects().length) assert(bounds.top >= archive.getBoundingClientRect().bottom, 'Footer account control must follow Recent Posts.');
        assert(bounds.top >= document.querySelector('#article-content').getBoundingClientRect().bottom, 'Footer account control must follow the page content.');
      } else {
        const header = document.querySelector('.site-header');
        const right = header.getBoundingClientRect().right - parseFloat(getComputedStyle(header).paddingRight);
        assert(visible[0].parentElement === header && Math.abs(bounds.right - right) < 1, 'Desktop sign-in must align with the upper-right header gutter.');
        const profile = header.querySelector('.profile');
        const profileText = profile.querySelector('.profile-details').getBoundingClientRect();
        const portrait = profile.querySelector('.portrait').getBoundingClientRect();
        assert(bounds.bottom <= profileText.top, 'Sign in must remain above the profile text.');
        assert(bounds.bottom <= portrait.top || bounds.left >= portrait.right, 'Sign in must not overlap the portrait.');
        const profileTop = profile.getBoundingClientRect().top;
        assert(profileTop - header.getBoundingClientRect().top === 24, 'Desktop profile must leave room for the sign-in utility.');
        assert(getComputedStyle(visible[0]).fontSize === '14px', 'Compact spacing must not shrink the sign-in text.');
        const headerHeight = header.getBoundingClientRect().height;
        visible[0].style.display = 'none';
        assert(profile.getBoundingClientRect().top === profileTop && header.getBoundingClientRect().height === headerHeight, 'Sign in must not shift the profile or increase header height.');
        visible[0].style.removeProperty('display');
      }
      if (innerWidth >= 1680) assert(bounds.right <= document.querySelector('.site-sidebar').getBoundingClientRect().right, 'Ultrawide sign-in must stay in the left column.');
    };
    const sidebar = document.querySelector('.site-sidebar');
    const archiveNav = document.querySelector(innerWidth >= 1680 ? '#sidebar-archive' : '#post-archive');
    const archive = archiveNav.querySelector('.post-archive');
    for (const link of archive.querySelectorAll('li a')) {
      const style = getComputedStyle(link);
      assert(style.display === 'block' && link.getBoundingClientRect().height >= 24, 'Recent post links need full-row targets at every width.');
      assert(style.fontSize === '16px' && style.fontWeight === '400' && Math.abs(parseFloat(style.lineHeight) - 25.6) < .1, 'Recent post links must use regular text with a consistent line height.');
      assert(style.paddingTop === '0px' && style.paddingBottom === '0px', 'Recent post links must use the same compact spacing at every width.');
      assert(style.textDecorationLine.includes('underline') && style.textDecorationColor === style.color, 'Recent post links must have clear, persistent underlines.');
      const previous = link.parentElement.previousElementSibling;
      if (previous) assert(Math.abs(link.parentElement.getBoundingClientRect().top - previous.getBoundingClientRect().bottom) < 1, 'Recent post links must flow without extra gaps between rows.');
      assert(link.getBoundingClientRect().width <= archive.getBoundingClientRect().width, 'Post links must stay within the reading list.');
    }
    const article = document.querySelector('#article-content');
    const background = getComputedStyle(body).backgroundImage;
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
    assert(initialRequests === 0, 'Recent Posts must be included in the initial HTML.');
    const unchanged = () => {
      assert(document.body === body && document.querySelector('.site-sidebar') === sidebar, 'Navigation replaced the page shell.');
      assert(archiveNav.querySelector('.post-archive') === archive, 'Navigation replaced Recent Posts.');
      assert(document.querySelector('#article-content') === article, 'Navigation replaced the article container.');
      const expectedBackground = innerWidth < 1680 && article.querySelector('.post-archive') ? 'none' : background;
      assert(getComputedStyle(body).backgroundImage === expectedBackground, 'Navigation changed the background unexpectedly.');
      assert(document.querySelector('#main-content') === main && getComputedStyle(main).backgroundImage === contentBackground, 'Navigation replaced the content background.');
      assert(archiveRequests() === initialRequests, 'Navigation fetched Recent Posts again.');
      if (innerWidth >= 1680) {
        assert(Math.abs(sidebar.getBoundingClientRect().width - sidebarWidth) < 1, 'Post length changed the sidebar width: ' + sidebarWidth + ' to ' + sidebar.getBoundingClientRect().width);
        assert(Math.abs(main.getBoundingClientRect().left - contentLeft) < 1, 'Post length shifted the content column.');
      } else {
        assert(Math.abs(article.getBoundingClientRect().width - contentWidth) < 1, 'Navigation changed content width.');
        assert(Math.abs(document.querySelector('.profile-details').getBoundingClientRect().width - bioWidth) < 1, 'Navigation changed bio width.');
        if (innerWidth < 768) assert(Math.abs(document.querySelector('.menu-toggle').getBoundingClientRect().right - menuRight) < 1, 'Navigation moved the mobile menu.');
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
    const pageLinks = innerWidth < 768 || innerWidth >= 1680 ? menu : document.querySelector('.profile-links');
    if (innerWidth >= 768) {
      const list = pageLinks.matches('ul') ? pageLinks : pageLinks.querySelector('ul');
      const rows = [...list.children];
      assert(Math.abs(rows[1].getBoundingClientRect().top - rows[0].getBoundingClientRect().bottom) < 1, 'Non-mobile page links must have the same compact row spacing.');
      for (const link of pageLinks.querySelectorAll('a')) {
        if (!link.getClientRects().length) continue;
        const style = getComputedStyle(link);
        assert(style.fontSize === '14.4px' && Math.abs(parseFloat(style.lineHeight) - 20.16) < .1 && style.paddingTop === '2px' && style.paddingBottom === '2px', 'Page links and subpages must share ultrawide sizing and spacing at every non-mobile width.');
      }
    }
    const projectLinks = document.querySelector('#site-menu .child-links');
    const projectToggle = document.querySelector('.child-toggle');
    const checkSubpageTitles = nav => {
      const long = nav.querySelector('a[href="/page/project-0"]');
      const short = nav.querySelector('a[href="/page/project-1"]');
      assert(long.getClientRects().length > 0, 'Truncation check needs a visible menu.');
      for (const link of [long, short]) {
        const style = getComputedStyle(link);
        assert(style.whiteSpace === 'nowrap' && style.textOverflow === 'ellipsis' && style.overflowX === 'hidden', 'Subpage titles must truncate rather than wrap.');
        assert(link.title === link.textContent, 'The full subpage title must remain available on hover and as link text.');
      }
      assert(long.scrollWidth > long.clientWidth, 'Long subpage fixture must exercise truncation.');
      assert(short.scrollWidth <= short.clientWidth, 'Short titles must fit without truncation.');
      assert(Math.abs(long.getBoundingClientRect().height - short.getBoundingClientRect().height) < 1, 'Long titles must not add menu rows.');
      assert(document.documentElement.scrollWidth <= Math.max(320, document.documentElement.clientWidth), 'Subpage titles must not widen the layout.');
    };
    assert(projectLinks.querySelectorAll('a').length === 6, 'Navigation must include immediate children only.');
    for (const nav of [menu, document.querySelector('.profile-links')]) {
      assert(!nav.querySelector('a[href="/page/project-guide"]') && !nav.querySelector('.child-links .child-links'), 'Navigation must stop at two levels.');
    }
    if (innerWidth < 768) {
      const toggleBox = menuToggle.getBoundingClientRect();
      assert(toggleBox.height >= 44 && toggleBox.width < 150, 'Native menu disclosure must remain a compact, touch-sized control.');
      menuToggle.click();
      checkSignIn();
      assert(!projectToggle.hidden && !projectLinks.parentElement.open, 'Mobile project tree must start collapsed.');
      projectToggle.click();
      assert(projectLinks.parentElement.open, 'Project toggle did not reveal child links.');
      checkSubpageTitles(menu);
      assert(!projectLinks.querySelector('.child-toggle'), 'Second-level links must not offer hidden descendants.');
      projectLinks.querySelector('a[href="/page/project-0"]').click();
      await wait(() => location.pathname === '/page/project-0');
      await headerVisible();
      assert(!menu.parentElement.open && !projectLinks.parentElement.open, 'Selecting a page must close the mobile branch.');
      unchanged();
    } else if (innerWidth >= 1680) {
      checkSignIn();
      assert(projectLinks.parentElement.open && projectToggle.hidden, 'Ultrawide project tree must remain expanded.');
      checkSubpageTitles(menu);
      for (const link of menu.querySelectorAll('a')) {
        if (!link.getClientRects().length) continue;
        const style = getComputedStyle(link);
        const expectedWeight = link.closest('.child-links') || link.matches('.header-sign-in') ? '400' : '600';
        assert(style.paddingTop === '2px' && style.paddingBottom === '2px' && style.fontWeight === expectedWeight, 'Ultrawide navigation must keep compact spacing with bold parents and regular-weight subpages.');
      }
      assert(projectLinks.querySelector('a').getBoundingClientRect().left > menu.querySelector('a[href="/projects"]').getBoundingClientRect().left, 'Ultrawide subpages must remain indented.');
      const bioGap = menu.getBoundingClientRect().top - document.querySelector('.profile').getBoundingClientRect().bottom;
      assert(Math.abs(bioGap - 50) < 1, 'Ultrawide navigation needs a 50px gap below the bio.');
      assert(menu.getBoundingClientRect().bottom <= archive.getBoundingClientRect().top, 'Site navigation overlaps Recent Posts.');
      const originalWidth = innerWidth;
      for (const width of [390, 1100, originalWidth]) {
        window.frameElement.style.width = width + 'px';
        await wait(() => innerWidth === width && menu.parentElement.open === (width >= 768) && projectLinks.parentElement.open === (width >= 768));
        assert(projectToggle.hidden === (width >= 768), 'Resizing left the project toggle in the wrong state.');
        assert((document.querySelector('.profile-links').getClientRects().length > 0) === (width >= 768 && width < 1680), 'Resizing left duplicate or missing compact navigation.');
      }
    } else {
      checkSignIn();
      assert(menu.getClientRects().length === 0, 'Middle widths must hide the separate menu.');
      checkSubpageTitles(pageLinks);
      const children = pageLinks.querySelector('.child-links');
      assert(children.getClientRects().length > 0 && children.querySelectorAll('a').length === 6, 'Middle navigation must show immediate children beneath Projects.');
      assert(children.getBoundingClientRect().top >= pageLinks.querySelector('a[href="/projects"]').getBoundingClientRect().bottom, 'Child links must sit below their parent link.');
      const profile = document.querySelector('.profile').getBoundingClientRect();
      const navigation = pageLinks.getBoundingClientRect();
      assert(navigation.left >= profile.right + 31, 'Middle navigation must sit to the right of the profile with a clear gap.');
      assert(getComputedStyle(pageLinks).textAlign === 'right', 'Middle navigation must align its text to the right.');
      assert(Math.abs((navigation.top + navigation.bottom) / 2 - (profile.top + profile.bottom) / 2) < 1, 'Middle navigation must be vertically centered beside the profile.');
      assert(document.documentElement.scrollWidth <= document.documentElement.clientWidth, 'Header navigation must fit the viewport.');
      for (const link of pageLinks.querySelectorAll(':scope > ul > li > a')) {
        assert(getComputedStyle(link).fontWeight === '600', 'Top-level navigation links must stay bold.');
      }
      for (const link of children.querySelectorAll('a')) {
        assert(getComputedStyle(link).fontWeight === '400', 'Middle subpage links must use lighter text than their parent.');
      }
      children.querySelector('a[href="/page/project-0"]').click();
      await wait(() => location.pathname === '/page/project-0');
      await headerVisible();
      unchanged();
    }
    if (innerWidth < 768) menuToggle.click();
    pageLinks.querySelector('a[href="/about"]').click();
    await wait(() => location.pathname === '/about' && document.title === 'Trent Nix | About Me');
    await headerVisible();
    assert(article.querySelector('h1')?.textContent === 'About Me', 'About navigation missed the article.');
    assertHeading();
    archiveVisible(innerWidth >= 1680);
    if (innerWidth >= 768 && innerWidth < 1680) checkSignIn();
    const aboutProfileLeft = document.querySelector('.profile-name').getBoundingClientRect().left;
    const aboutTitleLeft = article.querySelector('h1').getBoundingClientRect().left;
    if (innerWidth < 768) {
      assert(!menu.parentElement.open, 'Navigation must close the mobile menu.');
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
    if (innerWidth < 1680) assert(getComputedStyle(body).backgroundImage === 'none', 'The full archive must keep its links clear of the illustration.');
    archiveVisible(innerWidth >= 1680);
    unchanged();
    article.querySelector('a[href="/post/s/second"]').click();
    await wait(() => postVisible('second'));
    await headerVisible();
    assert(getComputedStyle(body).backgroundImage === background, 'Returning to a post must restore its illustration.');
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
