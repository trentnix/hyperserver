import {runFormUpload, setMediaStatus, showUploadFailure} from './media.js';
import {markField} from './forms.js';
import {registerComponent} from './post-components.js';

const initialized = new WeakSet();
const validID = /^[a-f0-9]{64}\.(pdf|zip|txt)$/;
const sizeLabel = size => size >= 1048576 ? (size / 1048576).toFixed(1) + ' MiB' : size >= 1024 ? (size / 1024).toFixed(1) + ' KiB' : size + ' B';

function element(tag, text, className) {
  const node = document.createElement(tag);
  if (text) node.textContent = text;
  if (className) node.className = className;
  return node;
}

function button(text, label) {
  const node = element('button', text, 'text-button');
  node.type = 'button';
  if (label) node.setAttribute('aria-label', label);
  return node;
}

async function requestFiles(path, options) {
  const response = await fetch(path, {...options, redirect: 'error'});
  if (response.status === 401) throw Object.assign(new Error('Sign in again to upload or choose files.'), {code: 'authentication_required'});
  if (!(response.headers.get('Content-Type') || '').includes('application/json')) throw new Error('Unable to load files. Try again.');
  const result = await response.json();
  if (!response.ok) throw Object.assign(new Error(result?.error || 'Unable to load files.'), {code: result?.code});
  return result;
}

function validEntry(entry) {
  return entry && validID.test(entry.id) && typeof entry.label === 'string' && entry.label.trim() && [...entry.label].length <= 240 &&
    (!entry.description || typeof entry.description === 'string' && [...entry.description].length <= 1000) && Number.isSafeInteger(entry.size) && entry.size >= 0 && entry.size <= 33554432;
}

