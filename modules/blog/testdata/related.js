(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate);
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  const set = (input, value) => { input.value = value; input.dispatchEvent(new Event('input', {bubbles: true})); };
  try {
    const stage = sessionStorage.getItem('related-stage');
    if (stage === 'view') {
      await wait(() => document.querySelector('.post-body .related-posts'));
      const navigation = document.querySelector('.post-body .related-posts');
      assert(navigation.querySelector('h3').textContent === 'Keep reading' && navigation.querySelectorAll('li').length === 2 && !navigation.textContent.includes('Private draft'), 'Readers must see the heading and public targets only.');
      const edit = document.querySelector('a[href^="/post/update/"]');
      await htmx.ajax('GET', edit.getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
      await wait(() => document.querySelector('[data-post-picker-selections]')?.children.length === 3 && !document.querySelector('[data-post-picker-results]').hasAttribute('aria-busy'));
      const restored = document.querySelector('[data-related-editor]');
      assert(restored.querySelector('[data-component-unsaved]').hidden, 'Reopened component must match its saved baseline.');
      assert(document.documentElement.scrollWidth <= innerWidth + 1, 'The saved editor must fit narrow viewports.');
      sessionStorage.removeItem('related-stage');
      await report('PASS');
      return;
    }
    if (stage === 'recover') {
      await wait(() => document.querySelector('[data-draft-recovery]') && !document.querySelector('[data-draft-recovery]').hidden);
      document.querySelector('[data-draft-restore]').click();
      await wait(() => document.querySelector('[data-post-picker-selections]')?.children.length === 3 && !document.querySelector('[data-post-picker-results]').hasAttribute('aria-busy'));
      assert(document.querySelector('[data-post-picker-title]').value === 'Keep reading', 'Draft recovery must preserve the heading and selections.');
      sessionStorage.setItem('related-stage', 'view');
      document.querySelector('#post-form').requestSubmit();
      return;
    }
    await wait(() => document.querySelector('#post-form .CodeMirror') && document.querySelector('[name="related"]').hidden);
    const form = document.querySelector('#post-form');
    form.querySelector('[data-component-add="related-posts"]').click();
    const section = form.querySelector('[data-related-editor]');
    const source = form.elements.related;
    const results = section.querySelector('[data-post-picker-results]');
    const rows = () => [...section.querySelector('[data-post-picker-selections]').children];
    const search = section.querySelector('[data-post-picker-search]');
    const status = section.querySelector('[data-post-picker-status]');
    assert(results.hidden && results.children.length === 0 && search.getAttribute('aria-expanded') === 'false', 'Adding the component must not preload a list of post choices.');
    assert(section.querySelector('.post-picker-selected-panel').hidden, 'An empty component must not show an empty selected-post panel.');
    assert(!section.querySelector('[data-component-unsaved]').hidden, 'A new component must show unsaved changes.');
    form.noValidate = true;
    set(form.elements.title, 'Related browser review');
    set(form.elements.slug, 'related-browser-review');
    form.requestSubmit();
    await wait(() => source.getAttribute('aria-invalid') === 'true');
    assert(section.contains(document.activeElement), 'An empty selection must focus its component.');
    for (const title of ['A long target', 'Second target', 'Private draft target']) {
      search.focus();
      const top = section.querySelector('.post-picker-selected-panel').getBoundingClientRect().top;
      set(search, title);
      await wait(() => !results.hidden && [...results.children].some(button => button.textContent.startsWith(title)));
      if (rows().length) assert(section.querySelector('.post-picker-selected-panel').getBoundingClientRect().top === top, 'Search suggestions must overlay the selected list without moving it.');
      assert(search.getAttribute('aria-expanded') === 'true', 'Search must announce its open suggestions.');
      search.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
      assert(results.hidden && search.getAttribute('aria-expanded') === 'false', 'Escape must dismiss suggestions.');
      set(search, title);
      await wait(() => !results.hidden && results.children.length > 0);
      search.dispatchEvent(new KeyboardEvent('keydown', {key: 'ArrowDown', bubbles: true}));
      assert(search.getAttribute('aria-activedescendant') === results.firstElementChild.id, 'Arrow keys must identify the active suggestion.');
      search.dispatchEvent(new KeyboardEvent('keydown', {key: 'Enter', bubbles: true}));
      await wait(() => rows().some(row => row.textContent.includes(title)) && !results.hasAttribute('aria-busy'));
      assert(!results.hidden && search.value === title && search.getAttribute('aria-expanded') === 'true', 'Adding a post must preserve the query and keep suggestions open.');
      assert(results.firstElementChild.getAttribute('aria-selected') === 'true' && results.firstElementChild.querySelector('small').textContent === 'Added', 'Added posts must be marked in the results.');
    }
    assert(JSON.parse(source.value).posts.length === 3 && source.getAttribute('aria-invalid') !== 'true', 'Selection must update source and clear its error.');
    assert(rows()[2].querySelector('small').textContent === 'Draft', 'Draft targets need a concise publication hint.');
    assert(section.querySelector('[data-post-picker-count]').textContent === '3' && !section.querySelector('.post-picker-selected-panel').hidden, 'The selected-post heading must show the current count.');
    for (const row of rows()) {
      const stacked = row.querySelector('.post-picker-selection-actions').getBoundingClientRect().top >= row.querySelector('.post-picker-selection-details').getBoundingClientRect().bottom;
      assert(row.getBoundingClientRect().height <= (stacked ? 112 : 76), 'Selected links must remain compact while giving narrow-screen titles their own line.');
      assert(row.querySelector('.post-picker-selection-actions').getBoundingClientRect().height === 44, 'Movement and removal controls must stay together on one line.');
      const title = row.querySelector('strong');
      assert(title.title === title.textContent && title.getBoundingClientRect().height <= parseFloat(getComputedStyle(title).lineHeight) * 2 + 1, 'Long titles must stop after two lines while retaining their full text.');
      assert(title.getBoundingClientRect().width >= 150, 'Titles need enough room to read beside movement and removal controls.');
    }
    assert(section.querySelector('.post-picker-search-panel').getBoundingClientRect().bottom <= section.querySelector('.post-picker-selected-panel').getBoundingClientRect().top, 'Search and selected posts must form one column at every width.');
    const original = JSON.parse(source.value).posts;
    rows()[0].querySelectorAll('button')[1].click();
    assert(JSON.parse(source.value).posts[1] === original[0] && document.activeElement.closest('[data-post-picker-id]').dataset.postPickerId === original[0], 'Movement must preserve focus and author order.');
    set(section.querySelector('[data-post-picker-title]'), 'Keep reading');
    search.focus();
    set(search, 'no such title');
    await wait(() => status.textContent === 'No matching posts.');
    assert(rows().length === 3, 'Searching must preserve selected posts.');
    const savedSource = source.value;
    section.querySelector('[data-component-remove]').click();
    assert(section.hidden && !source.value, 'Removal must clear component source.');
    form.querySelector('[data-component-undo="related-posts"]').click();
    await wait(() => rows().length === 3 && !results.hasAttribute('aria-busy'));
    assert(!section.hidden && source.value === savedSource, 'Undo must restore every selected ID and the heading.');
    const nativeFetch = window.fetch;
    window.fetch = (url, options) => String(url).startsWith('/post/related?') ? new Promise((resolve, reject) => {
      const timer = setTimeout(() => nativeFetch(url, options).then(resolve, reject), 300);
      options.signal.addEventListener('abort', () => {
        clearTimeout(timer);
        reject(new DOMException('Aborted', 'AbortError'));
      }, {once: true});
    }) : nativeFetch(url, options);
    source.dispatchEvent(new Event('change', {bubbles: true}));
    assert(rows().every(row => row.querySelector('strong').textContent === 'Selected post'), 'The delayed lookup must leave metadata pending for this check.');
    set(search, 'Second target');
    set(search, '');
    await wait(() => rows().every(row => row.querySelector('strong').textContent !== 'Selected post') && !results.hasAttribute('aria-busy'));
    assert(source.value === savedSource && results.hidden, 'Clearing an interrupted search must resolve selected titles without changing selections or showing suggestions.');
    window.fetch = nativeFetch;
    search.focus();
    set(search, 'Capacity');
    await wait(() => !results.hidden && results.children.length === 9 && !results.hasAttribute('aria-busy'));
    const choices = [...results.children];
    const positions = choices.map(choice => choice.offsetTop);
    let selectionRequests = 0;
    window.fetch = (url, options) => {
      if (String(url).startsWith('/post/related?')) selectionRequests++;
      return nativeFetch(url, options);
    };
    for (const choice of choices.slice(0, 3)) {
      choice.focus();
      choice.click();
      assert(!results.hidden && search.value === 'Capacity' && document.activeElement === search, 'Clicking results must keep the query, suggestions, and keyboard focus ready for another selection.');
      assert(choices.every((choice, index) => results.children[index] === choice && choice.offsetTop === positions[index]), 'Adding posts must preserve every result row and its position.');
      assert(choice.getAttribute('aria-selected') === 'true' && choice.getAttribute('aria-disabled') === 'true' && choice.querySelector('small').textContent === 'Added', 'An added result must show its selected state and prevent duplicate selection.');
    }
    choices[0].focus();
    choices[0].click();
    search.dispatchEvent(new KeyboardEvent('keydown', {key: 'Enter', bubbles: true}));
    assert(rows().length === 6 && document.activeElement === search, 'Repeated clicks or Enter on added results must preserve search focus without duplicating a post.');
    search.dispatchEvent(new KeyboardEvent('keydown', {key: 'ArrowDown', bubbles: true}));
    assert(search.getAttribute('aria-activedescendant') === choices[3].id, 'Keyboard navigation must skip results already added.');
    search.dispatchEvent(new KeyboardEvent('keydown', {key: 'Enter', bubbles: true}));
    assert(rows().length === 7 && !results.hidden, 'Enter must add the next available result without closing the list.');
    results.scrollTop = results.scrollHeight;
    const scroll = results.scrollTop;
    choices[8].focus({preventScroll: true});
    choices[8].click();
    assert(results.scrollTop === scroll, 'Adding a visible result must preserve the result list scroll position.');
    assert(selectionRequests === 0, 'Adding results must not reload or reorder the search response.');
    window.fetch = nativeFetch;
    for (const choice of choices.slice(4, 8)) choice.click();
    const limit = section.querySelector('[data-post-picker-limit]');
    assert(rows().length === 12 && search.disabled && results.hidden && !limit.hidden && limit.textContent.includes('Remove one') && search.getAttribute('aria-describedby').includes(limit.id), 'The selection limit must close suggestions and explain the disabled search immediately.');
    section.querySelector('[data-post-picker-title]').dispatchEvent(new PointerEvent('pointerdown', {bubbles: true}));
    section.querySelector('[data-post-picker-title]').focus();
    await wait(() => !results.hasAttribute('aria-busy'));
    assert(!limit.hidden && search.disabled, 'Dismissing suggestions or leaving search must preserve the selection-limit explanation.');
    rows()[0].querySelector('button:last-child').click();
    assert(!search.disabled && limit.hidden && !search.getAttribute('aria-describedby').includes(limit.id), 'Removing a selection must reenable search and remove the limit explanation from its accessible description.');
    source.value = savedSource;
    source.dispatchEvent(new Event('change', {bubbles: true}));
    await wait(() => !results.hasAttribute('aria-busy'));
    window.fetch = (url, options) => String(url).startsWith('/post/related?') ? Promise.resolve(new Response('Unavailable', {status: 503})) : nativeFetch(url, options);
    set(search, 'failure');
    await wait(() => status.textContent.includes('Unable to load posts.'));
    assert(source.value === savedSource && rows().length === 3, 'A failed search must preserve source and selections.');
    window.fetch = nativeFetch;
    status.querySelector('button').click();
    await wait(() => status.textContent === 'No matching posts.');
    source.value = '{"posts":[false]}';
    source.dispatchEvent(new Event('change', {bubbles: true}));
    assert(!source.hidden && section.querySelector('[data-post-picker-controls]').disabled, 'Invalid source must remain repairable without enhancement.');
    source.value = savedSource;
    source.dispatchEvent(new Event('change', {bubbles: true}));
    await wait(() => source.hidden && !results.hasAttribute('aria-busy'));
    for (const button of section.querySelectorAll('.post-picker-selection-actions button')) {
      const box = button.getBoundingClientRect();
      assert(box.width >= 44 && box.height >= 44, 'Selected-post controls must provide touch targets.');
    }
    assert(document.documentElement.scrollWidth <= innerWidth + 1, 'Long titles must fit the viewport.');
    form.querySelector('[name="published"][value="on"]').checked = true;
    form.dispatchEvent(new Event('input', {bubbles: true}));
    await wait(() => Object.keys(localStorage).some(key => key.startsWith('hyperserver.blog.draft.v1:') && localStorage.getItem(key).includes('Keep reading')));
    sessionStorage.setItem('related-stage', 'recover');
    location.reload();
  } catch (error) {
    const search = document.querySelector('[data-post-picker-search]');
    const results = document.querySelector('[data-post-picker-results]');
    await report(error.message + '\n' + error.stack + '\n' + JSON.stringify({focus: document.activeElement?.id, query: search?.value, expanded: search?.getAttribute('aria-expanded'), hidden: results?.hidden, choices: results?.textContent, status: document.querySelector('[data-post-picker-status]')?.textContent}));
  }
})();
