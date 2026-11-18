(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const nativeFetch = window.fetch;
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate);
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  const report = message => nativeFetch('/test/result', {method: 'POST', body: message});
  const assertStatus = (panel, failed) => {
    const status = panel.querySelector('[data-series-status]');
    const style = getComputedStyle(status);
    assert(!status.hidden && status.getAttribute('role') === (failed ? 'alert' : 'status'), 'Series feedback must announce errors and ordinary status appropriately.');
    assert(style.borderLeftWidth === (failed ? '4px' : '0px'), 'Loading and success messages must use neutral styling. Failures must retain the error panel.');
  };
  try {
    await htmx.ajax('GET', '/post/update/SOURCE_ID', {target: '#article-content', swap: 'innerHTML'});
    await wait(() => document.querySelector('#post-form .CodeMirror') && document.querySelector('[data-series-panel]'));
    let form = document.querySelector('#post-form');
    let panel = document.querySelector('[data-series-panel]');
    panel.querySelector('details').open = true;
    assert(!panel.querySelector('[data-series-reload], [data-series-reload-help]'), 'The series section must not duplicate the error recovery action.');
    assert(!form.querySelector('[data-series-reload-summary]'), 'An ordinary editor must not show a recovery action in its error summary.');
    if ('TEST_MODE' === 'pending') {
      panel.querySelector('[data-series-find]').click();
      const search = panel.querySelector('[data-series-search]');
      search.value = 'Review shared';
      search.dispatchEvent(new Event('input', {bubbles: true}));
      await wait(() => panel.querySelector('[data-series-search-results] button'));
      let release, reject, posts = 0;
      window.fetch = (url, options) => String(url).includes('?collection=') ? new Promise((resolve, fail) => {
        release = () => resolve(nativeFetch(url, options));
        reject = () => fail(new Error('Unavailable'));
        options.signal.addEventListener('abort', () => fail(new DOMException('Aborted', 'AbortError')), {once: true});
      }) : nativeFetch(url, options);
      const intercept = event => {
        if (event.detail.ctx.sourceElement === form) event.detail.ctx.fetch = () => {
          posts++;
          return Promise.resolve(new Response('Test validation failure', {status: 422}));
        };
      };
      document.addEventListener('htmx:before:request', intercept);
      const choose = () => panel.querySelector('[data-series-search-results] button').click();
      const blocked = async () => {
        const before = posts;
        form.requestSubmit();
        await new Promise(resolve => setTimeout(resolve, 100));
        assert(posts === before, 'A pending series lookup must block the actual HTMX request.');
        assertStatus(panel, false);
        await htmx.ajax('POST', form.action, {source: form, target: '#editor-errors'});
        assert(posts === before, 'Programmatic HTMX saves must also be blocked while series details load.');
      };
      choose();
      await wait(() => release);
      await blocked();
      reject();
      await wait(() => panel.getAttribute('aria-busy') === 'false');
      form.requestSubmit();
      await wait(() => posts === 1);
      choose();
      await blocked();
      panel.querySelector('[data-series-search-cancel]').click();
      form.requestSubmit();
      await wait(() => posts === 2);
      panel.querySelector('[data-series-find]').click();
      search.value = 'Review shared';
      search.dispatchEvent(new Event('input', {bubbles: true}));
      await wait(() => panel.querySelector('[data-series-search-results] button'));
      choose();
      await blocked();
      release();
      await wait(() => form.elements.seriesID.value && panel.getAttribute('aria-busy') === 'false');
      form.requestSubmit();
      await wait(() => posts === 3);
      document.removeEventListener('htmx:before:request', intercept);
      window.fetch = nativeFetch;
    } else if ('TEST_MODE' === 'series_only') {
      const title = panel.querySelector('[data-post-picker-title]');
      title.value = 'Unsaved series name';
      title.dispatchEvent(new Event('input', {bubbles: true}));
      assert((await nativeFetch('/test/change-series', {method: 'POST'})).ok, 'The concurrent series edit must succeed.');
      form.requestSubmit();
      await wait(() => form.querySelector('[data-series-reload-summary]'));
      form.querySelector('[data-series-reload-summary]').click();
      await wait(() => panel.querySelector('[data-series-status]').textContent.includes('Current series loaded.'));
      assert(panel.querySelector('[data-series-unsaved]').hidden, 'Accepting the current series must clear its unsaved badge.');
      assert(form.querySelector('[data-draft-status]').textContent === 'No unsaved changes.', 'Discarding the only edits must also clear the overall unsaved state.');
      assert(!Object.keys(localStorage).some(key => key.startsWith('hyperserver.blog.draft.v1:')), 'Accepting saved data without other edits must remove the recovery copy.');
      const leaving = new Event('beforeunload', {cancelable: true});
      window.dispatchEvent(leaving);
      assert(!leaving.defaultPrevented, 'Accepting the only unsaved edits must not warn before leaving.');
      form.elements.title.value = 'New post edit';
      form.elements.title.dispatchEvent(new Event('input', {bubbles: true}));
      assert(form.querySelector('[data-draft-status]').textContent === 'Unsaved changes.', 'Post edits after recovery must still be tracked.');
    } else {
      const fields = Object.fromEntries([...form.querySelectorAll('input[name],textarea[name],select[name]')]
        .filter(input => input.type !== 'file' && input.name !== 'version' && (input.type !== 'radio' || input.checked))
        .map(input => [input.name, input.type === 'checkbox' ? input.checked : input.value]));
      const baseline = JSON.stringify(fields);
      fields.title = 'Recovered writing';
      if ('TEST_MODE' === 'recover_none') {
        fields.seriesID = '';
        fields.seriesVersion = '0';
        fields.series = '';
      } else {
        if ('TEST_MODE' !== 'stale_untouched') fields.series = JSON.stringify({...JSON.parse(fields.series), title: 'Recovered series edit'});
        assert((await nativeFetch('/test/change-series', {method: 'POST'})).ok, 'The concurrent series edit must succeed.');
      }
      localStorage.setItem('hyperserver.blog.draft.v1:' + form.dataset.draftOwner + ':' + new URL(form.action).pathname + ':review-copy', JSON.stringify({saved: Date.now(), baseline, fields}));
      await htmx.ajax('GET', '/post/update/SOURCE_ID', {target: '#article-content', swap: 'innerHTML'});
      form = document.querySelector('#post-form');
      panel = document.querySelector('[data-series-panel]');
      await wait(() => form.querySelector('.CodeMirror') && !form.querySelector('[data-draft-recovery]').hidden);
      form.querySelector('[data-draft-restore]').click();
      await wait(() => form.elements.title.value === 'Recovered writing');
      panel.querySelector('details').open = true;
      if ('TEST_MODE' === 'recover_none') {
        assert(!panel.querySelector('[data-series-recovery]').hidden && !form.querySelector('[data-series-reload-summary]'), 'Recovery without a series edit must preserve saved membership without requiring a reload.');
        assert(panel.querySelector('[data-series-actions]').hidden, 'Recovery must not offer creating a second series while a saved membership exists.');
      } else if ('TEST_MODE' !== 'stale_untouched') {
        const responses = [];
        document.addEventListener('htmx:after:request', event => {
          if (event.detail.ctx.sourceElement === form) responses.push(event.detail.ctx.response.raw.status);
        });
        form.requestSubmit();
        await wait(() => responses.length);
        assert(responses[0] === 409 && form.querySelector('#editor-errors').textContent.includes('Click here') && form.querySelectorAll('[data-series-reload-summary]').length === 1, 'An edited stale copy must receive one recovery action in the error message.');
        const action = form.querySelector('#editor-errors [data-series-reload-summary]');
        assert(action?.type === 'button' && action.textContent === 'Click here', 'The conflict message must contain its own recovery action.');
        assert(document.activeElement === action, 'A series conflict must focus the recovery action beside the error.');
        const explanation = document.getElementById(action.getAttribute('aria-describedby'));
        assert(explanation.textContent === 'to replace your unsaved series edits with the current version of the series. Your post edits will be kept.', 'The error action must explain which edits it replaces.');
        assert(action.parentElement === explanation.parentElement && getComputedStyle(explanation).display === 'inline', 'The link, explanation, and reassurance must share one paragraph.');
        assert(action.getBoundingClientRect().height >= 44 && document.documentElement.scrollWidth <= innerWidth + 1, 'The error action must fit a mobile viewport and have a usable touch target.');
      }
      if ('TEST_MODE' === 'stale' || 'TEST_MODE' === 'removed') {
        // A failed reload must preserve both sets of edits and allow retry.
        const staged = form.elements.series.value;
        const summary = form.querySelector('#editor-errors');
        summary.prepend(document.createTextNode('\n  '));
        summary.append(document.createTextNode('\n  '));
        panel.querySelector('details').open = false;
        window.fetch = (url, options) => String(url).includes('/post/series/') ? Promise.reject(new Error('Unavailable')) : nativeFetch(url, options);
        summary.querySelector('[data-series-reload-summary]').click();
        await wait(() => panel.querySelector('[data-series-status]').textContent.includes('Unable to load the current series'));
        assertStatus(panel, true);
        const feedback = summary.querySelector('[data-series-reload-status]');
        assert(!panel.querySelector('details').open && feedback.checkVisibility() && feedback.textContent.includes('Unable to load the current series'), 'A failed recovery must show feedback beside its action even when Series is collapsed.');
        assert(summary.textContent.includes('Click here') && getComputedStyle(summary).display !== 'none', 'A failed load must preserve the unresolved conflict.');
        assert(form.elements.series.value === staged && form.elements.title.value === 'Recovered writing', 'A failed reload must keep post and series edits.');
        let release;
        window.fetch = (url, options) => String(url).includes('/post/series/') ? new Promise(resolve => { release = () => resolve(nativeFetch(url, options)); }) : nativeFetch(url, options);
        // Recover from the error summary without navigating back to the series.
        panel.querySelector('details').open = false;
        const action = summary.querySelector('[data-series-reload-summary]');
        action.scrollIntoView({block: 'center'});
        action.click();
        await wait(() => release);
        assert(action.disabled, 'The recovery action must be disabled while its request is pending.');
        assert(panel.querySelector('[data-series-status]').textContent === 'Loading current series…', 'Retrying must replace the failure with loading status.');
        assert(feedback.checkVisibility() && feedback.textContent === 'Loading current series…', 'Retry progress must stay visible beside the recovery action.');
        assertStatus(panel, false);
        release();
        await wait(() => panel.querySelector('[data-series-status]').textContent.includes('Current series loaded.'));
        window.fetch = nativeFetch;
        assertStatus(panel, false);
        assert(summary.isConnected && summary.childNodes.length === 0 && getComputedStyle(summary).display === 'none', 'Resolving the conflict must hide the empty error panel and retain its HTMX target.');
        assert(!form.elements.series.hasAttribute('aria-invalid') && !(form.elements.series.getAttribute('aria-describedby') || '').split(/\s+/).includes(summary.id), 'Resolving the conflict must clear its invalid state and error description.');
        assert(document.querySelector('#post-form') === form && form.elements.title.value === 'Recovered writing', 'Reload must keep the mounted post and its recovered edits.');
        assert(!form.querySelector('[data-series-reload-summary]'), 'Conflict recovery must disappear after accepting saved data.');
        assert(form.querySelector('[data-draft-status]').textContent !== 'No unsaved changes.', 'Accepting the saved series must retain the unsaved state of actual post edits.');
        assert(panel.querySelector('details').open && document.activeElement === panel.querySelector('summary'), 'Loading from the error must open and focus the series.');
        const top = panel.querySelector('summary').getBoundingClientRect().top;
        assert(top >= 0 && top < innerHeight, 'Loading from the error must bring the series into view.');
        if ('TEST_MODE' === 'removed') {
          assert(!form.elements.seriesID.value && !panel.querySelector('[data-series-actions]').hidden, 'A removed series must restore Create and Find actions.');
        } else {
          const title = JSON.parse(form.elements.series.value).title;
          assert(title === 'Changed in another editor', 'Reload must replace stale series content together with its version.');
          assert(!panel.querySelector('[data-series-selection]').hidden, 'Reload must restore the series editor.');
        }
      } else {
        assert(!form.querySelector('[data-series-reload-summary]'), 'An untouched stale series must not require conflict recovery.');
      }
      const responses = [];
      // Keep the test on the mounted editor after a real successful save.
      // HTMX handles redirect headers before its response events run.
      window.fetch = async (url, options) => {
        const response = await nativeFetch(url, options);
        if (String(url).includes('/post/SOURCE_ID') && options?.method === 'POST') {
          responses.push(response.status);
          const headers = new Headers(response.headers);
          headers.delete('HX-Redirect');
          return new Response(await response.text(), {status: response.status, headers});
        }
        return response;
      };
      form.requestSubmit();
      await wait(() => responses.length);
      assert(responses[0] === 200, 'Reloaded series state must save successfully without a repeated conflict.');
      window.fetch = nativeFetch;
      const saved = new DOMParser().parseFromString(await (await nativeFetch('/post/update/SOURCE_ID')).text(), 'text/html');
      assert(saved.querySelector('[name="title"]').value === 'Recovered writing', 'The recovered post edits must persist after resolving the series conflict.');
      if ('TEST_MODE' === 'stale_untouched') assert(JSON.parse(saved.querySelector('[name="series"]').value).title === 'Changed in another editor', 'Saving an untouched stale series must preserve the other editor’s series changes.');
    }
    await report('PASS');
  } catch (error) {
    await report(error.message + '\n' + error.stack);
  }
})();