function initialize(root) {
  root.querySelectorAll('[data-downloads-editor]').forEach(section => {
    if (initialized.has(section)) return;
    initialized.add(section);
    const source = section.querySelector('[name="downloads"]');
    const form = source.form;
    const list = section.querySelector('[data-downloads-list]');
    const status = section.querySelector('[data-downloads-status]');
    const recovery = section.querySelector('[data-downloads-recovery]');
    let entries = [];
    let removed = [];

    function serialize() {
      source.value = entries.length ? JSON.stringify(entries) : '';
      source.dispatchEvent(new Event('input', {bubbles: true}));
    }

    function render() {
      const busy = Boolean(form.dataset.uploading);
      section.querySelector('[data-downloads-count]').textContent = entries.length ? `${entries.length} ${entries.length === 1 ? 'file' : 'files'}` : '';
      section.querySelector('[data-download-upload]').disabled = busy || entries.length >= 20;
      section.querySelector('[data-download-browse]').disabled = busy || entries.length >= 20;
      section.querySelector('[data-component-remove]').disabled = busy;
      recovery.hidden = !removed.length;
      recovery.querySelector('span').textContent = `${removed.length} ${removed.length === 1 ? 'file' : 'files'} removed.`;
      recovery.querySelector('button').disabled = busy || entries.length >= 20;
      list.replaceChildren(...entries.map((entry, index) => {
        const item = element('li');
        const type = entry.id.split('.').pop().toUpperCase();
        const badge = element('a', type, 'download-type');
        badge.href = '/files/' + entry.id;
        badge.setAttribute('hx-boost', 'false');
        badge.setAttribute('download', '');
        badge.setAttribute('aria-label', 'Download ' + entry.label);
        const fields = element('div', '', 'download-fields');
        const label = element('label', 'Display name');
        const name = element('input');
        name.value = entry.label;
        name.maxLength = 240;
        name.id = 'download-name-' + entry.id;
        name.pattern = '.*\\S.*';
        name.required = true;
        name.setAttribute('aria-label', 'Name for ' + entry.label);
        label.append(name);
        const detail = element('label', 'Description (optional)');
        const description = element('textarea');
        description.value = entry.description || '';
        description.rows = 1;
        description.maxLength = 1000;
        detail.append(description);
        const update = () => { entry.label = name.value; entry.description = description.value; serialize(); };
        name.addEventListener('input', () => { markField(name, false); update(); });
        name.addEventListener('keydown', event => { if (event.key === 'Enter' && !event.isComposing) { event.preventDefault(); name.blur(); } });
        description.addEventListener('input', update);
        fields.append(label, detail, element('small', sizeLabel(entry.size), 'form-hint'));
        const actions = element('div', '', 'download-actions');
        for (const [direction, delta, text] of [['up', -1, '↑'], ['down', 1, '↓']]) {
          const move = button(text, `Move ${entry.label} ${direction}`);
          move.disabled = busy || index + delta < 0 || index + delta >= entries.length;
          move.addEventListener('click', () => {
            const target = index + delta;
            [entries[index], entries[target]] = [entries[target], entries[index]];
            serialize(); render();
            (list.children[target].querySelector(`button[data-direction="${direction}"]:not(:disabled)`) || list.children[target].querySelector('input')).focus();
          });
          move.dataset.direction = direction;
          actions.append(move);
        }
        const remove = button('×', 'Remove ' + entry.label);
        remove.disabled = busy;
        remove.addEventListener('click', () => {
          removed.push({entry: entries.splice(index, 1)[0], index});
          serialize(); render();
          const next = list.children[Math.min(index, entries.length - 1)];
          (next?.querySelector('input') || section.querySelector('[data-download-upload]')).focus();
          setMediaStatus(status, 'File removed. Save the post to apply changes.');
        });
        actions.append(remove);
        item.append(badge, fields, actions);
        return item;
      }));
    }

    function read() {
      try {
        const next = source.value.trim() ? JSON.parse(source.value) : [];
        if (!Array.isArray(next) || next.length > 20 || !next.every(validEntry) || new Set(next.map(entry => entry.id)).size !== next.length) throw new Error();
        entries = next;
      } catch {
        source.hidden = false;
        section.querySelector('[data-downloads-source-label]').hidden = false;
        section.querySelector('.downloads-controls').hidden = true;
        list.replaceChildren();
        setMediaStatus(status, 'Some file entries are invalid. Correct the source before adding files.', true);
        return;
      }
      source.hidden = true;
      section.querySelector('[data-downloads-source-label]').hidden = true;
      section.querySelector('.downloads-controls').hidden = false;
      removed = [];
      render();
      setMediaStatus(status, '');
    }

    function add(values) {
      if (!Array.isArray(values) || !values.every(validEntry)) throw new Error('Invalid file response. Try again.');
      const next = values.filter(entry => !entries.some(current => current.id === entry.id));
      if (entries.length + next.length > 20) throw new Error('Choose at most 20 files per post.');
      entries.push(...next);
      removed = removed.filter(value => !entries.some(entry => entry.id === value.entry.id));
      serialize(); render();
    }

    async function upload(files) {
      if (!section.isConnected || form.dataset.uploading) return;
      if (files.length + entries.length > 20) { setMediaStatus(status, 'Choose fewer files. Posts can offer up to 20 downloads.', true); return; }
      await runFormUpload(form, async () => {
        const failed = [];
        let lastError;
        for (let index = 0; index < files.length; index++) {
          if (!section.isConnected) return;
          setMediaStatus(status, `Uploading ${index + 1} of ${files.length}: ${files[index].name}`);
          try {
            if (files[index].size > 33554432) throw new Error('Choose a file of at most 32 MiB.');
            const data = new FormData();
            data.append('file', files[index]);
            const entry = await requestFiles('/post/files/upload', {method: 'POST', body: data});
            if (!section.isConnected) return;
            add([entry]);
          } catch (error) {
            lastError = error;
            failed.push(files[index]);
            if (['authentication_required', 'verification_required', 'access_denied'].includes(error.code)) {
              failed.push(...files.slice(index + 1));
              break;
            }
          }
        }
        if (!failed.length) { setMediaStatus(status, 'Files added. Save the post to apply changes.'); return; }
        const retry = () => upload(failed);
        showUploadFailure(status, lastError, retry, 'Files already added and your edits are preserved.');
        status.append(element('p', 'Not added: ' + failed.map(file => file.name).join(', ')));
        if (!status.querySelector('[data-upload-retry]')) {
          const retryButton = button('Retry remaining uploads');
          retryButton.dataset.uploadRetry = '';
          retryButton.addEventListener('click', retry);
          status.append(retryButton);
        }
      });
    }

    section.querySelector('[data-download-upload]').addEventListener('click', () => section.querySelector('[data-download-files]').click());
    section.querySelector('[data-download-files]').addEventListener('change', event => {
      const files = [...event.target.files];
      event.target.value = '';
      if (files.length) upload(files);
    });
    section.querySelector('[data-download-browse]').addEventListener('click', () => openLibrary(entries, add, status, form));
    section.querySelector('[data-download-undo]').addEventListener('click', () => {
      if (form.dataset.uploading || !removed.length || entries.length >= 20) return;
      const value = removed.pop();
      entries.splice(Math.min(value.index, entries.length), 0, value.entry);
      serialize(); render();
      setMediaStatus(status, 'File restored. Save the post to apply changes.');
    });
    form.addEventListener('blog:uploads-finished', render);
    source.addEventListener('change', read);
    registerComponent(section, {
      snapshot: () => ({source: source.value, removed: [...removed]}),
      clear: () => { source.value = ''; read(); },
      restore: copy => { source.value = copy.source; read(); removed = copy.removed; render(); },
      focus: () => (source.hidden ? section.querySelector('[data-download-upload]') : source).focus()
    });
    read();
  });
}

