import {openMediaLibrary, uploadImage, setMediaStatus, showUploadFailure} from './media.js';
import {openAlbumOrder} from './album-order.js';

const editors = new WeakMap();
const photoLine = /^!\[((?:\\.|[^\]\\])*)\]\((\/images\/[a-f0-9]{64}\.(?:png|jpeg|gif))\)$/;
const escaped = caption => caption.replace(/[\\\[\]]/g, '\\$&');

function initialize(root) {
  root.querySelectorAll('[data-album-editor]').forEach(section => {
    if (editors.has(section)) return;
    const source = section.querySelector('[name="album"]');
    const form = source.form;
    const list = section.querySelector('[data-album-list]');
    const status = section.querySelector('[data-album-status]');
    const progress = section.querySelector('[data-album-upload-progress]');
    const recovery = section.querySelector('[data-album-recovery]');
    let photos = [];
    let removed = [];
    const cards = new Map();

    function icon(line) {
      const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
      svg.setAttribute('viewBox', '0 0 24 24');
      svg.setAttribute('aria-hidden', 'true');
      svg.setAttribute('width', '20');
      svg.setAttribute('height', '20');
      const path = document.createElementNS(svg.namespaceURI, 'path');
      path.setAttribute('d', line);
      path.setAttribute('fill', 'none');
      path.setAttribute('stroke', 'currentColor');
      path.setAttribute('stroke-width', '2');
      path.setAttribute('stroke-linecap', 'round');
      svg.append(path);
      return svg;
    }

    function updatePositions() {
      [...list.children].forEach((item, index) => {
        item.dataset.albumIndex = index;
        item.querySelector('[data-album-remove]').setAttribute('aria-label', 'Remove photo ' + (index + 1) + ' from album');
        item.querySelector('[data-album-caption]').setAttribute('aria-label', 'Caption for photo ' + (index + 1));
        const preview = item.querySelector('[data-album-photo]');
        preview.setAttribute('aria-label', 'Preview photo ' + (index + 1));
        preview.dataset.caption = photos[index].caption;
        preview.querySelector('img').alt = photos[index].caption || 'Photo ' + (index + 1);
      });
    }

    function serialize() {
      source.value = photos.map(photo => `![${escaped(photo.caption)}](${photo.path})`).join('\n');
      source.dispatchEvent(new Event('input', {bubbles: true}));
    }

    function updateRecovery() {
      removed = removed.filter(entry => !photos.some(photo => photo.path === entry.photo.path));
      recovery.hidden = removed.length === 0;
      const message = `${removed.length} ${removed.length === 1 ? 'photo removed.' : 'photos removed.'}${photos.length >= 50 ? ' Remove a photo to make room for Undo.' : ''}`;
      const text = recovery.querySelector('[data-album-recovery-message]');
      if (text.textContent !== message) text.textContent = message;
      recovery.querySelector('[data-album-undo]').disabled = Boolean(form.dataset.uploading) || photos.length >= 50;
    }

    function render() {
      const remaining = 50 - photos.length;
      for (const control of section.querySelectorAll('[data-album-upload], [data-album-browse], [data-album-files]')) control.disabled = Boolean(form.dataset.uploading) || remaining === 0;
      const capacity = section.querySelector('[data-album-capacity]');
      capacity.hidden = remaining > 10;
      capacity.textContent = remaining === 0 ? 'Album full (50 photos)' : `${remaining} ${remaining === 1 ? 'space' : 'spaces'} left`;
      section.querySelector('[data-album-count]').textContent = photos.length ? `${photos.length} ${photos.length === 1 ? 'photo' : 'photos'}` : '';
      section.querySelector('[data-album-reorder]').disabled = Boolean(form.dataset.uploading) || photos.length < 2;
      section.querySelector('[data-album-reorder]').hidden = photos.length < 2;
      for (const [path, item] of cards) {
        if (photos.some(photo => photo.path === path)) continue;
        item.remove();
        cards.delete(path);
      }
      photos.forEach((photo, index) => {
        const existing = cards.get(photo.path);
        if (existing) {
          const caption = existing.querySelector('[data-album-caption]');
          if (caption.value !== photo.caption) caption.value = photo.caption;
          existing.querySelector('[data-album-remove]').disabled = Boolean(form.dataset.uploading);
          if (list.children[index] !== existing) list.insertBefore(existing, list.children[index] || null);
          return;
        }
        const item = document.createElement('li');
        item.dataset.albumIndex = index;
        const image = document.createElement('img');
        image.src = photo.path + '?variant=thumbnail';
        image.srcset = `${photo.path}?variant=thumbnail 1x, ${photo.path} 2x`;
        image.alt = photo.caption || 'Photo ' + (index + 1);
        image.loading = 'lazy';
        image.draggable = false;
        const preview = document.createElement('div');
        preview.className = 'album-photo-preview';
        const open = document.createElement('a');
        open.className = 'album-photo-open';
        open.href = photo.path;
        open.dataset.albumPhoto = '';
        open.setAttribute('hx-boost', 'false');
        open.title = 'Preview photo';
        const hint = document.createElement('span');
        hint.className = 'album-preview-hint';
        hint.setAttribute('aria-hidden', 'true');
        hint.append(icon('M8 3H3v5M16 3h5v5M3 16v5h5M21 16v5h-5'));
        open.append(image, hint);
        const remove = document.createElement('button');
        remove.type = 'button';
        remove.className = 'album-photo-remove';
        remove.dataset.albumRemove = '';
        remove.title = 'Remove from album';
        remove.disabled = Boolean(form.dataset.uploading);
        remove.append(icon('m6 6 12 12M6 18 18 6'));
        preview.append(open, remove);
        const caption = document.createElement('textarea');
        caption.rows = 1;
        caption.maxLength = 500;
        caption.value = photo.caption;
        caption.placeholder = 'Add a caption…';
        caption.dataset.albumCaption = '';
        const captionEditor = document.createElement('div');
        captionEditor.className = 'album-caption-editor';
        captionEditor.append(caption);
        item.append(preview, captionEditor);
        cards.set(photo.path, item);
        list.insertBefore(item, list.children[index] || null);
      });
      updatePositions();
      updateRecovery();
    }

    function read() {
      removed = [];
      updateRecovery();
      const next = [];
      for (const line of source.value.split(/\r?\n/).filter(line => line.trim())) {
        const match = photoLine.exec(line);
        if (!match || next.length >= 50 || next.some(photo => photo.path === match[2])) {
          source.hidden = false;
          section.querySelector('[data-album-source-label]').hidden = false;
          section.querySelector('.album-editor-controls').hidden = true;
          list.replaceChildren();
          setMediaStatus(status, 'Some album entries are invalid. Correct the album field before adding photos.', true);
          return;
        }
        next.push({caption: match[1].replace(/\\(.)/g, '$1'), path: match[2]});
      }
      photos = next;
      source.hidden = true;
      section.querySelector('[data-album-source-label]').hidden = true;
      section.querySelector('.album-editor-controls').hidden = false;
      render();
      setMediaStatus(status, '');
    }

    function add(paths) {
      const selected = [...new Set(paths)].filter(path => !photos.some(photo => photo.path === path));
      if (photos.length + selected.length > 50) throw new Error('Choose at most 50 photos for the album.');
      if (selected.length === 0) return;
      photos.push(...selected.map(path => ({path, caption: ''})));
      serialize();
      render();
      if (!form.dataset.uploading) setMediaStatus(status, `${selected.length} ${selected.length === 1 ? 'photo added' : 'photos added'}. Save the post to apply changes.`);
    }

    async function upload(files) {
      if (!section.isConnected || form.dataset.uploading) return;
      if (photos.length + files.length > 50) {
        const remaining = 50 - photos.length;
        setMediaStatus(status, remaining ? `You can add ${remaining} more ${remaining === 1 ? 'photo' : 'photos'}. Choose fewer files.` : 'The album is full. Remove a photo before adding more.', true);
        return;
      }
      form.dataset.uploading = 'true';
      const controls = [...form.querySelectorAll('button, input[type="file"], [data-image-value]')].filter(control => !control.disabled);
      controls.forEach(control => { control.disabled = true; });
      setMediaStatus(status, '');
      progress.hidden = false;
      const failed = [];
      const before = photos.length;
      let interrupted;
      try {
        for (let index = 0; index < files.length; index++) {
          if (!section.isConnected) return;
          const file = files[index];
          progress.textContent = `Uploading ${index + 1} of ${files.length}…`;
          progress.title = file.name;
          try {
            const path = await uploadImage(file);
            if (!section.isConnected) return;
            add([path]);
          } catch (error) {
            failed.push({file, error});
            // A rejected image must not prevent later valid files from being
            // added. Authentication and service failures pause the remaining batch.
            if (!error.status || error.status >= 500 || [401, 403, 429].includes(error.status)) {
              interrupted = files.slice(index + 1);
              break;
            }
          }
        }
      } finally {
        delete form.dataset.uploading;
        progress.hidden = true;
        progress.textContent = '';
        progress.removeAttribute('title');
        controls.forEach(control => { control.disabled = false; });
        render();
      }
      if (!section.isConnected) return;
      const added = photos.length - before;
      if (failed.length === 0) {
        const duplicate = files.length - added;
        setMediaStatus(status, added === 0 ? 'Those photos are already in the album.' : `${added} ${added === 1 ? 'photo added' : 'photos added'}.${duplicate ? ' Other selected photos are already in the album.' : ''} Save the post to apply changes.`);
        return;
      }
      // Keep successful uploads and retry only failed or unattempted files.
      const remaining = [...failed.map(result => result.file), ...(interrupted || [])];
      const retry = () => upload(remaining);
      const error = failed.find(result => ['authentication_required', 'verification_required'].includes(result.error.code))?.error || failed[0].error;
      showUploadFailure(status, {...error, message: `${added} ${added === 1 ? 'photo added' : 'photos added'}. ${remaining.length} not uploaded.`}, retry, 'Photos already added are kept.');
      const failures = document.createElement('ul');
      for (const {file, error} of failed) {
        const item = document.createElement('li');
        item.textContent = file.name + ': ' + error.message;
        failures.append(item);
      }
      if (interrupted?.length) {
        const item = document.createElement('li');
        item.textContent = interrupted.length + ' remaining photos were not attempted.';
        failures.append(item);
      }
      status.append(failures);
      if (!status.querySelector('[data-upload-retry]')) {
        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'button-secondary';
        button.dataset.uploadRetry = '';
        button.textContent = 'Retry remaining uploads';
        button.addEventListener('click', retry);
        status.append(button);
      }
    }

    section.addEventListener('focusout', event => {
      if (!event.target.matches('[data-album-caption]')) return;
      event.target.scrollTop = event.target.scrollLeft = 0;
    });
    section.addEventListener('input', event => {
      if (!event.target.matches('[data-album-caption]')) return;
      const index = Number(event.target.closest('[data-album-index]').dataset.albumIndex);
      // Captions wrap while editing, but the album format keeps one photo per line.
      if (/[\r\n]/.test(event.target.value)) {
        const position = event.target.value.slice(0, event.target.selectionStart).replace(/[\r\n]+/g, ' ').length;
        event.target.value = event.target.value.replace(/[\r\n]+/g, ' ');
        event.target.setSelectionRange(position, position);
      }
      photos[index].caption = event.target.value;
      const preview = list.children[index].querySelector('[data-album-photo]');
      preview.dataset.caption = photos[index].caption;
      preview.querySelector('img').alt = photos[index].caption || 'Photo ' + (index + 1);
      serialize();
    });
    section.addEventListener('click', event => {
      if (form.dataset.uploading) return;
      event.target.closest('[data-album-upload], [data-album-browse]')?.focus({preventScroll: true});
      if (event.target.closest('[data-album-reorder]') && photos.length > 1) {
        openAlbumOrder(photos, ordered => {
          if (!section.isConnected || form.dataset.uploading) return;
          photos = ordered;
          serialize();
          render();
          setMediaStatus(status, 'Photo order updated. Save the post to apply changes.');
        }, section.querySelector('[data-album-reorder]'));
        return;
      }
      if (event.target.closest('[data-album-upload]')) section.querySelector('[data-album-files]').click();
      if (event.target.closest('[data-album-browse]')) {
        if (photos.length >= 50) {
          setMediaStatus(status, 'Remove a photo before adding more. Albums hold up to 50 photos.', true);
          return;
        }
        openMediaLibrary(paths => {
          if (!section.isConnected || form.dataset.uploading) return;
          try { add(paths); } catch (error) { setMediaStatus(status, error.message, true); }
        }, {multiple: true, excluded: photos.map(photo => photo.path), limit: 50 - photos.length});
      }
      const undo = event.target.closest('[data-album-undo]');
      if (undo && removed.length) {
        if (photos.length >= 50) { setMediaStatus(status, 'The album already has 50 photos.', true); return; }
        const entry = removed.pop();
        const index = Math.min(entry.index, photos.length);
        photos.splice(index, 0, entry.photo);
        serialize();
        render();
        const item = list.children[index];
        const focus = item?.querySelector('[data-album-remove]');
        focus?.focus();
        setMediaStatus(status, 'Photo restored. Save the post to apply changes.');
        return;
      }
      const button = event.target.closest('[data-album-remove]');
      if (!button) return;
      const index = Number(button.closest('[data-album-index]').dataset.albumIndex);
      removed.push({photo: photos.splice(index, 1)[0], index});
      if (removed.length > 50) removed.shift();
      serialize();
      render();
      const next = list.children[Math.min(index, photos.length - 1)];
      (next?.querySelector('[data-album-remove]') || section.querySelector('[data-album-add]')).focus({preventScroll: true});
      setMediaStatus(status, '');
    });
    section.querySelector('[data-album-files]').addEventListener('change', event => {
      const files = [...event.target.files];
      event.target.value = '';
      if (files.length) upload(files);
    });
    source.addEventListener('change', read);
    editors.set(section, true);
    read();
  });
}

document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
