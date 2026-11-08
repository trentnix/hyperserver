// A separate native dialog keeps the current editor and its unsaved writing intact.
// HTMX owns browsing requests. JavaScript only connects a choice to the unsaved editor.
export function openMediaLibrary(select, {multiple = false, excluded = [], limit = 50} = {}) {
  if (document.querySelector('.media-browser')) return;
  const opener = document.activeElement;
  const dialog = document.querySelector('#media-browser-template').content.firstElementChild.cloneNode(true);
  const results = dialog.querySelector('[data-media-results]');
  const feedback = dialog.querySelector('[data-media-status]');
  feedback.removeAttribute('role');
  const browseStatus = document.createElement('span');
  browseStatus.className = 'media-browser-status';
  const status = document.createElement('span');
  status.className = 'media-browser-status';
  feedback.append(browseStatus, status);
  const selection = new AbortController();
  const choices = new Map();
  const excludedIDs = new Set(excluded.map(path => path.split('/').pop()));
  let count;
  let limitNote;
  let confirm;
  let review;
  let reviewList;
  let reviewToggle;
  let clearSelection;
  let showingSelected = false;
  let browseScroll = 0;
  if (multiple) {
    dialog.classList.add('media-browser-multiple');
    dialog.querySelector('h2').textContent = 'Choose album photos';
    const controls = document.querySelector('#media-selection-template').content.cloneNode(true);
    count = controls.querySelector('[data-media-selection-count]');
    limitNote = controls.querySelector('.media-selection-limit');
    review = controls.querySelector('.media-selection-review');
    const heading = review.querySelector('h3');
    reviewList = review.querySelector('.media-grid');
    reviewToggle = controls.querySelector('[data-media-show-selected]');
    clearSelection = controls.querySelector('[data-media-clear-selection]');
    confirm = controls.querySelector('[data-media-add-selected]');
    dialog.append(controls);
    reviewToggle.addEventListener('click', () => {
      if (choosing) return;
      if (!showingSelected) browseScroll = results.scrollTop;
      showingSelected = !showingSelected;
      results.hidden = showingSelected;
      review.hidden = !showingSelected;
      updateChoices();
      if (showingSelected) { review.scrollTop = 0; heading.focus({preventScroll: true}); }
      else results.scrollTop = browseScroll;
    });
    clearSelection.addEventListener('click', () => {
      if (choosing) return;
      choices.clear();
      showingSelected = false;
      results.hidden = false;
      review.hidden = true;
      updateChoices();
      setMediaStatus(status, '');
      (results.querySelector('[data-media-select]:not(:disabled)') || dialog.querySelector('[data-close-dialog]')).focus();
    });
  }
  let choosing = false;
  let browsing;
  let filtering = false;
  function renderSelectionReview() {
    const focused = reviewList.contains(document.activeElement) ? document.activeElement.closest('[data-media-select]') : null;
    const focusedIndex = focused ? [...reviewList.children].indexOf(focused.closest('li')) : -1;
    reviewList.replaceChildren(...[...choices].map(([id, name]) => {
      const item = document.createElement('li');
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'media-card';
      button.dataset.mediaSelect = id;
      button.setAttribute('aria-pressed', 'true');
      button.setAttribute('aria-label', 'Remove ' + name + ' from selection');
      button.disabled = choosing;
      const image = document.createElement('img');
      image.src = '/images/' + id + '?variant=thumbnail';
      image.alt = '';
      image.loading = 'lazy';
      image.width = 240;
      image.height = 160;
      const check = document.createElement('span');
      check.className = 'media-selection-check';
      check.setAttribute('aria-hidden', 'true');
      const caption = document.createElement('span');
      caption.className = 'media-card-caption';
      const filename = document.createElement('span');
      filename.className = 'media-filename';
      filename.textContent = name;
      filename.title = name;
      caption.append(filename);
      button.append(image, check, caption);
      item.append(button);
      return item;
    }));
    if (focusedIndex >= 0) {
      const same = [...reviewList.querySelectorAll('[data-media-select]')].find(button => button.dataset.mediaSelect === focused.dataset.mediaSelect);
      (same || reviewList.children[Math.min(focusedIndex, reviewList.children.length - 1)]?.querySelector('button') || reviewToggle).focus({preventScroll: true});
    }
  }
  function updateChoices() {
    if (!multiple) return;
    const leavingEmptyReview = showingSelected && choices.size === 0;
    const lastReviewedID = leavingEmptyReview ? reviewList.querySelector('[data-media-select]')?.dataset.mediaSelect : null;
    if (leavingEmptyReview) {
      showingSelected = false;
      results.hidden = false;
      review.hidden = true;
      results.scrollTop = browseScroll;
    }
    count.textContent = `${choices.size} ${choices.size === 1 ? 'photo' : 'photos'} selected`;
    confirm.textContent = choices.size ? `Add ${choices.size} ${choices.size === 1 ? 'photo' : 'photos'}` : 'Add photos';
    const message = choices.size >= limit ? 'Album limit reached. Deselect a photo to choose another.' : 'Albums can contain up to 50 photos.';
    if (limitNote.textContent !== message) limitNote.textContent = message;
    confirm.disabled = choosing || choices.size === 0 || results.hasAttribute('aria-busy');
    reviewToggle.textContent = showingSelected ? 'Back to library' : 'Show selected';
    reviewToggle.hidden = !showingSelected && choices.size === 0;
    reviewToggle.disabled = choosing;
    reviewToggle.setAttribute('aria-expanded', String(showingSelected));
    clearSelection.hidden = choices.size === 0;
    clearSelection.disabled = choosing;
    for (const button of results.querySelectorAll('[data-media-select]')) {
      const id = button.dataset.mediaSelect;
      const selected = choices.has(id);
      const atLimit = choices.size >= limit && !selected && !excludedIDs.has(id);
      button.setAttribute('aria-pressed', String(selected));
      button.classList.toggle('media-card-at-limit', atLimit);
      button.disabled = choosing || excludedIDs.has(id) || atLimit;
      if (!excludedIDs.has(id)) {
        button.setAttribute('aria-label', `${selected ? 'Deselect' : 'Select'} ${button.querySelector('.media-filename').textContent}`);
        button.title = atLimit ? 'Album limit reached. Deselect a photo to choose another.' : '';
        if (atLimit) button.setAttribute('aria-describedby', limitNote.id);
        else button.removeAttribute('aria-describedby');
      }
      if (!excludedIDs.has(id) && !button.querySelector('.media-selection-check')) {
        const check = document.createElement('span');
        check.className = 'media-selection-check';
        check.setAttribute('aria-hidden', 'true');
        button.prepend(check);
      }
      if (excludedIDs.has(id)) {
        button.classList.add('media-card-in-album');
        button.title = 'Added to album';
        button.setAttribute('aria-label', button.querySelector('.media-filename').textContent + ' — added to album, cannot be selected again');
        if (!button.querySelector('.media-album-badge')) {
          const badge = document.createElement('span');
          badge.className = 'media-album-badge';
          badge.textContent = 'Added to album';
          button.prepend(badge);
        }
      }
    }
    const hint = results.querySelector('.media-grid')?.previousElementSibling;
    if (hint?.matches('[role="status"]')) {
      const summary = hint.textContent.split(' · ')[0] + ' · Select photos to add.';
      if (hint.textContent !== summary) hint.textContent = summary;
    }
    if (showingSelected) renderSelectionReview();
    else reviewList.replaceChildren();
    if (leavingEmptyReview) {
      const previousPhoto = lastReviewedID ? [...results.querySelectorAll('[data-media-select]:not(:disabled)')].find(button => button.dataset.mediaSelect === lastReviewedID) : null;
      (previousPhoto || results.querySelector('[data-media-select]:not(:disabled), [data-media-filters] input:not([type=hidden])') || dialog.querySelector('[data-close-dialog]')).focus();
    }
  }
  dialog.addEventListener('htmx:before:request', event => {
    if (choosing) { event.preventDefault(); return; }
    browsing = event.detail.ctx;
    filtering = browsing.sourceElement.matches('[data-media-filters]');
    results.setAttribute('aria-busy', 'true');
    if (multiple) confirm.disabled = true;
    setMediaStatus(browseStatus, 'Loading images…');
  });
  // Keep failed responses out of the gallery. This chooser owns its Retry
  // feedback, including requests whose source was removed while loading.
  document.addEventListener('htmx:before:swap', event => {
    if (!browsing || event.detail.ctx !== browsing || !(browsing.response?.status >= 400)) return;
    event.preventDefault();
    event.stopImmediatePropagation();
  }, {capture: true, signal: selection.signal});
  document.addEventListener('htmx:error', event => {
    if (browsing && event.detail.ctx === browsing && browsing.text === undefined) event.stopImmediatePropagation();
  }, {capture: true, signal: selection.signal});
  // A search replaces its source form, so completion may dispatch on document.
  document.addEventListener('htmx:finally:request', event => {
    if (event.detail.ctx !== browsing) return;
    results.removeAttribute('aria-busy');
    const statusCode = browsing.response?.status || 0;
    const failed = statusCode < 200 || statusCode >= 300;
    const restoreFocus = !failed && document.activeElement === browseStatus;
    setMediaStatus(browseStatus, failed ? 'Unable to load images. Try again.' : '', failed);
    if (failed) {
      const retry = document.createElement('button');
      retry.type = 'button';
      retry.className = 'button-secondary';
      retry.dataset.mediaRetry = '';
      retry.textContent = 'Retry';
      retry.setAttribute('hx-get', browsing.request.action);
      retry.setAttribute('hx-target', '#media-results');
      retry.setAttribute('hx-swap', 'innerHTML show:none settle:0');
      retry.setAttribute('hx-sync', '#media-results:replace');
      retry.addEventListener('click', () => { browseStatus.tabIndex = -1; browseStatus.focus({preventScroll: true}); });
      browseStatus.append(' ', retry);
      htmx.process(retry);
    }
    if (multiple) confirm.disabled = choosing || choices.size === 0;
    if (restoreFocus) (showingSelected ? reviewToggle : results.querySelector('[data-media-filters] input:not([type=hidden]), [data-media-select]:not(:disabled)') || dialog.querySelector('[data-close-dialog]')).focus({preventScroll: true});
  }, {signal: selection.signal});
  dialog.addEventListener('htmx:after:swap', event => {
    updateChoices();
    results.scrollTop = 0;
    if (filtering) {
      results.querySelector('[data-media-filters] button')?.focus({preventScroll: true});
    }
  });
  dialog.addEventListener('click', async event => {
    const button = event.target.closest('[data-media-select]');
    if (!button || choosing || (results.contains(button) && results.hasAttribute('aria-busy'))) return;
    const id = button.dataset.mediaSelect;
    if (!/^[a-f0-9]{64}\.(png|jpeg|gif)$/.test(id)) return;
    if (multiple) {
      if (excludedIDs.has(id)) return;
      if (choices.has(id)) choices.delete(id);
      else {
        if (choices.size >= limit) return;
        choices.set(id, button.querySelector('.media-filename').textContent);
      }
      updateChoices();
      setMediaStatus(status, '');
      return;
    }
    choosing = true;
    button.disabled = true;
    setMediaStatus(status, 'Checking image…');
    try {
      // Cleanup can remove an image after the library was opened. Saving also
      // checks references, because choosing an image does not reserve it.
      const path = '/images/' + id;
      const response = await fetch(path, {method: 'HEAD', signal: selection.signal});
      if (response.status === 404) throw new Error('This image is no longer available. Choose another image or reopen the library.');
      if (!response.ok) throw new Error('Unable to check this image. Try again, or sign in again if your session has expired.');
      if (!dialog.open) return;
      select(path);
      dialog.close();
    } catch (error) {
      if (error.name !== 'AbortError') setMediaStatus(status, error.message, true);
    } finally {
      choosing = false;
      button.disabled = false;
    }
  });
  confirm?.addEventListener('click', async () => {
    if (choosing || choices.size === 0 || results.hasAttribute('aria-busy')) return;
    choosing = true;
    updateChoices();
    try {
      for (const [id, name] of choices) {
        setMediaStatus(status, 'Checking ' + name + '…');
        const response = await fetch('/images/' + id, {method: 'HEAD', signal: selection.signal});
        if (response.status === 404) {
          choices.delete(id);
          throw new Error(name + ' is no longer available. The other photos remain selected.');
        }
        if (!response.ok) throw new Error('Unable to check the selected photos. Try again, or sign in again if your session has expired.');
      }
      if (!dialog.open) return;
      select([...choices.keys()].map(id => '/images/' + id));
      dialog.close();
    } catch (error) {
      if (error.name !== 'AbortError') setMediaStatus(status, error.message, true);
    } finally {
      choosing = false;
      updateChoices();
    }
  });
  document.addEventListener('htmx:before:cleanup', event => {
    if (event.target.contains?.(opener)) dialog.close();
  }, {signal: selection.signal});
  document.addEventListener('htmx:after:swap', () => {
    if (!opener?.isConnected) dialog.close();
  }, {signal: selection.signal});
  dialog.addEventListener('close', () => {
    selection.abort();
    htmx.trigger(results, 'htmx:abort');
    dialog.remove();
    if (opener?.isConnected) opener.focus({preventScroll: true});
  }, {once: true});
  document.body.append(dialog);
  dialog.showModal();
  updateChoices();
  htmx.process(dialog);
}

