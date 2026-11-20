import {getMarkdownEditor} from './markdown-editor.js';
import {componentValue, restoreComponentInstances} from './post-components.js';

// Recovery copies belong to this browser, not the published content. Each editor
// instance owns a key so separate tabs never overwrite one another's writing.
(() => {
  const states = new Map();
  const prefix = 'hyperserver.blog.draft.v1:';
  const lifetime = 7 * 24 * 60 * 60 * 1000;
  const maxLength = 1024 * 1024;
  const instances = form => [...form.querySelectorAll('[data-component-id] textarea[name]:enabled')];

  function snapshot(state) {
    return Object.fromEntries([...state.fields, ...instances(state.form)].filter(field => field.type !== 'radio' || field.checked)
      .map(field => [field.name, field.closest('[data-component-pending]') ? '' :
        field.type === 'checkbox' ? field.checked : getMarkdownEditor(field)?.getMarkdown() ?? field.value]));
  }
  const encoded = state => JSON.stringify(snapshot(state));
  function dirty(state) {
    if (!state.baseline) return true;
    const before = JSON.parse(state.baseline);
    const after = snapshot(state);
    for (const source of [...state.componentSources, ...instances(state.form)]) {
      if (!Object.hasOwn(before, source.name)) continue;
      before[source.name] = componentValue(source, before[source.name] || '');
      after[source.name] = componentValue(source, after[source.name] || '');
    }
    return JSON.stringify(after) !== JSON.stringify(before);
  }
  const active = state => state.form.isConnected;

  function publishBaseline(state) {
    // Rejected ordinary submissions retain a component baseline without treating
    // their other submitted fields as saved.
    const baseline = state.baseline || state.form.dataset.currentContent;
    state.form.dispatchEvent(new CustomEvent('blog:editor-baseline', {detail: baseline ? JSON.parse(baseline) : null}));
  }

  function setStatus(state, message, failed = false) {
    if (state.status.textContent !== message) state.status.textContent = message;
    state.status.classList.toggle('draft-status--error', failed);
  }

  function storageFailure(state) {
    setStatus(state, 'Recovery copy could not be saved. Keep this editor open and save your changes.', true);
  }

  // Only remove the exact copy we read. Another tab might still be editing it.
  function removeCopy(copy) {
    const current = localStorage.getItem(copy.key);
    if (current !== null && current !== copy.raw) return false;
    localStorage.removeItem(copy.key);
    return true;
  }

  function dismissRecovery(state) {
    state.recovery = null;
    state.recoveryCopies = [];
    state.form.querySelector('[data-draft-recovery]').hidden = true;
  }

  function copiesBeforeSave(state) {
    // The create editor can hold recovery copies for several new items.
    if (!state.existing) return [...state.sources];
    return Object.keys(localStorage)
      .filter(key => key !== state.key && key.startsWith(state.scope))
      .map(key => ({key, raw: localStorage.getItem(key)}))
      .filter(copy => copy.raw !== null);
  }

  function clearSavedCopies(state, copies) {
    copies.forEach(removeCopy);
    state.sources = [];
    dismissRecovery(state);
  }

  function persist(state) {
    clearTimeout(state.timer);
    try {
      if (!dirty(state)) {
        localStorage.removeItem(state.key);
        state.lastCopy = null;
        setStatus(state, 'No unsaved changes.');
        return;
      }
      const copy = {saved: Date.now(), baseline: state.baseline, fields: snapshot(state)};
      const previous = state.lastCopy;
      // A save or discard in another editor may have cleared this copy. A
      // passive save on navigation must not put the same copy back.
      if (previous && previous.baseline === copy.baseline && localStorage.getItem(state.key) === null &&
          JSON.stringify(previous.fields) === JSON.stringify(copy.fields)) {
        setStatus(state, 'Unsaved changes.');
        return;
      }
      const raw = JSON.stringify(copy);
      if (raw.length > maxLength) throw new Error('Recovery copy too large');
      localStorage.setItem(state.key, raw);
      state.lastCopy = copy;
      setStatus(state, 'Saved locally. Use Save to apply changes.');
    } catch {
      storageFailure(state);
    }
  }

  // A scheduled copy may outlive publication, when Schedule is no longer offered.
  function unavailablePublication(state, copy) {
    return Object.hasOwn(copy.data.fields, 'published') &&
      ![...state.form.querySelectorAll('[name="published"]')].some(field => field.value === copy.data.fields.published);
  }

  // Read only this account's copies. Expired or malformed copies are not restored.
  function offerRecovery(state) {
    try {
      const copies = [];
      for (const key of Object.keys(localStorage)) {
        if (!key.startsWith(state.ownerPrefix)) continue;
        const raw = localStorage.getItem(key);
        let data;
        try { data = JSON.parse(raw); } catch { /* Invalid copies are removed below. */ }
        if (!data || !Number.isFinite(data.saved) || Date.now() - data.saved > lifetime || raw.length > maxLength) {
          localStorage.removeItem(key);
          continue;
        }
        if (!key.startsWith(state.scope) || key === state.key || state.sources.some(copy => copy.key === key)) continue;
        // Preserve copies from the former publication checkboxes.
        if (typeof data.fields?.published === 'boolean' || data.fields?.schedule === true) {
          data.fields.published = data.fields.schedule ? 'scheduled' : data.fields.published ? 'on' : '';
        }
        // Split copies saved by the former combined date/time control.
        if (typeof data.fields?.publishAt === 'string' && data.fields.publishDate === undefined && data.fields.publishTime === undefined) {
          [data.fields.publishDate, data.fields.publishTime = ''] = data.fields.publishAt.split('T');
        }
        if (typeof data.baseline !== 'string' || !data.fields || !state.fields.every(field =>
          field.type === 'checkbox' ? data.fields[field.name] === undefined || typeof data.fields[field.name] === 'boolean' :
            ((['previewImage', 'published', 'publishDate', 'publishTime', 'publishTimeZone', 'tags', 'componentOrder', 'seriesID', 'seriesVersion', 'seriesBaseline'].includes(field.name) || state.componentSources.includes(field)) && data.fields[field.name] === undefined) || typeof data.fields[field.name] === 'string')) continue;
        copies.push({key, raw, data});
      }
      copies.sort((a, b) => b.data.saved - a.data.saved);
      state.recovery = copies[0];
      state.recoveryCopies = state.existing ? copies : copies.slice(0, 1);
      state.form.querySelector('[data-draft-discard]').textContent = state.recoveryCopies.length > 1 ? 'Discard copies' : 'Discard copy';
      state.form.querySelector('[data-draft-recovery]').hidden = !state.recovery;
      if (state.recovery) {
        const data = state.recovery.data;
        state.form.querySelector('[data-draft-description]').textContent =
          'An unsaved copy from ' + new Date(data.saved).toLocaleString() + ' is available.' +
          (data.baseline !== state.baseline ? ' The server version or initial settings have changed. Restoring replaces the fields in this editor only.' : '') +
          (unavailablePublication(state, state.recovery) ? ' The saved publication choice is no longer available. Your current publication setting will be kept.' : '');
      }
    } catch {
      storageFailure(state);
    }
  }

  function initialize() {
    document.querySelectorAll('form[data-draft-owner]').forEach(form => {
      if (states.has(form) || !form.dataset.draftOwner) return;
      const ownerPrefix = prefix + form.dataset.draftOwner + ':';
      // New subpages have different initial parents, but share the new-page editor.
      const scope = ownerPrefix + new URL(form.action).pathname + ':';
      const state = {
        form, ownerPrefix, scope, key: scope + crypto.randomUUID(), sources: [],
        existing: Number(form.elements.version.value) > 0,
        fields: [...form.querySelectorAll('input[name], textarea[name], select[name]')].filter(field => field.type !== 'file' && field.name !== 'version' && !field.closest('[data-component-id]')),
        componentSources: [...form.querySelectorAll('[data-post-component] textarea[name], [data-post-picker-source]')].filter(field => !field.closest('[data-component-id]')),
        status: form.querySelector('[data-draft-status]')
      };
      // Rejected ordinary submissions contain unsaved input, not a server baseline.
      state.baseline = form.hasAttribute('data-unsaved') ? '' : encoded(state);
      // A loaded saved version is an edit, not the current server baseline.
      // The concurrency token always belongs to the newly opened editor, not a recovery copy.
      if (form.dataset.currentContent && !form.hasAttribute('data-unsaved')) {
        const baseline = snapshot(state);
        for (const [name, value] of Object.entries(JSON.parse(form.dataset.currentContent))) {
          // Match the browser's input and textarea normalization without changing the editor.
          const field = form.elements.namedItem(name)?.cloneNode();
          if (field) field.value = value;
          baseline[name] = field?.value ?? value;
        }
        state.baseline = JSON.stringify(baseline);
      }
      states.set(form, state);
      publishBaseline(state);
      state.status.hidden = false;
      offerRecovery(state);
      if (dirty(state)) persist(state);
      const changed = () => {
        state.approved = null;
        clearTimeout(state.timer);
        if (!state.status.classList.contains('draft-status--error')) {
          setStatus(state, dirty(state) ? 'Unsaved changes.' : 'No unsaved changes.');
        }
        state.timer = setTimeout(() => persist(state), 500);
      };
      form.addEventListener('input', changed);
      form.addEventListener('change', changed);
      form.addEventListener('blog:editor-adopt-fields', event => {
        if (state.baseline) {
          const baseline = JSON.parse(state.baseline);
          const current = snapshot(state);
          for (const name of event.detail) {
            if (Object.hasOwn(current, name)) baseline[name] = current[name];
          }
          state.baseline = JSON.stringify(baseline);
          publishBaseline(state);
        }
        changed();
        persist(state);
      });
      function restoreCopy(copy) {
        if (!active(state) || state.recovery !== copy) return;
        try {
          if (localStorage.getItem(copy.key) !== copy.raw) {
            offerRecovery(state);
            return;
          }
        } catch { /* A cached copy can still be restored if storage is unavailable. */ }
        const keepPublication = unavailablePublication(state, copy);
        for (const field of state.fields) {
          // Fields added after this copy was saved keep their current settings.
          if (!Object.hasOwn(copy.data.fields, field.name)) continue;
          if (field.name === 'published' && keepPublication) continue;
          if (field.name === 'publishTimeZone' && !copy.data.fields.publishDate && !copy.data.fields.publishTime) continue;
          if (field.type === 'checkbox') field.checked = copy.data.fields[field.name];
          else if (field.type === 'radio') field.checked = field.value === copy.data.fields[field.name];
          else field.value = copy.data.fields[field.name];
        }
        const editor = getMarkdownEditor(form.elements.content);
        editor?.setMarkdown(form.elements.content.value);
        editor?.placement?.resetHistory();
        restoreComponentInstances(form, copy.data.fields);
        state.componentSources.forEach(field => field.dispatchEvent(new Event('change', {bubbles: true})));
        form.elements.componentOrder?.dispatchEvent(new Event('change', {bubbles: true}));
        form.querySelectorAll('[data-image-value]').forEach(field => field.dispatchEvent(new Event('change', {bubbles: true})));
        state.sources.push(copy);
        form.dispatchEvent(new Event('input', {bubbles: true}));
        persist(state);
        dismissRecovery(state);
        if (!state.status.classList.contains('draft-status--error')) {
          setStatus(state, 'Copy restored. Save to apply changes.');
        }
      }
      form.querySelector('[data-draft-restore]').addEventListener('click', () => {
        const copy = state.recovery;
        if (!copy) return;
        if (dirty(state)) {
          window.blogConfirm({title: 'Replace your current edits?', message: 'Restoring this browser copy replaces the fields in this editor. Saved content stays unchanged until you save.', action: 'Restore copy', cancel: 'Keep editing'}, () => restoreCopy(copy));
        } else restoreCopy(copy);
      });
      form.querySelector('[data-draft-discard]').addEventListener('click', () => {
        const copy = state.recovery;
        if (!copy) return;
        const copies = state.recoveryCopies;
        const many = copies.length > 1;
        const confirmation = many
          ? {title: 'Discard recovery copies?', message: 'Remove the available recovery copies for this item from this browser.', action: 'Discard copies', cancel: 'Keep copies'}
          : {title: 'Discard this browser copy?', message: 'Remove this recovery copy from this browser.', action: 'Discard copy', cancel: 'Keep copy'};
        window.blogConfirm(confirmation, () => {
          if (!active(state) || state.recovery !== copy) return;
          try {
            const changed = copies.filter(candidate => !removeCopy(candidate));
            if (changed.length) {
              offerRecovery(state);
              setStatus(state, many ? 'Some copies changed in another tab and are still available.' : 'This copy changed in another tab and is still available.', true);
              return;
            }
            dismissRecovery(state);
            setStatus(state, many ? 'Browser copies discarded.' : 'Browser copy discarded.');
          } catch {
            offerRecovery(state);
            setStatus(state, many ? 'Some browser copies could not be discarded. Try again.' : 'Browser copy could not be discarded. Try again.', true);
          }
        });
      });
    });
  }

  function allowLeave(candidates, proceed, decline) {
    const changed = [...candidates].filter(state => active(state) && dirty(state) && state.approved !== encoded(state));
    changed.forEach(persist);
    if (!changed.length) return true;
    const snapshots = changed.map(state => encoded(state));
    window.blogConfirm({title: 'Leave without saving?', message: 'Your edits have not been saved to the server. Any available recovery copy will stay in this browser.', action: 'Leave without saving', cancel: 'Keep editing'}, () => {
      if (changed.some((state, index) => !active(state) || encoded(state) !== snapshots[index])) { decline?.(); return; }
      changed.forEach((state, index) => { state.approved = snapshots[index]; });
      proceed();
    }, decline);
    return false;
  }

  document.addEventListener('click', event => {
    const link = event.target.closest('a[href]');
    // Chooser pagination and filtering replace only the image results, not an editor.
    if (link?.matches('.media-browser [data-media-page]')) return;
    if (!link || link.hasAttribute('download') || link.target === '_blank' || event.ctrlKey || event.metaKey || event.shiftKey || link.getAttribute('href').startsWith('#')) return;
    // The album viewer opens over the editor and preserves its unsaved fields.
    if (link.matches('[data-album-editor] [data-album-photo]') && typeof HTMLDialogElement !== 'undefined' && event.button === 0 && !event.altKey) return;
    if (!allowLeave(states.values(), () => { if (link.isConnected) link.click(); })) {
      event.preventDefault();
      event.stopImmediatePropagation();
    }
  }, true);
  document.addEventListener('submit', event => {
    if (event.target.matches('.media-browser [data-media-filters]')) return;
    const state = states.get(event.target);
    if (state) {
      persist(state);
      state.approved = null;
    } else if (!allowLeave(states.values(), () => { if (event.target.isConnected) event.target.requestSubmit(event.submitter ?? undefined); })) {
      event.preventDefault();
      event.stopImmediatePropagation();
    }
  }, true);
  // Only an uncanceled ordinary submission can leave before the server responds.
  // HTMX saves and upload-blocked submissions must retain the unsaved-edit warning.
  window.addEventListener('submit', event => {
    const state = states.get(event.target);
    if (state && !event.defaultPrevented) state.approved = encoded(state);
  });
  const departures = new WeakMap();
  document.addEventListener('htmx:config:request', event => {
    const {ctx} = event.detail;
    // Browser traversal has already changed the URL. Preserve recovery copies
    // during cleanup, without stopping the history restoration midway.
    if (ctx.request.headers['HX-History-Restore-Request'] === 'true') return;
    const candidates = [...states.values()].filter(state => ctx.target?.contains(state.form) && active(state) && dirty(state));
    if (candidates.length) {
      departures.set(ctx, candidates);
      ctx.confirm = true;
    }
  });
  document.addEventListener('htmx:confirm', event => {
    const {ctx, issueRequest, dropRequest} = event.detail;
    const candidates = departures.get(ctx);
    if (!candidates) return;
    event.preventDefault();
    if (allowLeave(candidates, issueRequest, dropRequest)) issueRequest();
  });
  window.addEventListener('beforeunload', event => {
    const changed = [...states.values()].filter(state => active(state) && dirty(state));
    changed.forEach(persist);
    if (changed.some(state => state.approved !== encoded(state))) {
      event.preventDefault();
      event.returnValue = '';
    }
  });
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) states.forEach(persist);
  });
  document.addEventListener('htmx:before:history:update', () => states.forEach(persist));
  document.addEventListener('htmx:before:cleanup', event => {
    const state = states.get(event.target);
    if (state) {
      persist(state);
      states.delete(state.form);
    }
  });
  const submissions = new WeakMap();
  document.addEventListener('htmx:before:request', event => {
    const {ctx} = event.detail;
    const state = states.get(ctx.sourceElement);
    if (state) {
      const submission = {baseline: encoded(state)};
      try { submission.copies = copiesBeforeSave(state); }
      catch { /* Saving can proceed when browser storage is unavailable. */ }
      submissions.set(ctx, submission);
      persist(state);
    }
  });
  // HTMX 4 delivers HX-Trigger events after starting redirects. Read the commit
  // confirmation from the response first so a successful save can leave safely.
  document.addEventListener('htmx:before:response', event => {
    const {ctx} = event.detail;
    if (!ctx.response.raw.ok || ctx.response.headers.get('HX-Trigger') !== 'blog:content-saved') return;
    const state = states.get(ctx.sourceElement);
    const submitted = submissions.get(ctx);
    if (!state || !submitted) return;
    state.baseline = submitted.baseline;
    publishBaseline(state);
    persist(state);
    try {
      if (!submitted.copies) throw new Error('Recovery storage is unavailable');
      clearSavedCopies(state, submitted.copies);
    } catch { setStatus(state, 'Older recovery copies could not be cleared.', true); }
  });
  document.addEventListener('htmx:finally:request', () => states.forEach(state => { state.approved = null; }));
  document.addEventListener('DOMContentLoaded', initialize);
  document.addEventListener('htmx:after:process', initialize);
})();
