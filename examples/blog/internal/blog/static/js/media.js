// A separate native dialog keeps the current editor and its unsaved writing intact.
// Cancel pending reads when paging or closing. Selection never uploads another copy.
export function openMediaLibrary(select) {
  if (document.querySelector('.media-browser')) return;
  const opener = document.activeElement;
  const dialog = document.createElement('dialog');
  dialog.className = 'media-browser';
  dialog.setAttribute('aria-labelledby', 'media-browser-title');
  dialog.innerHTML = '<header class="page-manager-heading"><h2 id="media-browser-title">Choose an image</h2><button type="button" data-close-dialog autofocus>Cancel</button></header><p role="status" data-media-status></p><div data-media-results></div>';
  const results = dialog.querySelector('[data-media-results]');
  const status = dialog.querySelector('[data-media-status]');
  let request;
  let choosing = false;
  let loading = false;
  async function load(path, focusSearch = false) {
    request?.abort();
    const current = new AbortController();
    request = current;
    loading = true;
    results.setAttribute('aria-busy', 'true');
    status.textContent = 'Loading images…';
    try {
      const response = await fetch(path, {headers: {'HX-Request': 'true'}, signal: current.signal});
      if (!response.ok) throw new Error('Unable to load images. Close the library and try again.');
      const document = new DOMParser().parseFromString(await response.text(), 'text/html');
      const library = document.querySelector('[data-media-library]');
      if (!library) throw new Error('Unable to load images. Sign in again if your session has expired.');
      if (!dialog.open || current !== request || current.signal.aborted) return;
      results.replaceChildren(library);
      status.textContent = '';
      results.scrollTop = 0;
      if (focusSearch) library.querySelector('[data-media-filters] button').focus({preventScroll: true});
    } catch (error) {
      if (error.name !== 'AbortError' && current === request && dialog.open) status.textContent = error.message;
    } finally {
      if (current === request) {
        loading = false;
        results.removeAttribute('aria-busy');
      }
    }
  }
  dialog.addEventListener('submit', event => {
    if (!event.target.matches('[data-media-filters]')) return;
    event.preventDefault();
    if (!choosing) load('/media?' + new URLSearchParams(new FormData(event.target)), true);
  });
  dialog.addEventListener('click', async event => {
    const page = event.target.closest('[data-media-page]');
    if (page) {
      event.preventDefault();
      if (!choosing) load(page.dataset.mediaPage);
      return;
    }
    const button = event.target.closest('[data-media-select]');
    if (!button || choosing || loading) return;
    const id = button.dataset.mediaSelect;
    if (!/^[a-f0-9]{64}\.(png|jpeg|gif)$/.test(id)) return;
    choosing = true;
    button.disabled = true;
    status.textContent = 'Checking image…';
    try {
      // Cleanup can remove an image after the library was opened. Saving also
      // checks references, because choosing an image does not reserve it.
      const path = '/images/' + id;
      const response = await fetch(path, {method: 'HEAD', signal: request.signal});
      if (response.status === 404) throw new Error('This image is no longer available. Choose another image or reopen the library.');
      if (!response.ok) throw new Error('Unable to check this image. Try again, or sign in again if your session has expired.');
      if (!dialog.open) return;
      select(path);
      dialog.close();
    } catch (error) {
      if (error.name !== 'AbortError') status.textContent = error.message;
    } finally {
      choosing = false;
      button.disabled = false;
    }
  });
  dialog.addEventListener('close', () => {
    request?.abort();
    dialog.remove();
    if (opener?.isConnected) opener.focus({preventScroll: true});
  }, {once: true});
  document.body.append(dialog);
  dialog.showModal();
  load('/media?pick=1');
}

// The library and profile controls use the same validated upload endpoint as the editor.
async function uploadImage(file) {
  if (file.size > 8 * 1024 * 1024) throw new Error('Choose an image of at most 8 MiB.');
  const data = new FormData();
  data.append('image', file);
  const response = await fetch('/post/images/upload', {method: 'POST', body: data});
  if (!(response.headers.get('Content-Type') || '').includes('application/json')) throw new Error('Unable to upload the image.');
  const result = await response.json();
  if (!response.ok) throw new Error(result?.error || 'Unable to upload the image.');
  const path = result?.data?.filePath;
  if (typeof path !== 'string' || !/^\/images\/[a-f0-9]{64}\.(png|jpeg|gif)$/.test(path)) throw new Error('Invalid image upload response.');
  return path;
}

document.addEventListener('submit', async event => {
  const form = event.target;
  if (!form.matches('[data-media-upload]')) return;
  event.preventDefault();
  if (form.dataset.uploading) return;
  const input = form.elements.image;
  if (!input.files.length) return;
  const button = form.querySelector('[type=submit]');
  const status = form.querySelector('[data-upload-status]');
  form.dataset.uploading = 'true';
  input.disabled = button.disabled = true;
  status.textContent = 'Uploading image…';
  try {
    const path = await uploadImage(input.files[0]);
    location.assign('/media/' + path.slice('/images/'.length));
  } catch (error) {
    status.textContent = error.message;
  } finally {
    delete form.dataset.uploading;
    input.disabled = button.disabled = false;
  }
});

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
      picker.querySelector('[data-image-status]').textContent = 'Image selected. Save to apply.';
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
    picker.querySelector('[data-image-status]').textContent = 'Selection cleared. Save to apply.';
  }

});

// Pickers share the editor's validated upload endpoint. Serialize uploads within
// each form so Save cannot race another image selection or submit incomplete URLs.
document.addEventListener('change', async event => {
  const input = event.target;
  if (input.matches('[data-image-value]')) {
    updateImagePreview(input.closest('[data-image-picker]'));
    return;
  }
  if (!input.matches('[data-image-picker] input[type="file"]') || !input.files.length) return;
  const picker = input.closest('[data-image-picker]');
  const form = input.form;
  if (form.dataset.uploading) {
    input.value = '';
    return;
  }
  const status = picker.querySelector('[data-image-status]');
  const file = input.files[0];
  if (file.size > 8 * 1024 * 1024) {
    status.textContent = 'Choose an image of at most 8 MiB. The current image is unchanged.';
    input.value = '';
    return;
  }
  form.dataset.uploading = 'true';
  const controls = [...form.querySelectorAll('[data-image-picker] input, [data-image-reset], [data-image-upload], [data-media-browse], [type="submit"]')].filter(control => !control.disabled);
  controls.forEach(control => { control.disabled = true; });
  status.textContent = 'Uploading image…';
  try {
    const path = await uploadImage(file);
    picker.querySelector('[data-image-value]').value = path.slice((picker.dataset.imagePrefix || '').length);
    picker.querySelector('[data-image-value]').dispatchEvent(new Event('input', {bubbles: true}));
    updateImagePreview(picker);
    status.textContent = 'Image uploaded. Save to apply.';
  } catch (error) {
    status.textContent = error.message + ' The current image is unchanged.';
  } finally {
    delete form.dataset.uploading;
    controls.forEach(control => { control.disabled = false; });
    input.value = '';
  }
});

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
document.addEventListener('htmx:afterSwap', event => initialize(event.target));
initialize(document);
