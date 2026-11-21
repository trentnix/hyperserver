import {runFormUpload, setMediaStatus, showUploadFailure} from './media.js';
import {markField} from './forms.js';
import {initializeComponentPanels, registerComponent, jsonComponentValue} from './post-components.js';
import {requestFiles, validEntry, openFileLibrary, sizeLabel} from './file-picker.js';

const initialized = new WeakSet();

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


function initialize(root) {
  root.querySelectorAll('[data-downloads-editor]').forEach(section => {
    if (initialized.has(section)) return;
    initialized.add(section);
    const source = section.querySelector('textarea[name]');
    const maxFiles = Number(section.dataset.maxFiles);
    const pdfOnly = section.dataset.postComponent === 'pdf-preview';
    const accepts = entry => validEntry(entry) && (!pdfOnly || entry.id.endsWith('.pdf'));
    const limitMessage = pdfOnly ? 'Choose one PDF for the preview.' : 'Choose at most 20 files for this component.';
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
          const options = element('fieldset', '', 'pdf-editor-options');
          options.setAttribute('aria-label', 'Viewer options');
          const option = element('label', '', 'file-download-option');
          const showDownload = element('input');
          showDownload.type = 'checkbox';
          showDownload.checked = !entry.hideDownload;
          showDownload.addEventListener('change', () => {
            if (showDownload.checked) delete entry.hideDownload;
            else entry.hideDownload = true;
            serialize();
          });
          option.append(showDownload, document.createTextNode('Show download link'));

          const presentationOption = element('label', '', 'file-download-option pdf-editor-presentation-option');
          const copy = element('span');
          const presentationTitle = element('span', 'Presentation mode');
          presentationTitle.id = source.id + '-presentation-title';
          const hint = element('small', 'One page at a time, with Previous and Next buttons.', 'form-hint');
          hint.id = source.id + '-presentation-help';
          const presentation = element('input');
          presentation.type = 'checkbox';
          presentation.checked = Boolean(entry.presentation);
          presentation.setAttribute('aria-labelledby', presentationTitle.id);
          presentation.setAttribute('aria-describedby', hint.id);
          presentation.addEventListener('change', () => {
            if (presentation.checked) entry.presentation = true;
            else delete entry.presentation;
            serialize();
          });
          copy.append(presentationTitle, hint);
          presentationOption.append(presentation, copy);
          options.append(option, presentationOption);
          fields.append(options);
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
          setMediaStatus(status, 'File removed. Save to apply changes.');
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
        if (!failed.length) { setMediaStatus(status, pdfOnly ? 'PDF ready. Save to apply changes.' : 'Files added. Save to apply changes.'); return; }
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
    section.querySelector('[data-download-browse]').addEventListener('click', () => openFileLibrary(entries, add, status, form, {maxFiles, extension: pdfOnly ? 'pdf' : ''}));
    section.querySelector('[data-download-undo]').addEventListener('click', () => {
      if (form.dataset.uploading || !removed.length || !pdfOnly && entries.length >= maxFiles) return;
      const value = removed.pop();
      if (pdfOnly) entries = [value.entry];
      else entries.splice(Math.min(value.index, entries.length), 0, value.entry);
      serialize(); render();
      if (pdfOnly) list.querySelector('input')?.focus();
      setMediaStatus(status, pdfOnly ? 'PDF restored. Save to apply changes.' : 'File restored. Save to apply changes.');
    });
    form.addEventListener('blog:uploads-finished', render);
    source.addEventListener('change', read);
    registerComponent(section, {
      label: () => entries[0]?.label,
      normalize: value => jsonComponentValue(value, {strings: ['description'], flags: ['hideDownload', 'presentation'], empty: '[]'}),
      snapshot: () => ({source: source.value, removed: [...removed]}),
      clear: () => { source.value = ''; read(); },
      restore: copy => { source.value = copy.source; read(); removed = copy.removed; render(); },
      focus: () => (source.hidden ? section.querySelector('[data-download-upload]') : source).focus()
    });
    read();
  });
}


initializeComponentPanels(initialize);
