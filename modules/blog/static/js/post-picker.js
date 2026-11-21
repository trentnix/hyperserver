import {markField} from './forms.js';

const validID = id => typeof id === 'string' && /^[A-Z2-7]{26}$/.test(id);

export function createPostPicker(section, {source, maxPosts = 12, searchURL = "/post/related", exclude = "", includePages = false, parameters = () => ({})}) {
  const sourceLabel = section.querySelector('[data-post-picker-source-label]');
  const controls = section.querySelector('[data-post-picker-controls]');
  const title = section.querySelector('[data-post-picker-title]');
  const search = section.querySelector('[data-post-picker-search]');
  const selections = section.querySelector('[data-post-picker-selections]');
  const count = section.querySelector('[data-post-picker-count]');
  const selectedPanel = section.querySelector('.post-picker-selected-panel');
  const searchPanel = section.querySelector('.post-picker-search-panel');
  const results = section.querySelector('[data-post-picker-results]');
  const status = section.querySelector('[data-post-picker-status]');
  const limit = section.querySelector('[data-post-picker-limit]');
  const item = includePages ? 'link' : 'post';
  const items = item + 's';
  const validLink = id => validID(id) || (includePages && typeof id === 'string' && id.startsWith('page:') && validID(id.slice(5)));
  const choiceLabel = (target, state) => [includePages && target?.kind, state].filter(Boolean).join(' · ');
  limit.textContent = `${maxPosts} ${items} selected. Remove one to choose another.`;
  const lifetime = new AbortController();
  const targets = new Map();
  let selectedIDs = [], field = 'posts', request, timer, resolved = false, suggesting = false, activeOption = -1;

  function closeSuggestions() {
    suggesting = false;
    activeOption = -1;
    results.hidden = true;
    search.setAttribute('aria-expanded', 'false');
    search.removeAttribute('aria-activedescendant');
    if (searchPanel.contains(status)) status.textContent = '';
  }

  function highlight(index) {
    const options = [...results.children];
    activeOption = index;
    options.forEach((option, i) => { option.dataset.active = String(i === index); });
    search.setAttribute('aria-activedescendant', options[index].id);
    options[index].scrollIntoView({block: 'nearest'});
  }

  // The dialog footer sits outside its scrollable body. Make space for the
  // results or message while keeping the focused search field in view.
  function revealSearchFeedback(feedback) {
    const body = section.closest('.component-dialog-body');
    if (!body || document.activeElement !== search) return;
    const viewport = body.getBoundingClientRect();
    const overflow = feedback.getBoundingClientRect().bottom + 8 - viewport.bottom;
    const room = search.getBoundingClientRect().top - viewport.top - 8;
    if (overflow > 0 && room > 0) body.scrollTop += Math.min(overflow, room);
  }

  function serialize() {
    const key = includePages && selectedIDs.some(id => id.startsWith('page:')) ? 'links' : field;
    source.value = selectedIDs.length || title.value ? JSON.stringify({title: title.value, [key]: selectedIDs}) : '';
    markField(source, false);
    source.dispatchEvent(new Event('input', {bubbles: true}));
  }

  function updateChoice(button, post) {
    const added = selectedIDs.includes(post.id);
    const description = choiceLabel(post, post.state);
    button.setAttribute('aria-selected', String(added));
    button.setAttribute('aria-disabled', String(added));
    button.setAttribute('aria-label', `${added ? 'Added' : 'Add'} ${post.title}${description ? ` (${description})` : ''}`);
    button.querySelector('small').textContent = choiceLabel(post, added ? 'Added' : post.state);
    button.querySelector('path').setAttribute('d', added ? 'm5 12 4 4L19 6' : 'M12 5v14M5 12h14');
  }

  function action(name, path, click) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'text-button';
    button.setAttribute('aria-label', name);
    button.title = name;
    // Only fixed icon paths enter HTML. Authored text uses textContent.
    button.innerHTML = `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="${path}"/></svg>`;
    button.addEventListener('click', click);
    return button;
  }

  function renderSelections() {
    const focusedRow = document.activeElement?.closest('[data-post-picker-id]');
    const focusedID = focusedRow?.dataset.postPickerId;
    const focusedControl = focusedRow ? [...focusedRow.querySelectorAll('button')].indexOf(document.activeElement) : -1;
    selections.replaceChildren();
    count.textContent = selectedIDs.length;
    selectedPanel.hidden = selectedIDs.length === 0;
    selectedIDs.forEach((id, index) => {
      const target = targets.get(id);
      const row = document.createElement('li');
      row.dataset.postPickerId = id;
      const details = document.createElement('div');
      details.className = 'post-picker-selection-details';
      const name = document.createElement('strong');
      name.textContent = target?.title || (resolved ? `${includePages ? 'Link' : 'Post'} unavailable` : `Selected ${item}`);
      name.title = name.textContent;
      details.append(name);
      const stateLabel = target?.state || (!target && resolved ? 'Unavailable' : '');
      if (stateLabel || (includePages && target?.kind)) {
        const state = document.createElement('small');
        state.textContent = choiceLabel(target, stateLabel);
        if (stateLabel) {
          state.title = 'Hidden from readers';
          state.dataset.state = stateLabel.toLowerCase();
        }
        details.append(state);
      }
      const actions = document.createElement('div');
      actions.className = 'post-picker-selection-actions';
      for (const [offset, direction, path] of [[-1, 'up', 'm6 14 6-6 6 6'], [1, 'down', 'm6 10 6 6 6-6']]) {
        const move = action(`Move ${target?.title || item} ${direction}`, path, () => {
          [selectedIDs[index], selectedIDs[index + offset]] = [selectedIDs[index + offset], selectedIDs[index]];
          serialize();
          renderSelections();
          const buttons = [...selections.children[index + offset].querySelectorAll('button')];
          (buttons.find(button => button.title.endsWith(direction) && !button.disabled) || buttons.find(button => !button.disabled)).focus();
        });
        move.disabled = index + offset < 0 || index + offset >= selectedIDs.length;
        actions.append(move);
      }
      actions.append(action(`Remove ${target?.title || item}`, 'm7 7 10 10M7 17 17 7', () => {
        selectedIDs.splice(index, 1);
        serialize();
        renderSelections();
        (selections.children[Math.min(index, selectedIDs.length - 1)]?.querySelector('button:last-child') || search).focus();
        loadChoices();
      }));
      row.append(details, actions);
      selections.append(row);
    });
    search.disabled = selectedIDs.length >= maxPosts;
    limit.hidden = !search.disabled;
    search.setAttribute('aria-describedby', `${source.id}-help ${status.id}` + (search.disabled ? ` ${limit.id}` : ''));
    if (search.disabled) closeSuggestions();
    if (focusedID) {
      const row = [...selections.children].find(row => row.dataset.postPickerId === focusedID);
      const control = row?.querySelectorAll('button')[focusedControl];
      if (control && !control.disabled) control.focus({preventScroll: true});
    }
  }

  async function loadChoices() {
    clearTimeout(timer);
    request?.abort();
    request = undefined;
    results.removeAttribute('aria-busy');
    if (section.hidden || controls.hidden || !section.isConnected) return;
    const query = search.disabled ? '' : search.value.trim();
    const searching = Boolean(query && suggesting);
    results.replaceChildren();
    results.hidden = true;
    activeOption = -1;
    search.setAttribute('aria-expanded', 'false');
    search.removeAttribute('aria-activedescendant');
    status.textContent = '';
    if (!query && selectedIDs.length === 0) return;
    request = new AbortController();
    const active = request;
    const params = new URLSearchParams({q: query, ids: selectedIDs.join(','), exclude, ...parameters()});
    results.setAttribute('aria-busy', 'true');
    const loading = setTimeout(() => {
      if (request === active && section.isConnected && !section.hidden && query && suggesting) {
        status.textContent = `Finding ${includePages ? 'posts and pages' : 'posts'}…`;
        revealSearchFeedback(status);
      }
    }, 200);
    const timeout = setTimeout(() => active.abort(), 10000);
    try {
      const response = await fetch(searchURL + '?' + params, {signal: active.signal, redirect: 'error'});
      if (!response.ok || !(response.headers.get('Content-Type') || '').includes('application/json')) throw new Error();
      const value = await response.json();
      const options = includePages ? value.choices : value.posts;
      if (!Array.isArray(options) || !Array.isArray(value.selected) || [...options, ...value.selected].some(post => !validLink(post.id) || typeof post.title !== 'string')) throw new Error();
      if (request !== active || !section.isConnected || section.hidden) return;
      targets.clear();
      for (const post of [...options, ...value.selected]) targets.set(post.id, post);
      resolved = true;
      renderSelections();
      results.replaceChildren();
      for (const post of options) {
        const button = document.createElement('button');
        button.type = 'button';
        button.id = source.id + '-option-' + post.id;
        button.setAttribute('role', 'option');
        button.tabIndex = -1;
        button.title = post.title;
        const name = document.createElement('span');
        name.textContent = post.title;
        const state = document.createElement('small');
        button.append(name, state);
        const icon = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
        icon.setAttribute('viewBox', '0 0 24 24');
        icon.setAttribute('aria-hidden', 'true');
        icon.innerHTML = '<path d="M12 5v14M5 12h14"/>';
        button.append(icon);
        updateChoice(button, post);
        button.addEventListener('click', () => {
          if (selectedIDs.length >= maxPosts) return;
          if (selectedIDs.includes(post.id)) { search.focus({preventScroll: true}); return; }
          selectedIDs.push(post.id);
          serialize();
          renderSelections();
          updateChoice(button, post);
          if (search.disabled) selections.lastElementChild.querySelector('button:last-child').focus();
          else {
            search.focus({preventScroll: true});
            highlight([...results.children].indexOf(button));
          }
        });
        results.append(button);
      }
      const open = suggesting && query && selectedIDs.length < maxPosts && options.length > 0 && document.activeElement === search;
      results.hidden = !open;
      search.setAttribute('aria-expanded', String(Boolean(open)));
      status.textContent = query && suggesting && !options.length ? `No matching ${includePages ? 'posts or pages' : 'posts'}.` : '';
      if (open) revealSearchFeedback(results);
      else if (status.textContent) revealSearchFeedback(status);
    } catch {
      if (request !== active || !section.isConnected || section.hidden || searching && !suggesting) return;
      results.replaceChildren();
      status.textContent = `Unable to load ${includePages ? 'posts and pages' : 'posts'}. `;
      const retry = document.createElement('button');
      retry.type = 'button';
      retry.className = 'text-button';
      retry.textContent = 'Retry';
      retry.addEventListener('click', () => {
        if (search.disabled) {
          title.focus();
          loadChoices();
          return;
        }
        const focused = document.activeElement === search;
        search.focus();
        if (focused || !search.value.trim()) loadChoices();
      });
      status.append(retry);
      revealSearchFeedback(status);
    } finally {
      clearTimeout(loading);
      clearTimeout(timeout);
      if (request === active) results.removeAttribute('aria-busy');
    }
  }

  function read(load = true) {
    request?.abort();
    request = undefined;
    clearTimeout(timer);
    let value, ids, selectionField;
    try {
      value = source.value.trim() ? JSON.parse(source.value) : {posts: []};
      selectionField = includePages && value?.links != null ? 'links' : 'posts';
      ids = value?.[selectionField];
      const valid = selectionField === 'links' ? validLink : validID;
      if (!Array.isArray(ids) || !ids.every(valid) || ids.length > maxPosts || new Set(ids).size !== ids.length ||
          Object.keys(value).some(key => key !== selectionField && (key !== 'title' || typeof value.title !== 'string'))) throw new Error();
    } catch {
      source.hidden = sourceLabel.hidden = false;
      controls.hidden = controls.disabled = true;
      controls.after(status);
      status.textContent = `The ${item} selection source is invalid. Correct it before saving.`;
      return;
    }
    selectedIDs = ids;
    field = selectionField;
    resolved = false;
    targets.clear();
    title.value = value.title || '';
    source.hidden = sourceLabel.hidden = true;
    controls.hidden = false;
    controls.disabled = section.hidden;
    searchPanel.append(status);
    results.replaceChildren();
    closeSuggestions();
    results.removeAttribute('aria-busy');
    status.textContent = '';
    renderSelections();
    if (load) loadChoices();
  }

  title.addEventListener('input', serialize);
  search.addEventListener('input', () => {
    request?.abort();
    request = undefined;
    clearTimeout(timer);
    closeSuggestions();
    results.replaceChildren();
    results.removeAttribute('aria-busy');
    suggesting = Boolean(search.value.trim());
    status.textContent = '';
    if (!suggesting) {
      if (selectedIDs.length && !resolved) loadChoices();
      return;
    }
    timer = setTimeout(loadChoices, 200);
  });
  search.addEventListener('keydown', event => {
    if (event.key === 'Escape') {
      if (suggesting || !results.hidden || status.textContent) {
        event.preventDefault();
        closeSuggestions();
      }
      return;
    }
    if (event.key === 'Tab') {
      if (event.shiftKey || !status.querySelector('button')) closeSuggestions();
      return;
    }
    if (['ArrowDown', 'ArrowUp'].includes(event.key) && !results.hidden && results.children.length) {
      event.preventDefault();
      const options = [...results.children];
      const step = event.key === 'ArrowDown' ? 1 : -1;
      let index = activeOption < 0 && step < 0 ? 0 : activeOption;
      for (let visited = 0; visited < options.length; visited++) {
        index = (index + step + options.length) % options.length;
        if (options[index].getAttribute('aria-disabled') !== 'true') { highlight(index); break; }
      }
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      if (!results.hidden) (results.children[activeOption] || results.querySelector('[aria-disabled="false"]'))?.click();
      else if (search.value.trim()) { suggesting = true; loadChoices(); }
    }
  });
  search.addEventListener('focus', () => {
    if (search.value.trim()) {
      suggesting = true;
      if (results.hidden || !results.children.length) loadChoices();
    }
  });
  searchPanel.addEventListener('focusout', event => {
    if (!searchPanel.contains(event.relatedTarget)) closeSuggestions();
  });
  document.addEventListener('pointerdown', event => {
    if (!searchPanel.contains(event.target)) closeSuggestions();
  }, {signal: lifetime.signal});
  source.addEventListener('change', () => read());
  document.addEventListener('htmx:before:cleanup', event => {
    if (event.target.contains?.(source.form)) { request?.abort(); request = undefined; clearTimeout(timer); lifetime.abort(); }
  }, {signal: lifetime.signal});
  return {
    read,
    snapshot: () => source.value,
    clear: () => { source.value = ''; search.value = ''; read(false); },
    restore: copy => { source.value = copy; read(); },
    focus: () => { (source.hidden ? search.disabled ? title : search : source).focus(); loadChoices(); }
  };
}