// The library and profile controls use the same validated upload endpoint as the editor.
export async function uploadImage(file) {
  if (file.size > 8 * 1024 * 1024) throw Object.assign(new Error('Choose an image of at most 8 MiB.'), {status: 413});
  const data = new FormData();
  data.append('image', file);
  const response = await fetch('/post/images/upload', {method: 'POST', body: data, redirect: 'error'});
  if (response.status === 401) {
    throw Object.assign(new Error("You're signed out. Sign in again to upload this image."), {code: 'authentication_required'});
  }
  if (!(response.headers.get('Content-Type') || '').includes('application/json')) throw new Error('Unable to upload the image.');
  const result = await response.json();
  if (!response.ok) throw Object.assign(new Error(result?.error || 'Unable to upload the image.'), {status: response.status, code: response.status === 403 ? result?.code : undefined});
  const path = result?.data?.filePath;
  if (typeof path !== 'string' || !/^\/images\/[a-f0-9]{64}\.(png|jpeg|gif)$/.test(path)) throw new Error('Invalid image upload response.');
  return path;
}

export function setMediaStatus(status, message, failed = false) {
  status.classList.toggle('upload-error', failed);
  status.setAttribute('role', failed ? 'alert' : 'status');
  status.textContent = message;
}

// Authentication happens in another tab so neither unsaved fields nor the
// selected File leave this page. Retrying is always an explicit user action.
export function showUploadFailure(status, error, retry, preserved = 'Your edits are unchanged.') {
  setMediaStatus(status, error.message + ' ' + preserved, true);
  const verify = error.code === 'verification_required';
  if (error.code !== 'authentication_required' && !verify) return;
  status.append(' Keep this page open. After ' + (verify ? 'verifying your email' : 'signing in') + ', return here and retry.');
  const actions = document.createElement('span');
  actions.className = 'image-upload-recovery';
  const link = document.createElement('a');
  link.href = verify ? '/auth/request/verify' : '/login';
  link.target = '_blank';
  link.rel = 'noopener';
  link.setAttribute('hx-boost', 'false');
  link.textContent = verify ? 'Verify email in a new tab' : 'Sign in in a new tab';
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'button-secondary';
  button.dataset.uploadRetry = '';
  button.textContent = 'Retry upload';
  button.addEventListener('click', retry);
  actions.append(link, button);
  status.append(actions);
}

