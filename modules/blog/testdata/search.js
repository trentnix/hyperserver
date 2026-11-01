// Reader search keeps the shell, supports ordinary links, and restores browser history.
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
    const headMetadata = () => Object.fromEntries([...document.head.querySelectorAll('[data-blog-metadata]')].map(node => [node.getAttribute('name') || node.getAttribute('property') || 'canonical', node.getAttribute('content') ?? node.getAttribute('href')]));
    const checkMetadata = async () => {
      const response = await fetch(location.pathname + location.search);
      const full = new DOMParser().parseFromString(await response.text(), 'text/html');
      const expected = Object.fromEntries([...full.head.querySelectorAll('[data-blog-metadata]')].map(node => [node.getAttribute('name') || node.getAttribute('property') || 'canonical', node.getAttribute('content') ?? node.getAttribute('href')]));
      assert(document.head.querySelectorAll('[data-blog-metadata]').length === Object.keys(expected).length, 'Navigation duplicated page metadata.');
      assert(JSON.stringify(headMetadata()) === JSON.stringify(expected), 'Partial navigation left stale or duplicate page metadata.');
    };
    const shell = ['body', '.site-sidebar', '#post-archive', '#sidebar-archive'].map(selector => [selector, document.querySelector(selector)]);
    assert(!sessionStorage.getItem('reader-search-started'), 'Search reloaded the document.');
    sessionStorage.setItem('reader-search-started', 'true');
    const ready = async query => {
      await wait(() => location.pathname === '/search' && new URLSearchParams(location.search).get('q') === query && article.querySelector('input[name=q]')?.value === (query || '') && !article.classList.contains('htmx-settling'));
      for (const [selector, node] of shell) assert(document.querySelector(selector) === node, 'Search replaced ' + selector);
      const input = article.querySelector('input');
      const button = article.querySelector('button[type=submit]');
      assert(Math.abs(input.getBoundingClientRect().height - button.getBoundingClientRect().height) < 1, 'Search controls have different heights.');
      assert(button.getBoundingClientRect().height >= 44, 'Search button is too small for touch.');
      assert(document.documentElement.scrollWidth <= Math.max(320, document.documentElement.clientWidth), 'Search overflows the viewport.');
      assert([...document.querySelectorAll('form[role=search]')].filter(form => form.getClientRects().length).length === 1, 'The results page must not show a duplicate search form.');
      assert(!document.head.querySelector('[rel=canonical], [property^="og:"]'), 'Search retained the previous page canonical or sharing tags.');
    };
    assert(!document.querySelector('.site-header a[href="/search"]'), 'Search must not appear in the header.');
    const archiveForm = article.querySelector('form[role=search]');
    assert(archiveForm?.action === location.origin + '/search', 'All Posts needs a reader search form.');
    article.querySelector('input').value = 'needle';
    article.querySelector('input').focus();
    article.querySelector('input').setSelectionRange(1, 4);
    archiveForm.requestSubmit();
    await ready('needle');
    assert(document.activeElement === article.querySelector('input') && document.activeElement.selectionStart === 1 && document.activeElement.selectionEnd === 4, 'Search must preserve keyboard focus and selection.');
    await checkMetadata();
    assert(article.querySelector('[role=status]').textContent === '3 results found.', 'Incorrect public result count.');
    assert([...article.querySelectorAll('.search-results h2')].map(node => node.textContent).join('|') === 'Needle post|Summary match|Body match', 'Incorrect result ranking or visibility.');
    const fallback = await fetch('/search?q=needle');
    const html = await fallback.text();
    assert(fallback.ok && html.includes('<!DOCTYPE') && html.includes('3 results found.'), 'Ordinary search did not return a full page.');
    assert(fallback.headers.get('X-Robots-Tag') === 'noindex, follow', 'Search must not be indexed.');
    article.querySelector('.management-search-results a').click();
    await ready(null);
    history.back();
    await ready('needle');
    await checkMetadata();
    history.forward();
    await ready(null);
    article.querySelector('input').value = 'absent';
    article.querySelector('button[type=submit]').click();
    await ready('absent');
    assert(document.activeElement === article.querySelector('input'), 'Submitting from the button must return focus to the search field.');
    assert(article.textContent.includes('No matches.'), 'Missing empty-state guidance.');
    article.querySelector('input').value = 'needle';
    article.querySelector('button[type=submit]').click();
    await ready('needle');
    article.querySelector('.search-results a').click();
    await wait(() => location.pathname === '/post/s/needle-post' && article.querySelector('h1')?.textContent === 'Needle post' && !article.classList.contains('htmx-settling'));
    assert(document.activeElement === article.querySelector('h1'), 'Opening a search result must focus its heading without scrolling separately.');
    await checkMetadata();
    history.back();
    await ready('needle');
    history.forward();
    await wait(() => location.pathname === '/post/s/needle-post' && article.querySelector('h1')?.textContent === 'Needle post' && !article.classList.contains('htmx-settling'));
    await checkMetadata();
    for (const [selector, node] of shell) assert(document.querySelector(selector) === node, 'Opening a result replaced ' + selector);
    const placement = innerWidth >= 1680 ? '.content-search' : '#post-archive';
    const searchForm = document.querySelector(placement + ' form[role=search]');
    assert(searchForm?.getClientRects().length, 'Search form is missing from its responsive placement.');
    if (innerWidth >= 1680) {
      assert(!document.querySelector('#sidebar-archive form'), 'Ultrawide search must not appear in the sidebar.');
      assert(searchForm.getBoundingClientRect().top >= article.getBoundingClientRect().bottom, 'Ultrawide search must follow the page content.');
      assert(Math.abs(searchForm.getBoundingClientRect().left - article.getBoundingClientRect().left - parseFloat(getComputedStyle(article).paddingLeft)) < 1, 'Ultrawide search must align with the content text.');
      window.scrollTo(0, 0);
      const slot = searchForm.closest('.content-search');
      assert(Math.abs(slot.getBoundingClientRect().bottom - innerHeight) < 2, 'Search must sit at the bottom of a short ultrawide page.');
      article.style.minHeight = '150vh';
      assert(slot.getBoundingClientRect().top >= article.getBoundingClientRect().bottom, 'Search must follow long content without covering it.');
      article.style.removeProperty('min-height');
    } else {
      const recentLinks = document.querySelector(placement + ' .post-archive > ul');
      assert(searchForm.getBoundingClientRect().top >= recentLinks.getBoundingClientRect().bottom, 'Footer search must follow Recent Posts.');
    }
    const field = searchForm.querySelector('input');
    const submit = searchForm.querySelector('button');
    assert(field.labels.length === 1 && field.labels[0].textContent.trim() === 'Search Site', 'Search field needs a label that identifies its site-wide scope.');
    const inputBox = field.getBoundingClientRect(), buttonBox = submit.getBoundingClientRect();
    assert(Math.abs(inputBox.top - buttonBox.top) < 1 && Math.abs(inputBox.height - buttonBox.height) < 1, 'Archive search controls must align in one row.');
    assert(buttonBox.right <= searchForm.getBoundingClientRect().right + 1 && buttonBox.height >= 44, 'Archive search controls overflow or are too small.');
    field.value = 'needle';
    submit.scrollIntoView();
    submit.click();
    await ready('needle');
    assert(document.activeElement === article.querySelector('input'), 'Footer/content search must move focus into the results form.');
    await wait(() => window.scrollY === 0);
    assert(article.querySelector('[role=status]').textContent === '3 results found.', 'Archive search did not submit its query.');
    report('PASS');
  } catch (error) {
    report(error.message + '\n' + (error.stack || ''));
  }
})();
