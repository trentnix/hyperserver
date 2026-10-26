// Recovery copies belong to this browser, not the published content. Each editor
// instance owns a key so separate tabs never overwrite one another's writing.
(() => {
  const states = new Map();
  const prefix = 'hyperserver.blog.draft.v1:';
  const lifetime = 7 * 24 * 60 * 60 * 1000;
  const maxLength = 1024 * 1024;

  function snapshot(state) {
    return Object.fromEntries(state.fields.map(field => [field.name, field.type === 'checkbox' ? field.checked : field.value]));
  }
  const encoded = state => JSON.stringify(snapshot(state));
  const dirty = state => encoded(state) !== state.baseline;
  const active = state => state.form.isConnected && (!state.form.closest('dialog') || state.form.closest('dialog').open);

  function setStatus(state, message, failed = false) {
    if (state.status.textContent !== message) state.status.textContent = message;
    state.status.classList.toggle('draft-status--error', failed);
  }

  function storageFailure(state) {
    setStatus(state, 'Recovery copy could not be saved. Keep this editor open and save your changes.', true);
  }

  // Only remove the exact copy we read. Another tab might still be editing it.
  function removeCopy(copy) {
    if (localStorage.getItem(copy.key) === copy.raw) localStorage.removeItem(copy.key);
  }

  function persist(state) {
    clearTimeout(state.timer);
    try {
      if (!dirty(state)) {
        localStorage.removeItem(state.key);
        setStatus(state, 'No unsaved changes.');
        return;
      }
      const raw = JSON.stringify({saved: Date.now(), baseline: state.baseline, fields: snapshot(state)});
      if (raw.length > maxLength) throw new Error('Recovery copy too large');
      localStorage.setItem(state.key, raw);
      setStatus(state, 'Saved locally. Use Save to apply changes.');
    } catch {
      storageFailure(state);
    }
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
        if (typeof data.baseline !== 'string' || !data.fields || !state.fields.every(field =>
          field.type === 'checkbox' ? data.fields[field.name] === undefined || typeof data.fields[field.name] === 'boolean' : typeof data.fields[field.name] === 'string')) continue;
        copies.push({key, raw, data});
      }
      copies.sort((a, b) => b.data.saved - a.data.saved);
      state.recovery = copies[0];
      state.form.querySelector('[data-draft-recovery]').hidden = !state.recovery;
      if (state.recovery) {
        const data = state.recovery.data;
        state.form.querySelector('[data-draft-description]').textContent =
          'An unsaved copy from ' + new Date(data.saved).toLocaleString() + ' is available.' +
          (data.baseline !== state.baseline ? ' The server version or initial settings have changed. Restoring replaces the fields in this editor only.' : '');
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
        fields: [...form.querySelectorAll('input[name], textarea[name], select[name]')].filter(field => field.type !== 'file'),
        status: form.querySelector('[data-draft-status]')
      };
      state.baseline = encoded(state);
      states.set(form, state);
      state.status.hidden = false;
      offerRecovery(state);
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
      form.querySelector('[data-draft-restore]').addEventListener('click', () => {
        if (!state.recovery || (dirty(state) && !confirm('Replace the current edits with this recovery copy?'))) return;
        const copy = state.recovery;
        for (const field of state.fields) {
          // A checkbox added after this copy was saved keeps its current setting.
          if (!Object.hasOwn(copy.data.fields, field.name)) continue;
          if (field.type === 'checkbox') field.checked = copy.data.fields[field.name];
          else field.value = copy.data.fields[field.name];
        }
        const editor = form.querySelector('.CodeMirror')?.CodeMirror;
        if (editor) editor.setValue(form.elements.content.value);
        form.querySelectorAll('[data-image-value]').forEach(field => field.dispatchEvent(new Event('change', {bubbles: true})));
        state.sources.push(copy);
        form.dispatchEvent(new Event('input', {bubbles: true}));
        persist(state);
        offerRecovery(state);
      });
      form.querySelector('[data-draft-discard]').addEventListener('click', () => {
        if (!state.recovery || !confirm('Permanently discard this recovery copy?')) return;
        try {
          removeCopy(state.recovery);
          offerRecovery(state);
        } catch { storageFailure(state); }
      });
    });
  }

  function allowLeave(candidates) {
    const changed = [...candidates].filter(state => active(state) && dirty(state) && state.approved !== encoded(state));
    changed.forEach(persist);
    if (changed.length && !confirm('Leave without saving your edits to the server? Any available recovery copy will stay in this browser.')) return false;
    changed.forEach(state => { state.approved = encoded(state); });
    return true;
  }

  document.addEventListener('click', event => {
    const close = event.target.closest('[data-close-dialog]');
    const link = event.target.closest('a[href]');
    if (!close && (!link || link.hasAttribute('download') || link.target === '_blank' || event.ctrlKey || event.metaKey || event.shiftKey || link.getAttribute('href').startsWith('#'))) return;
    const candidates = close ? [...states.values()].filter(state => close.closest('dialog').contains(state.form)) : states.values();
    if (!allowLeave(candidates)) {
      event.preventDefault();
      event.stopImmediatePropagation();
    }
  }, true);
  document.addEventListener('cancel', event => {
    if (!allowLeave([...states.values()].filter(state => event.target.contains(state.form)))) event.preventDefault();
  }, true);
  document.addEventListener('submit', event => {
    const state = states.get(event.target);
    if (state) {
      persist(state);
      state.approved = null;
    } else if (!allowLeave(states.values())) {
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
  document.addEventListener('htmx:confirm', event => {
    const target = event.detail.target;
    if (target && !allowLeave([...states.values()].filter(state => target.contains(state.form)))) event.preventDefault();
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
  document.addEventListener('htmx:beforeHistorySave', () => states.forEach(persist));
  document.addEventListener('htmx:beforeCleanupElement', event => {
    const state = states.get(event.detail.elt);
    if (state) {
      persist(state);
      states.delete(state.form);
    }
  });
  document.addEventListener('htmx:beforeRequest', event => {
    const state = states.get(event.detail.elt);
    if (state) {
      state.submitted = encoded(state);
      persist(state);
    }
  });
  // This signal is sent only after storage commits, before HTMX follows its redirect.
  document.addEventListener('blog:content-saved', event => {
    const state = states.get(event.target);
    if (!state || !state.submitted) return;
    state.baseline = state.submitted;
    try {
      state.sources.forEach(removeCopy);
      state.sources = [];
    } catch { storageFailure(state); }
    persist(state);
  });
  document.addEventListener('htmx:afterRequest', () => states.forEach(state => { state.approved = null; }));
  document.addEventListener('DOMContentLoaded', initialize);
  document.addEventListener('htmx:load', initialize);
})();
