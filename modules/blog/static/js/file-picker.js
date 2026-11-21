import {showUploadFailure, setMediaStatus, updateUploadStorage} from './media.js';

const validID = /^[a-f0-9]{64}\.(pdf|zip|txt|mp3)$/;
export const sizeLabel = size => size >= 1048576 ? (size / 1048576).toFixed(1) + ' MiB' : size >= 1024 ? (size / 1024).toFixed(1) + ' KiB' : size + ' B';

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

export async function requestFiles(path, options, form) {
  const response = await fetch(path, {...options, redirect: 'error'});
  if (form) updateUploadStorage(form, response);
  if (response.status === 401) throw Object.assign(new Error('Sign in again to upload or choose files.'), {code: 'authentication_required'});
  if (!(response.headers.get('Content-Type') || '').includes('application/json')) throw new Error('Unable to load files. Try again.');
  const result = await response.json();
  if (!response.ok) throw Object.assign(new Error(result?.error || 'Unable to load files.'), {code: result?.code});
  return result;
}

export function validEntry(entry) {
  return entry && validID.test(entry.id) && typeof entry.label === 'string' && entry.label.trim() && [...entry.label].length <= 240 &&
    (entry.description === undefined || typeof entry.description === 'string' && [...entry.description].length <= 1000) &&
    (entry.hideDownload === undefined || typeof entry.hideDownload === 'boolean') && Number.isSafeInteger(entry.size) && entry.size >= 0 && entry.size <= 33554432;
}

export async function openFileLibrary(existing, add, status, form, {maxFiles, extension}) {
  if (document.querySelector('.download-library')) return;
  const single = Boolean(extension);
  const kind = extension === 'mp3' ? 'MP3' : 'PDF';
  const opener = document.activeElement;
  const replacing = single && existing.length > 0;
  const dialog = element('dialog', '', 'download-library');
  dialog.setAttribute('aria-labelledby', 'download-library-title');
  const title = element('h2', single ? (replacing ? `Choose a replacement ${kind}` : `Choose ${kind === 'MP3' ? 'an' : 'a'} ${kind}`) : 'Choose files');
  title.id = 'download-library-title';
  const close = button('Cancel');
  const singleAction = replacing ? `Replace ${kind}` : `Use ${kind}`;
  const confirm = button(single ? singleAction : 'Add files');
  confirm.disabled = true;
  const searchLabel = element('label', 'Search files');
  const search = element('input');
  search.type = 'search';
  searchLabel.append(search);
  const feedback = element('p', '', 'form-hint');
  feedback.setAttribute('role', 'status');
  const list = element('ul', '', 'download-library-list');
  const actions = element('footer', '', 'form-actions');
  actions.append(close, confirm);
  const body = element('div', '', 'download-library-body');
  const fields = element('div', '', 'download-library-fields');
  fields.append(searchLabel, feedback, list);
  body.append(fields);
  dialog.append(title, body, actions);
  document.body.append(dialog);
  const controller = new AbortController();
  const selected = new Map();
  const selectionSummary = () => selected.size ? `${selected.size} ${selected.size === 1 ? 'file' : 'files'} selected` : '';
  const room = single ? 1 : maxFiles - existing.length;
  document.addEventListener('htmx:before:cleanup', event => { if (event.target.contains?.(form)) dialog.close(); }, {signal: controller.signal});
  document.addEventListener('htmx:after:swap', () => { if (!form.isConnected) dialog.close(); }, {signal: controller.signal});
  close.addEventListener('click', () => dialog.close());
  dialog.addEventListener('close', () => { controller.abort(); dialog.remove(); if (opener?.isConnected) opener.focus(); }, {once: true});
  confirm.addEventListener('click', () => {
    if (!form.isConnected || form.dataset.uploading) return;
    try { add([...selected.values()]); dialog.close(); setMediaStatus(status, single ? `${kind} selected.` : 'Files added. Save to apply changes.'); }
    catch (error) { feedback.textContent = error.message; }
  });
  function filterChoices() {
    const query = search.value.trim().toLowerCase();
    let hasMatches = false;
    for (const row of list.children) {
      row.hidden = !row.textContent.toLowerCase().includes(query);
      hasMatches ||= !row.hidden;
    }
    if (list.childElementCount) feedback.textContent = hasMatches ? selectionSummary() : 'No matching files.';
  }
  search.addEventListener('input', filterChoices);
  dialog.showModal();
  search.focus({preventScroll: true});
  await load();

  async function load() {
    setMediaStatus(feedback, 'Loading files…');
    try {
      const files = await requestFiles('/post/files', {signal: controller.signal});
      if (!dialog.open) return;
      if (files !== null && (!Array.isArray(files) || !files.every(validEntry))) throw new Error('Unable to read files.');
      const choices = (files || []).filter(entry => !single || entry.id.endsWith('.' + extension));
      feedback.textContent = choices.length ? '' : single ? `No ${kind} files in the library.` : 'No files in the library.';
      for (const entry of choices) {
        const row = element('li');
        const label = element('label');
        const check = element('input');
        check.type = single ? 'radio' : 'checkbox';
        check.name = 'download-library-choice';
        check.disabled = existing.some(current => current.id === entry.id);
        const name = element('span', entry.label);
        name.append(element('small', check.disabled ? (single ? `Current ${kind}` : 'Already added') : entry.id.split('.').pop().toUpperCase() + ' · ' + sizeLabel(entry.size)));
        label.append(check, name);
        row.append(label);
        list.append(row);
        check.addEventListener('change', () => {
          if (single) selected.clear();
          if (check.checked && selected.size >= room) { check.checked = false; feedback.textContent = `This component can hold ${maxFiles} files. Deselect a file to choose another.`; return; }
          if (check.checked) selected.set(entry.id, {...entry}); else selected.delete(entry.id);
          confirm.disabled = !selected.size;
          confirm.textContent = single ? singleAction : selected.size ? `Add ${selected.size} ${selected.size === 1 ? 'file' : 'files'}` : 'Add files';
          feedback.textContent = selectionSummary();
        });
      }
      filterChoices();
    } catch (error) {
      if (error.name !== 'AbortError' && dialog.open) {
        showUploadFailure(feedback, error, () => {
          search.focus({preventScroll: true});
          load();
        }, 'Your edits are unchanged.', 'Retry loading files');
      }
    }
  }
}

