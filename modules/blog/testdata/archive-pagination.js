// Archive paging replaces only the article and supports history and ordinary links.
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
    const article = document.querySelector('#article-content');
    const shell = ['body', '.site-sidebar', '.account-panel', '#post-archive', '#sidebar-archive'].map(selector => [selector, document.querySelector(selector)]);
    const pageSize = __ARCHIVE_PAGE_SIZE__;
    const ready = async page => {
      await wait(() => location.search === (page === 1 ? '' : '?page=' + page) && article.querySelector('.archive-page-number')?.textContent === 'Page ' + page && !article.classList.contains('htmx-settling'));
      assert(document.title === 'Trent Nix | All Posts' + (page === 1 ? '' : ' — Page ' + page), 'Archive title lost its page number.');
      for (const [selector, node] of shell) assert(document.querySelector(selector) === node, 'Pagination replaced ' + selector);
      const links = article.querySelectorAll('li > a');
      assert(links.length === (page < 3 ? pageSize : 1), 'Incorrect number of archive entries.');
      const controls = article.querySelectorAll('.archive-pagination');
      assert(controls.length === 1, 'Archive needs just one navigation row.');
      assert(article.querySelector('.post-archive').lastElementChild === controls[0], 'Pagination must follow the archive list.');
      for (const nav of controls) {
        assert(!!nav.querySelector('[rel=prev]') === (page > 1) && !!nav.querySelector('[rel=next]') === (page < 3), 'Incorrect adjacent navigation links.');
        const bounds = nav.getBoundingClientRect();
        const current = nav.querySelector('.archive-page-number').getBoundingClientRect();
        assert(Math.abs((current.left + current.right) / 2 - (bounds.left + bounds.right) / 2) < 1, 'Page number must remain centered at archive boundaries.');
        for (const link of nav.querySelectorAll('a')) {
          const rect = link.getBoundingClientRect();
          assert(rect.height >= 44, 'Pagination links must be touch-sized.');
          assert(rect.left >= bounds.left - 1 && rect.right <= bounds.right + 1, 'Pagination link exceeds its row.');
          assert(link.rel === 'prev' ? rect.right <= current.left - 7 : rect.left >= current.right + 7, 'Pagination link overlaps the page number.');
          assert(link.textContent.includes(link.rel === 'prev' ? 'Newer posts' : 'Older posts'), 'Pagination direction must describe post chronology.');
        }
      }
      assert(document.documentElement.scrollWidth <= Math.max(320, document.documentElement.clientWidth), 'Archive overflows the viewport.');
    };
    assert(!sessionStorage.getItem('archive-pagination-started'), 'Archive paging reloaded the document.');
    sessionStorage.setItem('archive-pagination-started', 'true');
    await ready(1);
    const next = article.querySelector('[rel=next]');
    next.scrollIntoView();
    next.click();
    await ready(2);
    const fallback = await fetch(location.href);
    const html = await fallback.text();
    assert(fallback.ok && html.includes('href="https://blog.example/posts?page=2"') && html.includes('Archive &lt;' + String(pageSize).padStart(3, '0') + '&gt;'), 'Ordinary navigation must render page two with its canonical URL.');
    article.querySelector('[rel=next]').click();
    await ready(3);
    article.querySelector('[rel=prev]').click();
    await ready(2);
    article.querySelector('[rel=prev]').click();
    await ready(1);
    history.back();
    await ready(2);
    history.forward();
    await ready(1);
    assert(!performance.getEntriesByType('resource').some(entry => new URL(entry.name).pathname === '/posts/recent'), 'Pagination fetched Recent Posts again.');
    report('PASS');
  } catch (error) {
    report(error.message + '\n' + (error.stack || ''));
  }
})();
