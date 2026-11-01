// A separate native dialog keeps the current editor and its unsaved writing intact.
// HTMX owns browsing requests. JavaScript only connects a choice to the unsaved editor.
export function openMediaLibrary(select) {
  if (document.querySelector('.media-browser')) return;
  const opener = document.activeElement;
  const dialog = document.querySelector('#media-browser-template').content.firstElementChild.cloneNode(true);
  const results = dialog.querySelector('[data-media-results]');
  const status = dialog.querySelector('[data-media-status]');
  const selection = new AbortController();
  let choosing = false;
  let browsing;
  dialog.addEventListener('htmx:beforeRequest', event => {
    if (choosing) { event.preventDefault(); return; }
    browsing = event.detail.xhr;
    results.setAttribute('aria-busy', 'true');
    status.textContent = 'Loading images…';
  });
  dialog.addEventListener('htmx:afterRequest', event => {
    if (event.detail.xhr !== browsing) return;
    results.removeAttribute('aria-busy');
    status.textContent = event.detail.xhr.status >= 200 && event.detail.xhr.status < 300 ? '' : 'Unable to load images. Close the library and try again.';
  });
  dialog.addEventListener('htmx:afterSwap', event => {
    results.scrollTop = 0;
    if (event.detail.requestConfig.elt.matches('[data-media-filters]')) {
      results.querySelector('[data-media-filters] button')?.focus({preventScroll: true});
    }
  });
  dialog.addEventListener('click', async event => {
    const button = event.target.closest('[data-media-select]');
    if (!button || choosing || results.hasAttribute('aria-busy')) return;
    const id = button.dataset.mediaSelect;
    if (!/^[a-f0-9]{64}\.(png|jpeg|gif)$/.test(id)) return;
    choosing = true;
    button.disabled = true;
    status.textContent = 'Checking image…';
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
      if (error.name !== 'AbortError') status.textContent = error.message;
    } finally {
      choosing = false;
      button.disabled = false;
    }
  });
  dialog.addEventListener('close', () => {
    selection.abort();
    htmx.trigger(results, 'htmx:abort');
    htmx.remove(dialog);
    if (opener?.isConnected) opener.focus({preventScroll: true});
  }, {once: true});
  document.body.append(dialog);
  dialog.showModal();
  htmx.process(dialog);
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