async function openLibrary(existing, add, status, form) {
  if (document.querySelector('.download-library')) return;
  const opener = document.activeElement;
  const dialog = element('dialog', '', 'download-library');
  dialog.setAttribute('aria-labelledby', 'download-library-title');
  const title = element('h2', 'Choose files');
  title.id = 'download-library-title';
  const close = button('Cancel');
  const confirm = button('Add files');
  confirm.disabled = true;
  const searchLabel = element('label', 'Search files');
  const search = element('input');
  search.type = 'search';
  searchLabel.append(search);
  const feedback = element('p', 'Loading files…', 'form-hint');
  feedback.setAttribute('role', 'status');
  const list = element('ul', '', 'download-library-list');
  const actions = element('div', '', 'form-actions');
  actions.append(close, confirm);
  dialog.append(title, searchLabel, feedback, list, actions);
  document.body.append(dialog);
  const controller = new AbortController();
  const selected = new Map();
  const room = 20 - existing.length;
  document.addEventListener('htmx:before:cleanup', event => { if (event.target.contains?.(form)) dialog.close(); }, {signal: controller.signal});
  document.addEventListener('htmx:after:swap', () => { if (!form.isConnected) dialog.close(); }, {signal: controller.signal});
  close.addEventListener('click', () => dialog.close());
  dialog.addEventListener('close', () => { controller.abort(); dialog.remove(); if (opener?.isConnected) opener.focus(); }, {once: true});
  confirm.addEventListener('click', () => {
    if (!form.isConnected || form.dataset.uploading) return;
    try { add([...selected.values()]); dialog.close(); setMediaStatus(status, 'Files added. Save the post to apply changes.'); }
    catch (error) { feedback.textContent = error.message; }
  });
  search.addEventListener('input', () => {
    for (const row of list.children) row.hidden = !row.textContent.toLowerCase().includes(search.value.trim().toLowerCase());
  });
  dialog.showModal();
  search.focus();
  try {
    const files = await requestFiles('/post/files', {signal: controller.signal});
    if (!dialog.isConnected) return;
    if (files !== null && (!Array.isArray(files) || !files.every(validEntry))) throw new Error('Unable to read files.');
    feedback.textContent = files?.length ? 'Choose files to offer as downloads.' : 'No uploaded files. Upload files from the post editor.';
    for (const entry of files || []) {
      const row = element('li');
      const label = element('label');
      const check = element('input');
      check.type = 'checkbox';
      check.disabled = existing.some(current => current.id === entry.id);
      const name = element('span', entry.label);
      name.append(element('small', check.disabled ? 'Added to this post' : entry.id.split('.').pop().toUpperCase() + ' · ' + sizeLabel(entry.size)));
      label.append(check, name);
      row.append(label);
      list.append(row);
      check.addEventListener('change', () => {
        if (check.checked && selected.size >= room) { check.checked = false; feedback.textContent = 'Post limit reached. Deselect a file to choose another.'; return; }
        if (check.checked) selected.set(entry.id, {...entry}); else selected.delete(entry.id);
        confirm.disabled = !selected.size;
        confirm.textContent = selected.size ? `Add ${selected.size} ${selected.size === 1 ? 'file' : 'files'}` : 'Add files';
        feedback.textContent = `${selected.size} ${selected.size === 1 ? 'file' : 'files'} selected`;
      });
    }
  } catch (error) {
    if (error.name !== 'AbortError' && dialog.isConnected) showUploadFailure(feedback, error, () => { dialog.close(); openLibrary(existing, add, status, form); }, 'Your edits are unchanged.');
  }
}

document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
