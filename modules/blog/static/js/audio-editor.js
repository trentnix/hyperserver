import {initializeComponentPanels, registerComponent, jsonComponentValue} from './post-components.js';
import {runFormUpload, setMediaStatus, showUploadFailure} from './media.js';
import {requestFiles, validEntry, openFileLibrary, sizeLabel} from './file-picker.js';

const initialized = new WeakSet();

function validAudio(value) {
  return value && typeof value === 'object' && !Array.isArray(value) &&
    /^[a-f0-9]{64}\.mp3$/.test(value.id) && Number.isSafeInteger(value.size) && value.size > 0 && value.size <= 33554432 &&
    Object.entries(value).every(([key, field]) => {
      if (key === 'id' || key === 'size') return true;
      if (key === 'hideDownload') return typeof field === 'boolean';
      const limit = {title: 240, description: 1000, transcript: 20000}[key];
      return limit && typeof field === 'string' && [...field].length <= limit;
    });
}

function initialize(root) {
  for (const section of root.querySelectorAll('[data-audio-editor]')) {
    if (initialized.has(section)) continue;
    initialized.add(section);
    const source = section.querySelector('textarea[name]');
    const form = source.form;
    const controls = section.querySelector('[data-audio-controls]');
    const fields = section.querySelector('[data-audio-fields]');
    const status = section.querySelector('[data-audio-status]');
    const uploadButton = section.querySelector('[data-audio-upload]');
    const browseButton = section.querySelector('[data-audio-browse]');
    const input = section.querySelector('[data-audio-file]');
    const player = section.querySelector('audio');
    const recovery = section.querySelector('[data-audio-recovery]');
    const texts = [...fields.querySelectorAll('[data-audio-field]')];
    const download = fields.querySelector('[data-audio-download]');
    const lifetime = new AbortController();
    let value = null;
    let previous;
    const filenames = new Map();
    let filenameRequest;

    function filename() {
      if (!value || filenames.has(value.id)) return;
      // Saved posts and recovered drafts keep only the immutable media ID.
      // Resolve presentation metadata once, without changing the component source.
      filenameRequest ??= requestFiles('/post/files', {signal: lifetime.signal}).then(entries => {
        for (const entry of entries || []) if (validEntry(entry)) filenames.set(entry.id, entry.label);
        if (section.isConnected) {
          render();
          source.dispatchEvent(new Event('blog:component-label', {bubbles: true}));
        }
      }).catch(() => {});
    }

    function serialize() {
      source.value = value ? JSON.stringify(value) : '';
      source.dispatchEvent(new Event('input', {bubbles: true}));
    }

    function render() {
      if (!source.hidden) return;
      const busy = Boolean(form.dataset.uploading);
      uploadButton.disabled = browseButton.disabled = busy;
      section.querySelector('[data-component-remove]').disabled = busy;
      uploadButton.textContent = value ? 'Replace MP3' : 'Upload MP3';
      fields.hidden = !value;
      fields.disabled = !value || section.hidden;
      recovery.hidden = !previous;
      recovery.querySelector('button').disabled = busy;
      const url = value ? '/files/' + value.id + '?preview=1' : '';
      if (player.getAttribute('src') !== url) {
        player.pause();
        if (url) player.setAttribute('src', url);
        else player.removeAttribute('src');
        player.load();
      }
      player.hidden = !value;
      section.querySelector('[data-audio-size]').textContent = value ? sizeLabel(value.size) : '';
      section.querySelector('[data-audio-name]').textContent = value ? filenames.get(value.id) || 'Selected MP3' : '';
      filename();
    }

    function read() {
      try {
        const next = source.value.trim() ? JSON.parse(source.value) : null;
        if (next !== null && !validAudio(next)) throw new Error();
        // Only a blank source means no file. Preserve malformed JSON such as null.
        if (next === null && source.value.trim()) throw new Error();
        value = next;
      } catch {
        source.hidden = section.querySelector('[data-audio-source-label]').hidden = false;
        controls.hidden = fields.hidden = fields.disabled = true;
        player.pause();
        player.removeAttribute('src');
        player.load();
        recovery.hidden = true;
        setMediaStatus(status, 'The audio source is invalid. Correct it before saving.', true);
        return;
      }
      source.hidden = section.querySelector('[data-audio-source-label]').hidden = true;
      controls.hidden = false;
      for (const field of texts) field.value = value?.[field.dataset.audioField] || '';
      download.checked = !value?.hideDownload;
      previous = undefined;
      render();
      setMediaStatus(status, '');
    }

    function add(entries) {
      const entry = entries[0];
      if (entries.length !== 1 || !validEntry(entry) || !entry.id.endsWith('.mp3') || entry.size <= 0) throw new Error('Choose one MP3 file.');
      filenames.set(entry.id, entry.label);
      if (value?.id === entry.id) { render(); return; }
      previous = value ? {...value} : undefined;
      value = {...value, id: entry.id, size: entry.size};
      serialize();
      render();
    }

    async function upload(file) {
      if (!section.isConnected || form.dataset.uploading) return;
      await runFormUpload(form, async () => {
        setMediaStatus(status, 'Uploading MP3…');
        try {
          if (!file.name.toLowerCase().endsWith('.mp3') || file.size > 33554432) throw new Error('Choose an MP3 of at most 32 MiB.');
          const body = new FormData();
          body.append('file', file);
          const entry = await requestFiles('/post/files/upload', {method: 'POST', body});
          if (!section.isConnected) return;
          add([entry]);
          setMediaStatus(status, 'MP3 uploaded.');
        } catch (error) {
          if (section.isConnected) showUploadFailure(status, error, () => upload(file), 'Your edits are preserved.');
        }
      });
    }

    uploadButton.addEventListener('click', () => input.click());
    input.addEventListener('change', () => {
      const file = input.files[0];
      input.value = '';
      if (file) upload(file);
    });
    browseButton.addEventListener('click', () => openFileLibrary(value ? [{id: value.id}] : [], add, status, form, {maxFiles: 1, extension: 'mp3'}));
    for (const field of texts) {
      field.addEventListener('input', () => {
        if (!value) return;
        value[field.dataset.audioField] = field.value;
        serialize();
      });
      if (field.tagName === 'INPUT') field.addEventListener('keydown', event => {
        if (event.key === 'Enter' && !event.isComposing) { event.preventDefault(); field.blur(); }
      });
    }
    download.addEventListener('change', () => {
      if (!value) return;
      if (download.checked) delete value.hideDownload;
      else value.hideDownload = true;
      serialize();
    });
    recovery.querySelector('button').addEventListener('click', () => {
      if (!previous || form.dataset.uploading) return;
      value = previous;
      previous = undefined;
      serialize();
      read();
      setMediaStatus(status, 'Previous MP3 restored.');
    });
    form.addEventListener('blog:uploads-finished', render);
    source.addEventListener('change', read);
    registerComponent(section, {
      label: () => value?.title?.trim() || filenames.get(value?.id),
      normalize: value => jsonComponentValue(value, {strings: ['title', 'description', 'transcript'], flags: ['hideDownload']}),
      snapshot: () => ({source: source.value, previous}),
      clear: () => { source.value = ''; read(); },
      restore: copy => { source.value = copy.source; read(); previous = copy.previous; render(); },
      focus: () => (source.hidden ? uploadButton : source).focus()
    });
    document.addEventListener('htmx:before:cleanup', event => {
      if (event.target.contains?.(form)) { player.pause(); lifetime.abort(); }
    }, {signal: lifetime.signal});
    read();
  }
}

initializeComponentPanels(initialize);
