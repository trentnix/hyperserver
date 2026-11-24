import {getMarkdownEditor} from './markdown-editor.js';
import {runFormUpload, setMediaStatus, showUploadFailure, uploadImage, uploadStorageFull} from './media.js';
import {clearPostComponents} from './post-components.js';

const maxMarkdownBytes = 256 * 1024;
const storageFullMessage = 'Upload storage is full. Free up space before uploading.';
const encoder = new TextEncoder();
const decoder = new TextDecoder('utf-8', {ignoreBOM: true});
const markdownExtension = /\.(md|markdown)$/i;
const relativePath = ({webkitRelativePath: path = ''}) => path.slice(path.indexOf('/') + 1);

const readEntryFile = entry => new Promise((resolve, reject) => entry.file(resolve, reject));

async function readDroppedFolder(folder, signal) {
  const files = new Map();
  async function visit(directory, prefix) {
    const reader = directory.createReader();
    while (!signal.aborted) {
      // Browsers can return a directory in batches. An empty batch ends it.
      const entries = await new Promise((resolve, reject) => reader.readEntries(resolve, reject));
      if (!entries.length) return;
      for (const entry of entries) {
        if (signal.aborted) return;
        const path = prefix + entry.name;
        if (entry.isDirectory) await visit(entry, path + '/');
        else if (entry.isFile) files.set(path, await readEntryFile(entry));
      }
    }
  }
  await visit(folder, '');
  return files;
}

function resolveFolderPath(document, image) {
  const parts = document.split('/').slice(0, -1);
  for (const segment of image.split('/')) {
    if (segment === '..') {
      if (!parts.length) return;
      parts.pop();
    } else {
      parts.push(segment);
    }
  }
  return parts.join('/');
}

function matchImages(plan, entries, document) {
  const matches = new Map();
  const used = new Set([document]);
  for (const {path} of plan) {
    if (matches.has(path)) continue;
    const filename = resolveFolderPath(document, path);
    if (filename === undefined) throw new Error(`Image is outside the chosen folder: ${path}.`);
    const file = entries.get(filename);
    if (!file || filename === document) throw new Error(`Missing image: ${path}.`);
    matches.set(path, file);
    used.add(filename);
  }
  const extra = [...entries.keys()].find(path => !used.has(path));
  if (extra !== undefined) throw new Error(`Extra file: ${extra}. Include only the Markdown file and its images.`);
  return matches;
}

function rewriteMarkdown(source, images, paths) {
  const parts = [];
  let cursor = 0;
  for (const image of images) {
    parts.push(decoder.decode(source.subarray(cursor, image.start)), image.before, paths.get(image.path), image.after);
    cursor = image.end;
  }
  parts.push(decoder.decode(source.subarray(cursor)));
  return parts.join('');
}

