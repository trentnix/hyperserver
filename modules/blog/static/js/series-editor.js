import {createPostPicker} from './post-picker.js';
import {jsonComponentValue, registerSourceComparison} from './post-components.js';

const initialized = new WeakSet();
function initialize(root) {
  for (const section of root.querySelectorAll('[data-series-panel]')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('[name="series"]');
    if (!source) continue;
    const form = source.form;
    registerSourceComparison(source, jsonComponentValue);
    const id = form.elements.seriesID;
    const version = form.elements.seriesVersion;
    const original = form.elements.seriesBaseline;
    const selection = section.querySelector('[data-series-selection]');
    const status = section.querySelector('[data-series-status]');
    const actions = section.querySelector('[data-series-actions]');
    const searchPanel = section.querySelector('[data-series-search-panel]');
    const search = section.querySelector('[data-series-search]');
    const results = section.querySelector('[data-series-search-results]');
    const searchStatus = section.querySelector('[data-series-search-status]');
    const lifetime = new AbortController();
    let savedID = section.dataset.savedSeries;
    let request, searchRequest, timer, baseline, searching = false;
    const picker = createPostPicker(section, {
      source, maxPosts: section.dataset.postId ? 100 : 99, searchURL: section.dataset.seriesUrl,
      parameters: () => ({choices: '1', collection: id.value === 'new' ? '' : id.value}),
    });
    const snapshot = () => [id.value, version.value, jsonComponentValue(source.value)];
    function setStatus(message = '', failed = false) {
      status.textContent = message;
      status.className = failed ? 'field-error' : 'form-hint';
      status.setAttribute('role', failed ? 'alert' : 'status');
      status.hidden = !message;
      const recoveryStatus = form.querySelector('[data-series-reload-status]');
      if (recoveryStatus) {
        recoveryStatus.textContent = message;
        recoveryStatus.hidden = !message;
      }
    }
    function update() {
      selection.hidden = !id.value;
      source.disabled = !id.value;
      section.querySelector('[data-series-help]').hidden = !id.value;
      const controls = section.querySelector('[data-post-picker-controls]');
      controls.disabled = !id.value || controls.hidden;
      section.setAttribute('aria-busy', String(Boolean(request)));
      if (actions) {
        actions.hidden = Boolean(id.value) || Boolean(savedID) || searching;
        searchPanel.hidden = Boolean(id.value) || !searching;
        section.querySelector('[data-series-cancel]').hidden = !id.value || Boolean(savedID);
        section.querySelector('[data-series-recovery]').hidden = Boolean(id.value) || !savedID;
        results.querySelectorAll('button').forEach(button => { button.disabled = Boolean(request); });
      }
      form.querySelectorAll('[data-series-reload-summary]').forEach(button => { button.disabled = Boolean(request); });
      let title = '';
      try {
        const value = JSON.parse(source.value);
        title = value.title || '';
        section.querySelector('[data-series-empty]').hidden = !section.dataset.postId || id.value === 'new' || !id.value || value.posts.length > 0;
      } catch { /* Invalid source stays available for correction. */ }
      const label = section.querySelector('[data-series-title]');
      label.textContent = id.value === 'new' ? 'New series' : title;
      label.hidden = !id.value;
      section.querySelector('[data-series-unsaved]').hidden = baseline === undefined || baseline !== null && JSON.stringify(snapshot()) === JSON.stringify(baseline);
    }
    function cancelRequests() {
      request?.abort();
      request = undefined;
      searchRequest?.abort();
      searchRequest = undefined;
      clearTimeout(timer);
    }
    function clearSearch() {
      if (!search) return;
      search.value = '';
      results.replaceChildren();
      results.hidden = true;
      searchStatus.textContent = '';
    }
    function read() {
      cancelRequests();
      searching = false;
      clearSearch();
      setStatus();
      picker.read();
      update();
    }
    function stage(value, revision, content) {
      id.value = value;
      version.value = revision;
      source.value = content;
      read();
      source.dispatchEvent(new Event('input', {bubbles: true}));
    }
    function cancelChoice() {
      cancelRequests();
      searching = false;
      clearSearch();
      setStatus();
      update();
      section.querySelector('[data-series-find]').focus();
    }
    source.addEventListener('change', () => { cancelRequests(); searching = false; clearSearch(); update(); });
    section.addEventListener('invalid', () => { section.querySelector('details').open = true; }, true);
    form.addEventListener('blog:editor-baseline', event => {
      const fields = event.detail;
      baseline = fields ? [fields.seriesID || '', fields.seriesVersion || '0', jsonComponentValue(fields.series || '')] : null;
      update();
    });
    form.addEventListener('input', update);
    form.addEventListener('change', update);
    source.addEventListener('blog:field-validation', () => {
      if (source.getAttribute('aria-invalid') === 'true') {
        section.querySelector('details').open = true;
        queueMicrotask(() => {
          const recovery = form.querySelector('[data-series-reload-summary]');
          if (recovery) { recovery.focus(); return; }
          const title = section.querySelector('[data-post-picker-title]');
          if (source.hidden && !title.value.trim()) title.focus();
          else picker.focus();
        });
      }
    });
    section.querySelector('[data-series-create]')?.addEventListener('click', () => {
      stage('new', '0', JSON.stringify({title: '', posts: section.dataset.postId ? [section.dataset.postId] : []}));
      section.querySelector('[data-post-picker-title]').focus();
    });
    section.querySelector('[data-series-find]')?.addEventListener('click', () => {
      searching = true;
      update();
      search.focus();
    });
    section.querySelector('[data-series-cancel]')?.addEventListener('click', () => {
      stage('', '0', '');
      section.querySelector('[data-series-create]').focus();
    });
    section.querySelector('[data-series-search-cancel]')?.addEventListener('click', cancelChoice);
    function loadCurrentSeries() {
      if (!request) chooseSeries(section.dataset.postId ? '' : id.value, true);
    }
    form.addEventListener('click', event => {
      if (event.target.closest('[data-series-reload-summary]')) loadCurrentSeries();
    });

    async function chooseSeries(value, reloading = false) {
      request?.abort();
      const active = request = new AbortController();
      const timeout = setTimeout(() => active.abort(), 10000);
      setStatus(reloading ? 'Loading current series…' : '');
      if (!reloading) searchStatus.textContent = 'Loading series…';
      update();
      try {
        const response = await fetch(section.dataset.seriesUrl + (value ? '?collection=' + encodeURIComponent(value) : ''), {signal: active.signal, redirect: 'error'});
        if (!response.ok || !(response.headers.get('Content-Type') || '').includes('application/json')) throw new Error();
        const data = await response.json();
        if (request !== active || !section.isConnected) return;
        if (reloading) {
          if (section.dataset.postId) {
            savedID = data.id;
            original.value = data.baseline;
          }
        }
        stage(data.id, data.version, data.source);
        if (reloading) {
          if (section.dataset.postId) {
            baseline = snapshot();
            form.dispatchEvent(new CustomEvent('blog:editor-adopt-fields', {detail: ['seriesID', 'seriesVersion', 'seriesBaseline', 'series']}));
          }
          update();
          setStatus('Current series loaded. Your post edits are unchanged.');
          section.querySelector('details').open = true;
          section.querySelector('summary').focus({preventScroll: true});
          section.scrollIntoView({block: 'start'});
        } else {
          section.querySelector('[data-post-picker-title]').focus();
        }
      } catch {
        if (request !== active || !section.isConnected) return;
        searchStatus.textContent = `${results.children.length} series shown.`;
        setStatus(reloading ? 'Unable to load the current series. Your edits are unchanged. Try again.' : 'Unable to load the series. Choose it again to retry.', true);
      } finally {
        clearTimeout(timeout);
        if (request === active) request = undefined;
        if (section.isConnected) update();
      }
    }
    async function searchSeries() {
      clearTimeout(timer);
      searchRequest?.abort();
      searchRequest = undefined;
      results.replaceChildren();
      results.hidden = true;
      searchStatus.textContent = '';
      const query = search.value.trim();
      if (!query) return;
      const active = searchRequest = new AbortController();
      const loading = setTimeout(() => { if (searchRequest === active) searchStatus.textContent = 'Finding series…'; }, 200);
      const timeout = setTimeout(() => active.abort(), 10000);
      try {
        const response = await fetch(section.dataset.seriesUrl + '?q=' + encodeURIComponent(query), {signal: active.signal, redirect: 'error'});
        if (!response.ok || !(response.headers.get('Content-Type') || '').includes('application/json')) throw new Error();
        const choices = await response.json();
        if (searchRequest !== active || !section.isConnected) return;
        for (const value of choices) {
          const row = document.createElement('li');
          const button = document.createElement('button');
          button.type = 'button';
          button.title = value.title;
          const title = document.createElement('strong');
          title.textContent = value.title;
          const count = document.createElement('small');
          count.textContent = `${value.count} ${value.count === 1 ? 'post' : 'posts'}`;
          button.append(title, count);
          if (value.posts.length) {
            const posts = document.createElement('span');
            posts.textContent = 'Includes: ' + value.posts.join(' · ');
            button.append(posts);
          }
          button.addEventListener('click', () => chooseSeries(value.id));
          row.append(button);
          results.append(row);
        }
        results.hidden = choices.length === 0;
        searchStatus.textContent = choices.length ? `${choices.length} series shown.` : 'No matching series.';
      } catch {
        if (searchRequest !== active || !section.isConnected) return;
        searchStatus.textContent = 'Unable to search series. Try again.';
        const retry = document.createElement('button');
        retry.type = 'button';
        retry.className = 'text-button';
        retry.textContent = 'Retry';
        retry.addEventListener('click', searchSeries);
        searchStatus.append(' ', retry);
      } finally {
        clearTimeout(loading);
        clearTimeout(timeout);
        if (searchRequest === active) searchRequest = undefined;
      }
    }
    search?.addEventListener('input', () => {
      cancelRequests();
      results.replaceChildren();
      results.hidden = true;
      searchStatus.textContent = '';
      setStatus();
      update();
      timer = setTimeout(searchSeries, 200);
    });
    search?.addEventListener('keydown', async event => {
      if (event.key === 'Enter') { event.preventDefault(); await searchSeries(); results.querySelector('button')?.focus(); }
      if (event.key === 'ArrowDown' && !results.hidden) { event.preventDefault(); results.querySelector('button')?.focus(); }
    });
    searchPanel?.addEventListener('keydown', event => {
      if (event.key === 'Escape') { event.preventDefault(); cancelChoice(); }
    });
    function blockPendingSave(event) {
      if (!request) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      section.querySelector('details').open = true;
      setStatus('Wait for the series to load before saving.');
    }
    // Capture ordinary submissions before HTMX starts a request. Programmatic
    // HTMX submissions also pass through the request configuration event.
    form.addEventListener('submit', blockPendingSave, true);
    document.addEventListener('htmx:config:request', event => {
      if (event.detail.ctx.sourceElement === form) blockPendingSave(event);
    }, {signal: lifetime.signal});
    document.addEventListener('htmx:before:cleanup', event => {
      if (event.target.contains?.(section)) { cancelRequests(); lifetime.abort(); }
    }, {signal: lifetime.signal});
    read();
  }
}
document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
