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
    (entry.description === undefined || typeof entry.description === 'string' && [...entry.description].length <= 1000) &&
    (entry.hideDownload === undefined || typeof entry.hideDownload === 'boolean') && Number.isSafeInteger(entry.size) && entry.size >= 0 && entry.size <= 33554432;
}

function initialize(root) {
  root.querySelectorAll('[data-downloads-editor]').forEach(section => {
    if (initialized.has(section)) return;
    initialized.add(section);
    const source = section.querySelector('textarea[name]');
    const maxFiles = Number(section.dataset.maxFiles);
    const pdfOnly = section.dataset.postComponent === 'pdf-preview';
    const accepts = entry => validEntry(entry) && (!pdfOnly || entry.id.endsWith('.pdf'));
    const limitMessage = pdfOnly ? 'Choose one PDF for the preview.' : 'Choose at most 20 files per post.';
    const form = source.form;
    const list = section.querySelector('[data-downloads-list]');
    const status = section.querySelector('[data-downloads-status]');
    const recovery = section.querySelector('[data-downloads-recovery]');
    const pdfRow = section.querySelector('[data-pdf-document-row]');
    let entries = [];
    let removed = [];

    function serialize() {
      source.value = entries.length ? JSON.stringify(entries) : '';
      source.dispatchEvent(new Event('input', {bubbles: true}));
    }

    function render() {
      const busy = Boolean(form.dataset.uploading);
      const count = section.querySelector('[data-downloads-count]');
      if (count) count.textContent = entries.length ? `${entries.length} ${entries.length === 1 ? 'file' : 'files'}` : '';
      section.querySelector('[data-download-upload]').disabled = busy || !pdfOnly && entries.length >= maxFiles;
      section.querySelector('[data-download-browse]').disabled = busy || !pdfOnly && entries.length >= maxFiles;
      if (pdfOnly) {
        pdfRow.hidden = false;
        const metadata = pdfRow.querySelector('[data-pdf-document]');
        metadata.hidden = !entries.length;
        section.querySelector('[data-download-upload-label]').textContent = entries.length ? 'Replace PDF' : 'Upload PDF';
        if (entries.length) {
          metadata.querySelector('a').href = '/files/' + entries[0].id + '?preview=1';
          metadata.querySelector('[data-pdf-size]').textContent = sizeLabel(entries[0].size);
        }
      }
      section.querySelector('[data-component-remove]').disabled = busy;
      recovery.hidden = !removed.length;
      recovery.querySelector('span').textContent = pdfOnly ? 'PDF replaced.' : `${removed.length} ${removed.length === 1 ? 'file' : 'files'} removed.`;
      recovery.querySelector('button').disabled = busy || !pdfOnly && entries.length >= maxFiles;
      list.replaceChildren(...entries.map((entry, index) => {
        const item = element(pdfOnly ? 'div' : 'li');
        const fields = element('div', '', 'download-fields');
        const label = element('label', 'Display name');
        const name = element('input');
        name.value = entry.label;
        name.maxLength = 240;
        name.id = source.name + '-name-' + entry.id;
        name.pattern = '.*\\S.*';
        name.required = true;
        name.setAttribute('aria-label', 'Name for ' + entry.label);
        label.append(name);
        const detail = element('label', 'Description (optional)');
        const description = element('textarea');
        description.value = entry.description || '';
        description.rows = pdfOnly ? 2 : 1;
        description.maxLength = 1000;
        detail.append(description);
        const update = () => { entry.label = name.value; entry.description = description.value; serialize(); };
        name.addEventListener('input', () => { markField(name, false); update(); });
        name.addEventListener('keydown', event => { if (event.key === 'Enter' && !event.isComposing) { event.preventDefault(); name.blur(); } });
        description.addEventListener('input', update);
        fields.append(label, detail);
        if (pdfOnly) {
          const option = element('label', '', 'pdf-download-option');
          const showDownload = element('input');
          showDownload.type = 'checkbox';
          showDownload.checked = !entry.hideDownload;
          showDownload.addEventListener('change', () => {
            if (showDownload.checked) delete entry.hideDownload;
            else entry.hideDownload = true;
            serialize();
          });
          option.append(showDownload, document.createTextNode('Show download link'));
          fields.append(option);
        }
        if (pdfOnly) {
          item.append(fields);
          return item;
        }
        const badge = element('a', entry.id.split('.').pop().toUpperCase(), 'download-type');
        badge.href = '/files/' + entry.id;
        badge.setAttribute('hx-boost', 'false');
        badge.setAttribute('download', '');
        badge.setAttribute('aria-label', 'Download ' + entry.label);
        fields.append(element('small', sizeLabel(entry.size), 'form-hint'));
        const actions = element('div', '', 'download-actions');
        for (const [direction, delta, text] of (maxFiles > 1 ? [['up', -1, '↑'], ['down', 1, '↓']] : [])) {
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
        if (!Array.isArray(next) || next.length > maxFiles || !next.every(accepts) || new Set(next.map(entry => entry.id)).size !== next.length) throw new Error();
        entries = next;
      } catch {
        source.hidden = false;
        section.querySelector('[data-downloads-source-label]').hidden = false;
        section.querySelector('.downloads-controls').hidden = true;
        if (pdfRow) pdfRow.hidden = true;
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
      if (!Array.isArray(values) || !values.every(accepts)) throw new Error('Invalid file response. Try again.');
      const next = values.filter(entry => !entries.some(current => current.id === entry.id));
      if (pdfOnly) {
        if (next.length > 1) throw new Error(limitMessage);
        if (!next.length) return;
        const previous = entries[0];
        removed = previous ? [{entry: previous, index: 0}] : [];
        entries = [previous ? {...previous, id: next[0].id, size: next[0].size} : next[0]];
      } else {
        if (entries.length + next.length > maxFiles) throw new Error(limitMessage);
        entries.push(...next);
        removed = removed.filter(value => !entries.some(entry => entry.id === value.entry.id));
      }
      serialize(); render();
    }

    async function upload(files) {
      if (!section.isConnected || form.dataset.uploading) return;
      if (files.length + (pdfOnly ? 0 : entries.length) > maxFiles) { setMediaStatus(status, limitMessage, true); return; }
      await runFormUpload(form, async () => {
        const failed = [];
        let lastError;
        for (let index = 0; index < files.length; index++) {
          if (!section.isConnected) return;
          setMediaStatus(status, `Uploading ${index + 1} of ${files.length}: ${files[index].name}`);
          try {
            if (pdfOnly && !files[index].name.toLowerCase().endsWith('.pdf')) throw new Error('Choose a PDF file.');
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
        if (!failed.length) { setMediaStatus(status, pdfOnly ? 'PDF ready. Save the post to apply changes.' : 'Files added. Save the post to apply changes.'); return; }
        const retry = () => upload(failed);
        showUploadFailure(status, lastError, retry, pdfOnly ? 'Your edits are preserved.' : 'Files already added and your edits are preserved.');
        status.append(element('p', 'Not added: ' + failed.map(file => file.name).join(', ')));
        if (!status.querySelector('[data-upload-retry]')) {
          const retryButton = button(pdfOnly ? 'Retry upload' : 'Retry remaining uploads');
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
    section.querySelector('[data-download-browse]').addEventListener('click', () => openLibrary(entries, add, status, form, {maxFiles, pdfOnly}));
    section.querySelector('[data-download-undo]').addEventListener('click', () => {
      if (form.dataset.uploading || !removed.length || !pdfOnly && entries.length >= maxFiles) return;
      const value = removed.pop();
      if (pdfOnly) entries = [value.entry];
      else entries.splice(Math.min(value.index, entries.length), 0, value.entry);
      serialize(); render();
      if (pdfOnly) list.querySelector('input')?.focus();
      setMediaStatus(status, pdfOnly ? 'PDF restored. Save the post to apply changes.' : 'File restored. Save the post to apply changes.');
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

async function openLibrary(existing, add, status, form, {maxFiles, pdfOnly}) {
  if (document.querySelector('.download-library')) return;
  const opener = document.activeElement;
  const replacing = pdfOnly && existing.length > 0;
  const dialog = element('dialog', '', 'download-library');
  dialog.setAttribute('aria-labelledby', 'download-library-title');
  const title = element('h2', pdfOnly ? (replacing ? 'Choose a replacement PDF' : 'Choose a PDF') : 'Choose files');
  title.id = 'download-library-title';
  const close = button('Cancel');
  const pdfAction = replacing ? 'Replace PDF' : 'Use PDF';
  const confirm = button(pdfOnly ? pdfAction : 'Add files');
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
  const room = pdfOnly ? 1 : maxFiles - existing.length;
  document.addEventListener('htmx:before:cleanup', event => { if (event.target.contains?.(form)) dialog.close(); }, {signal: controller.signal});
  document.addEventListener('htmx:after:swap', () => { if (!form.isConnected) dialog.close(); }, {signal: controller.signal});
  close.addEventListener('click', () => dialog.close());
  dialog.addEventListener('close', () => { controller.abort(); dialog.remove(); if (opener?.isConnected) opener.focus(); }, {once: true});
  confirm.addEventListener('click', () => {
    if (!form.isConnected || form.dataset.uploading) return;
    try { add([...selected.values()]); dialog.close(); setMediaStatus(status, pdfOnly ? 'PDF ready. Save the post to apply changes.' : 'Files added. Save the post to apply changes.'); }
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
    const choices = (files || []).filter(entry => !pdfOnly || entry.id.endsWith('.pdf'));
    feedback.textContent = choices.length ? (pdfOnly ? 'Choose a PDF to embed.' : 'Choose files to offer as downloads.') : (pdfOnly ? 'No uploaded PDFs. Upload a PDF from the post editor.' : 'No uploaded files. Upload files from the post editor.');
    for (const entry of choices) {
      const row = element('li');
      const label = element('label');
      const check = element('input');
      check.type = pdfOnly ? 'radio' : 'checkbox';
      check.name = 'download-library-choice';
      check.disabled = existing.some(current => current.id === entry.id);
      const name = element('span', entry.label);
      name.append(element('small', check.disabled ? (pdfOnly ? 'Current PDF' : 'Added to this post') : entry.id.split('.').pop().toUpperCase() + ' · ' + sizeLabel(entry.size)));
      label.append(check, name);
      row.append(label);
      list.append(row);
      check.addEventListener('change', () => {
        if (pdfOnly) selected.clear();
        if (check.checked && selected.size >= room) { check.checked = false; feedback.textContent = 'Post limit reached. Deselect a file to choose another.'; return; }
        if (check.checked) selected.set(entry.id, {...entry}); else selected.delete(entry.id);
        confirm.disabled = !selected.size;
        confirm.textContent = pdfOnly ? pdfAction : selected.size ? `Add ${selected.size} ${selected.size === 1 ? 'file' : 'files'}` : 'Add files';
        feedback.textContent = `${selected.size} ${selected.size === 1 ? 'file' : 'files'} selected`;
      });
    }
  } catch (error) {
    if (error.name !== 'AbortError' && dialog.isConnected) showUploadFailure(feedback, error, () => { dialog.close(); openLibrary(existing, add, status, form, {maxFiles, pdfOnly}); }, 'Your edits are unchanged.');
  }
}

document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
