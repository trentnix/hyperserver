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
  const panel = () => document.querySelector('[data-series-panel]');
  const postForm = () => document.querySelector('#post-form');
  try {
    if (sessionStorage.getItem('series-stage') === 'joined') {
      await wait(() => document.querySelector('.series-navigation') && document.querySelector('.post-heading h1')?.textContent === 'New series member');
      assert(document.querySelector('.series-navigation').textContent.includes('Part 4 of 4'), 'A new post must join the selected series with its first save.');
      sessionStorage.removeItem('series-stage');
      await report('PASS');
      return;
    }
    if (sessionStorage.getItem('series-stage') === 'view') {
      await wait(() => document.querySelector('.series-navigation') && document.querySelector('.post-heading h1')?.textContent === 'Unsaved post title');
      assert(document.querySelector('.series-navigation').textContent.includes('Recovered series name'), 'Saving a post must preserve its separate series membership.');
      const navigation = document.querySelector('.series-navigation');
      assert(navigation.querySelector('details').open, 'The series list must start expanded.');
      if (innerWidth < 768) assert(navigation.querySelector('.series-neighbors > div').getBoundingClientRect().height === 0, 'A missing previous post must not leave an empty mobile row.');
      const response = await fetch(navigation.querySelector('[rel="next"]').href);
      const middle = new DOMParser().parseFromString(await response.text(), 'text/html').querySelector('.series-navigation');
      navigation.replaceWith(middle);
      const previous = middle.querySelector('[rel="prev"]').getBoundingClientRect();
      const next = middle.querySelector('[rel="next"]').getBoundingClientRect();
      assert(innerWidth < 768 ? next.top >= previous.bottom + 16 : Math.abs(previous.top - next.top) < 1, 'Series neighbors must stack with spacing on mobile and share a row on wider screens.');
      assert(document.documentElement.scrollWidth <= innerWidth + 1, 'Series navigation must fit the viewport.');
      middle.querySelector('summary').click();
      assert(!middle.querySelector('details').open, 'Readers must still be able to collapse the series list.');
      sessionStorage.removeItem('series-stage');
      await htmx.ajax('GET', '/post/create', {target: '#article-content', swap: 'innerHTML'});
      await wait(() => postForm()?.querySelector('.CodeMirror') && panel()?.querySelector('[data-series-find]'));
      const post = postForm();
      set(post.elements.title, 'New series member');
      post.querySelector('.CodeMirror').CodeMirror.setValue('A new post joins the shared series in one save.');
      post.querySelector('[name="published"][value="on"]').checked = true;
      post.querySelector('[name="published"][value="on"]').dispatchEvent(new Event('change', {bubbles: true}));
      panel().querySelector('details').open = true;
      panel().querySelector('[data-series-find]').click();
      const find = panel().querySelector('[data-series-search]');
      const matches = panel().querySelector('[data-series-search-results]');
      assert(matches.hidden && !matches.children.length, 'Finding a series must not preload all series.');
      set(find, 'Unsaved post title');
      await wait(() => !matches.hidden && matches.children.length === 1);
      assert(matches.textContent.includes('Recovered series name') && matches.textContent.includes('Unsaved post title') && matches.textContent.includes('4 posts'), 'A member post search must identify its series and show matching post context.');
      find.dispatchEvent(new KeyboardEvent('keydown', {key: 'ArrowDown', bubbles: true, cancelable: true}));
      assert(document.activeElement === matches.querySelector('button'), 'Search results must be reachable from the keyboard.');
      matches.querySelector('button').click();
      await wait(() => post.elements.seriesID.value && !panel().querySelector('[data-post-picker-results]').hasAttribute('aria-busy'));
      assert(JSON.parse(post.elements.series.value).posts.length === 4 && !panel().querySelector('[data-series-unsaved]').hidden, 'Selecting an existing series must stage its full saved order.');
      sessionStorage.setItem('series-stage', 'joined');
      post.requestSubmit();
      return;
    }
    if (sessionStorage.getItem('series-stage') === 'recover') {
      await wait(() => postForm()?.querySelector('[data-draft-restore]') && !postForm().querySelector('[data-draft-recovery]').hidden);
      postForm().querySelector('[data-draft-restore]').click();
      await wait(() => panel().querySelector('[data-post-picker-title]').value === 'Recovered series name');
      assert(postForm().elements.title.value === 'Unsaved post title' && JSON.parse(postForm().elements.series.value).posts.length === 4, 'One recovery copy must restore both post and series edits.');
      panel().querySelector('details').open = true;
      assert(!panel().querySelector('[data-series-unsaved]').hidden && postForm().querySelectorAll('[data-draft-recovery]').length === 1, 'Series edits must share the post recovery and show their unsaved status.');
      sessionStorage.setItem('series-stage', 'view');
      postForm().requestSubmit();
      return;
    }
    await htmx.ajax('GET', '/post/update/SOURCE_ID', {target: '#article-content', swap: 'innerHTML'});
    history.replaceState(null, '', '/post/update/SOURCE_ID');
    await wait(() => document.querySelector('#post-form .CodeMirror') && panel());
    const post = document.querySelector('#post-form');
    set(post.elements.title, 'Unsaved post title');
    panel().querySelector('details').open = true;
    assert(!panel().querySelector('select') && !panel().querySelector('[data-series-actions]').hidden, 'Unassigned posts must offer explicit actions instead of a dropdown.');
    panel().querySelector('[data-series-find]').click();
    const find = panel().querySelector('[data-series-search]');
    assert(document.activeElement === find && panel().querySelector('[data-series-search-results]').hidden, 'Find a series must focus an empty search without preloaded results.');
    assert(panel().querySelector('[data-series-search-cancel]').getBoundingClientRect().height >= 44, 'Search cancellation must remain easy to tap.');
    find.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true}));
    assert(!panel().querySelector('[data-series-actions]').hidden && post.elements.title.value === 'Unsaved post title', 'Canceling search must leave the post unassigned and preserve its writing.');
    panel().querySelector('[data-series-create]').click();
    assert(document.activeElement === panel().querySelector('[data-post-picker-title]'), 'Creating a series must focus its name field.');
    panel().querySelector('[data-series-cancel]').click();
    assert(post.elements.seriesID.value === '' && post.elements.title.value === 'Unsaved post title', 'Canceling a new series must preserve post edits.');
    panel().querySelector('[data-series-create]').click();
    await wait(() => postForm()?.elements.series.hidden && !panel().querySelector('[data-post-picker-results]').hasAttribute('aria-busy'));
    assert(!document.querySelector('dialog[open]') && document.querySelector('#post-form') === post && post.elements.title.value === 'Unsaved post title', 'Opening series settings must preserve the mounted unsaved post.');
    assert(JSON.parse(postForm().elements.series.value).posts[0] === 'SOURCE_ID', 'A new series must include its source post.');
    set(panel().querySelector('[data-post-picker-title]'), 'An ordered guide');
    const search = panel().querySelector('[data-post-picker-search]');
    search.focus();
    set(search, 'Series target');
    const results = panel().querySelector('[data-post-picker-results]');
    await wait(() => !results.hidden && results.children.length === 3);
    for (const option of [...results.children]) option.click();
    assert(JSON.parse(postForm().elements.series.value).posts.length === 4 && !results.hidden, 'The shared picker must add multiple series members without dismissing results.');
    const rows = [...panel().querySelectorAll('[data-post-picker-id]')];
    rows[2].querySelector('button').click();
    assert(JSON.parse(postForm().elements.series.value).posts[1] === rows[2].dataset.postPickerId, 'Movement must change the shared reading order.');
    assert(document.documentElement.scrollWidth <= innerWidth + 1, 'The series editor must fit a narrow viewport.');
    assert(panel().closest('form') === post && panel().compareDocumentPosition(post.querySelector('.publication-footer .form-actions')) & Node.DOCUMENT_POSITION_FOLLOWING, 'Series must live inside the post form before its save actions.');
    assert(document.querySelectorAll('form[data-draft-owner]').length === 1 && !panel().querySelector('button[type="submit"]'), 'There must be one save mechanism and one recovery owner.');
    const response = await fetch('/post/id/SOURCE_ID');
    const reader = new DOMParser().parseFromString(await response.text(), 'text/html');
    assert(!reader.querySelector('.series-navigation') && reader.querySelector('.post-heading h1').textContent === 'Series starting point', 'Post and series edits must stay unsaved until the post save.');
    set(panel().querySelector('[data-post-picker-title]'), 'Recovered series name');
    await wait(() => Object.keys(localStorage).some(key => key.includes('/post/SOURCE_ID:') && localStorage.getItem(key).includes('Recovered series name')));
    sessionStorage.setItem('series-stage', 'recover');
    location.reload();
  } catch (error) {
    await report(error.message + '\n' + error.stack + '\n' + JSON.stringify({panel: panel()?.textContent, focus: document.activeElement?.id}));
  }
})();