// Post and page previews store an image ID. Profile fields store the complete URL.
function updateImagePreview(picker) {
  picker.querySelector('[data-image-missing]')?.remove();
  const empty = picker.querySelector('.image-empty');
  if (empty) empty.hidden = false;
  const value = picker.querySelector('[data-image-value]').value.trim();
  const url = value ? (picker.dataset.imagePrefix || '') + value : picker.dataset.imageDefault;
  const preview = picker.querySelector('[data-image-preview]');
  preview.hidden = !url;
  if (url) preview.src = url;
  else preview.removeAttribute('src');
}

document.addEventListener('click', event => {
  const upload = event.target.closest('[data-image-upload]');
  if (upload && !upload.form.dataset.uploading) {
    upload.closest('[data-image-picker]').querySelector('input[type="file"]').click();
  }
  const browse = event.target.closest('[data-media-browse]');
  if (browse && !browse.form.dataset.uploading) {
    const picker = browse.closest('[data-image-picker]');
    openMediaLibrary(path => {
      if (!picker.isConnected || browse.form.dataset.uploading) return;
      const input = picker.querySelector('[data-image-value]');
      input.value = path.slice((picker.dataset.imagePrefix || '').length);
      input.dispatchEvent(new Event('input', {bubbles: true}));
      input.dispatchEvent(new Event('change', {bubbles: true}));
      picker.querySelector('input[type="file"]').value = '';
      setMediaStatus(picker.querySelector('[data-image-status]'), 'Image selected. Save to apply.');
    });
  }
  const remove = event.target.closest('[data-image-reset]');
  if (remove) {
    if (remove.form.dataset.uploading) return;
    const picker = remove.closest('[data-image-picker]');
    const input = picker.querySelector('[data-image-value]');
    if (!input.value.trim()) return;
    input.value = '';
    input.dispatchEvent(new Event('input', {bubbles: true}));
    picker.querySelector('input[type="file"]').value = '';
    updateImagePreview(picker);
    setMediaStatus(picker.querySelector('[data-image-status]'), 'Selection cleared. Save to apply.');
  }

});