export function initializeMarkdownImport(dialog) {
  if (dialog.dataset.initialized) return;
  dialog.dataset.initialized = 'true';
  const editorForm = dialog.previousElementSibling;
  const actions = dialog.parentElement.querySelector('[data-markdown-import-actions]');
  const menu = actions.querySelector('details');
  const content = editorForm.elements.content;
  const componentOrder = editorForm.elements.componentOrder;
  const draft = editorForm.querySelector('input[name="published"][value=""]');
  const importForm = dialog.querySelector('form');
  const source = importForm.querySelector('#markdown-import-source');
  const folder = importForm.querySelector('#markdown-import-folder');
  const pickFolder = importForm.querySelector('[data-markdown-import-pick-folder]');
  const pickFile = importForm.querySelector('[data-markdown-import-pick-file]');
  const picker = importForm.querySelector('[data-markdown-import-picker]');
  const selection = importForm.querySelector('[data-markdown-import-selection]');
  const name = selection.querySelector('[data-markdown-import-name]');
  const imageCount = selection.querySelector('[data-markdown-import-image-count]');
  const change = selection.querySelector('[data-markdown-import-change]');
  const status = importForm.querySelector('[data-markdown-import-status]');
  const submit = dialog.querySelector('[data-markdown-import-submit]');
  const submitLabel = submit.textContent;
  const inputs = [pickFolder, pickFile, source, folder, change];
  let session;
  let reading;

  const launchBlocked = () => editorForm.hasAttribute('data-uploading') ||
    editorForm.hasAttribute('data-component-action-pending');
  const launchButtons = actions.querySelectorAll('[data-markdown-import-open]');
  const updateLaunchButtons = () => {
    const blocked = launchBlocked();
    launchButtons.forEach(button => { button.disabled = blocked; });
  };
  const launchObserver = new MutationObserver(updateLaunchButtons);
  launchObserver.observe(editorForm, {attributeFilter: ['data-uploading', 'data-component-action-pending']});
  editorForm.addEventListener('htmx:before:cleanup', event => {
    if (event.target === editorForm) launchObserver.disconnect();
  });
  updateLaunchButtons();
  actions.hidden = false;
  const folderSupported = 'webkitdirectory' in folder;
  const dropSupported = typeof globalThis.DataTransferItem?.prototype.webkitGetAsEntry === 'function';
  pickFolder.hidden = !folderSupported;
  picker.toggleAttribute('data-folder-drop', dropSupported);
  const prompt = picker.querySelector('[data-markdown-import-prompt]');
  if (dropSupported) prompt.textContent = 'Drop a folder with your Markdown and images';
  else if (!folderSupported) prompt.textContent = 'Choose a Markdown file';
  pickFolder.addEventListener('click', () => folder.click());
  pickFile.addEventListener('click', () => source.click());
  const focusPicker = () => (folderSupported ? pickFolder : pickFile).focus({preventScroll: true});
  function showPicker() {
    importForm.reset();
    picker.hidden = false;
    picker.removeAttribute('data-drag-over');
    selection.hidden = true;
  }
  function resetSource() {
    reading?.abort();
    session = {uploads: new WeakMap()};
    showPicker();
    inputs.forEach(input => { input.disabled = false; });
    submit.textContent = submitLabel;
    submit.disabled = true;
    setMediaStatus(status, '');
  }
  const failureMessage = (label, message) => `Could not import “${label}”:\n${message}`;
  function rejectSelection(label, message) {
    resetSource();
    setMediaStatus(status, failureMessage(label, message), true);
    focusPicker();
  }
  actions.addEventListener('click', event => {
    const button = event.target.closest('[data-markdown-import-open]');
    if (!button || launchBlocked()) return;
    const returnFocus = menu.contains(button) ? menu.querySelector('summary') : button;
    returnFocus.focus({preventScroll: true});
    menu.open = false;
    resetSource();
    dialog.showModal();
  });
  change.addEventListener('click', () => {
    resetSource();
    focusPicker();
  });
  dialog.querySelector('[data-markdown-import-cancel]').addEventListener('click', () => dialog.close());
  dialog.addEventListener('close', () => {
    if (dialog.open) return;
    reading?.abort();
    session = null;
  });

  async function prepareSelection(entries, folderName = '') {
    if (!session || session.busy) return;
    reading?.abort();
    reading = new AbortController();
    const {signal} = reading;
    session.plan = null;
    const documents = [...entries.keys()].filter(path => markdownExtension.test(path));
    const [document] = documents;
    const file = entries.get(document);
    const label = folderName || entries.keys().next().value;
    name.textContent = documents.length === 1 ? file.name : label;
    picker.hidden = true;
    selection.hidden = false;
    imageCount.hidden = true;
    submit.textContent = submitLabel;
    submit.disabled = true;
    change.focus({preventScroll: true});
    setMediaStatus(status, 'Reading Markdown…');
    try {
      if (!file) throw new Error(folderName ? 'No Markdown file found in this folder.' : 'Choose a .md or .markdown file.');
      if (documents.length > 1) throw new Error('This folder contains more than one Markdown file.');
      if (file.size > maxMarkdownBytes) throw new Error('Choose a Markdown file of at most 256 KiB.');
      let markdown;
      try {
        markdown = new TextDecoder('utf-8', {fatal: true}).decode(await file.arrayBuffer());
      } catch {
        throw new Error('Choose a Markdown file encoded as UTF-8.');
      }
      const bytes = encoder.encode(markdown.replace(/\r\n?/g, '\n'));
      const response = await fetch('/post/markdown-import', {
        method: 'POST', body: bytes, signal, redirect: 'error',
        headers: {'Content-Type': 'text/plain; charset=utf-8'}
      });
      if (response.status === 403) throw new Error('Check that you are signed in and have access to create posts.');
      const result = await response.json();
      if (!response.ok) throw new Error(result.error || 'Unable to read the Markdown file.');
      if (signal.aborted || !dialog.open || !dialog.isConnected) return;
      if (!folderName && result.length) throw new Error('This file uses local images. Use Choose folder to import the file and images together.');
      const matches = matchImages(result, entries, document);
      if (matches.size && uploadStorageFull(editorForm)) throw new Error(storageFullMessage);
      session.label = label;
      session.file = file;
      session.plan = result;
      session.bytes = bytes;
      session.matches = matches;
      const count = matches.size;
      imageCount.hidden = count === 0;
      if (count) imageCount.textContent = count === 1 ? '1 image ready' : `${count} images ready`;
      submit.disabled = false;
      setMediaStatus(status, '');
    } catch (error) {
      if (signal.aborted || !dialog.open || !dialog.isConnected) return;
      rejectSelection(label, error instanceof SyntaxError || error instanceof TypeError ?
        'Unable to read the Markdown file. Try again.' : error.message);
    }
  }
  function selectFile(file) {
    prepareSelection(new Map([[file.name, file]]));
  }
  source.addEventListener('change', () => {
    if (session && source.files.length) selectFile(source.files[0]);
  });
  folder.addEventListener('change', () => {
    if (!session || !folder.files.length) return;
    const entries = new Map([...folder.files].map(file => [relativePath(file), file]));
    prepareSelection(entries, folder.files[0].webkitRelativePath.split('/')[0]);
  });

  if (dropSupported) {
    picker.addEventListener('dragover', event => {
      if (!event.dataTransfer.types.includes('Files')) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = 'copy';
      picker.setAttribute('data-drag-over', '');
    });
    picker.addEventListener('dragleave', event => {
      if (!picker.contains(event.relatedTarget)) picker.removeAttribute('data-drag-over');
    });
    picker.addEventListener('drop', async event => {
      if (!event.dataTransfer.types.includes('Files')) return;
      event.preventDefault();
      picker.removeAttribute('data-drag-over');
      if (!session || session.busy) return;
      resetSource();
      const items = [...event.dataTransfer.items].filter(item => item.kind === 'file');
      // The drag data store is readable only during the drop event.
      const entry = items.length === 1 ? items[0].webkitGetAsEntry() : null;
      if (!entry?.isDirectory && !(entry?.isFile && markdownExtension.test(entry.name))) {
        setMediaStatus(status, 'Drop one folder or Markdown file.', true);
        return;
      }
      reading = new AbortController();
      const {signal} = reading;
      const active = () => !signal.aborted && dialog.open && dialog.isConnected;
      setMediaStatus(status, entry.isDirectory ? 'Reading folder…' : 'Reading Markdown…');
      try {
        if (entry.isDirectory) {
          const entries = await readDroppedFolder(entry, signal);
          if (active()) prepareSelection(entries, entry.name);
        } else {
          const file = await readEntryFile(entry);
          if (active()) selectFile(file);
        }
      } catch {
        if (active()) rejectSelection(entry.name, 'Unable to read the dropped files. Try choosing the folder or file.');
      }
    });
  }

  const replacementState = () => [content.value, componentOrder.value, editorForm.elements.published.value];
  const listFormat = new Intl.ListFormat('en');

  async function importMarkdown() {
    const current = session;
    if (!current?.plan || current.busy || submit.disabled) return;
    const active = () => session === current && dialog.open && editorForm.isConnected;
    const plan = current.plan;
    const bytes = current.bytes;
    const previous = replacementState();
    const [writing, components, publication] = previous;
    const asDraft = publication === draft.value;
    const original = JSON.stringify(previous);
    const unchanged = () => current.plan === plan && JSON.stringify(replacementState()) === original;
    const replaced = [];
    if (writing && (plan.length || writing !== decoder.decode(bytes))) replaced.push('post content');
    if (components && components !== '[]') replaced.push('components');
    if (!asDraft) replaced.push('publication setting');
    current.busy = true;
    const controls = [...inputs, submit];
    let imagePath;
    try {
      if (replaced.length && (current.approved?.original !== original || current.approved.plan !== plan)) {
        const confirmed = await new Promise(resolve => window.blogConfirm({
          title: 'Replace unsaved changes?',
          message: `Importing will replace your unsaved ${listFormat.format(replaced)}.` +
            (asDraft ? '' : ' The post will be set to Draft.'),
          action: 'Replace and import'
        }, () => resolve(true), () => resolve(false)));
        if (!confirmed || !active()) return;
        if (!unchanged()) throw new Error('The post changed. Review your edits and try importing again.');
        current.approved = {original, plan};
      }
      picker.hidden = true;
      selection.hidden = false;
      setMediaStatus(status, '');
      controls.forEach(control => { control.disabled = true; });
      const paths = new Map();
      if (current.matches.size) {
        await runFormUpload(editorForm, async () => {
          for (const [path, file] of current.matches) {
            if (!active()) return;
            let uploaded = current.uploads.get(file);
            if (!uploaded) {
              if (uploadStorageFull(editorForm)) throw new Error(storageFullMessage);
              imagePath = path;
              setMediaStatus(status, `Uploading image ${paths.size + 1} of ${current.matches.size}…`);
              uploaded = await uploadImage(file, editorForm);
              imagePath = undefined;
              current.uploads.set(file, uploaded);
            }
            paths.set(path, uploaded);
          }
        });
      }
      if (!active()) return;
      if (!unchanged()) throw new Error('The post changed. Review your edits and try importing again.');
      const markdown = rewriteMarkdown(bytes, plan, paths);
      if (encoder.encode(markdown).length > maxMarkdownBytes) {
        throw new Error('With its image links, this Markdown exceeds the 256 KiB content limit.');
      }
      const title = editorForm.elements.title;
      if (!title.value.trim()) {
        const filename = current.file.name.replace(markdownExtension, '').trim();
        title.value = [...filename].slice(0, title.maxLength).join('');
        title.dispatchEvent(new Event('input', {bubbles: true}));
      }
      draft.checked = true;
      draft.dispatchEvent(new Event('change', {bubbles: true}));
      const editor = getMarkdownEditor(content);
      if (editor) editor.setMarkdown(markdown);
      else {
        content.value = markdown;
        content.dispatchEvent(new Event('input', {bubbles: true}));
      }
      clearPostComponents(editorForm);
      editor?.placement?.resetHistory();
      dialog.close();
      if (editor) editor.focus();
      else content.focus();
    } catch (error) {
      if (!active()) return;
      const message = imagePath ? `“${imagePath}” failed to upload. ${error.message}` : error.message;
      if (uploadStorageFull(editorForm) || [400, 413, 422].includes(error.status)) {
        rejectSelection(current.label, `${message} Your draft is unchanged.`);
      } else {
        showPicker();
        submit.textContent = 'Retry import';
        showUploadFailure(status, {message: failureMessage(current.label, message), code: error.code},
          null, 'Your draft is unchanged.');
      }
    } finally {
      if (active()) {
        controls.forEach(control => { control.disabled = false; });
        current.busy = false;
      }
    }
  }
  importForm.addEventListener('submit', event => {
    event.preventDefault();
    importMarkdown();
  });
}