// Pickers share the editor's validated upload endpoint. Serialize uploads within
// each form so Save cannot race another image selection or submit incomplete URLs.
document.addEventListener('change', event => {
  const input = event.target;
  if (input.matches('[data-image-value]')) {
    updateImagePreview(input.closest('[data-image-picker]'));
    setMediaStatus(input.closest('[data-image-picker]').querySelector('[data-image-status]'), '');
    return;
  }
  if (!input.matches('[data-image-picker] input[type="file"]') || !input.files.length) return;
  uploadPickerImage(input.closest('[data-image-picker]'), input.files[0]);
  input.value = '';
});

async function uploadPickerImage(picker, file) {
  const input = picker.querySelector('input[type="file"]');
  const form = input.form;
  if (!picker.isConnected || form.dataset.uploading) return;
  const status = picker.querySelector('[data-image-status]');
  if (file.size > 8 * 1024 * 1024) {
    setMediaStatus(status, 'Choose an image of at most 8 MiB. The current image is unchanged.', true);
    input.value = '';
    return;
  }
  form.dataset.uploading = 'true';
  const controls = [...form.querySelectorAll('[data-image-picker] input, [data-image-reset], [data-image-upload], [data-media-browse], [data-upload-retry], [type="submit"]')].filter(control => !control.disabled);
  controls.forEach(control => { control.disabled = true; });
  setMediaStatus(status, 'Uploading image…');
  try {
    const path = await uploadImage(file);
    picker.querySelector('[data-image-value]').value = path.slice((picker.dataset.imagePrefix || '').length);
    picker.querySelector('[data-image-value]').dispatchEvent(new Event('input', {bubbles: true}));
    updateImagePreview(picker);
    setMediaStatus(status, 'Image uploaded. Save to apply.');
  } catch (error) {
    showUploadFailure(status, error, () => uploadPickerImage(picker, file));
  } finally {
    delete form.dataset.uploading;
    controls.forEach(control => { control.disabled = false; });
    input.value = '';
  }
}

document.addEventListener('submit', event => {
  if (event.target.dataset.uploading) {
    event.preventDefault();
    event.stopImmediatePropagation();
    return;
  }
}, true);

function initialize(root) {
  root.querySelectorAll('.image-upload-controls').forEach(controls => { controls.hidden = false; });
}
document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
