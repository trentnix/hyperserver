import {registerComponent} from './post-components.js';
import {openMediaLibrary, uploadImage, runFormUpload, setMediaStatus, showUploadFailure} from './media.js';
import {markField} from './forms.js';

const initialized = new WeakSet();
const imageID = /^[a-f0-9]{64}\.(png|jpeg|gif)$/;

function element(tag, text, className) {
  const node = document.createElement(tag);
  if (text) node.textContent = text;
  if (className) node.className = className;
  return node;
}

function button(text, action) {
  const node = element('button', text, 'button-secondary');
  node.type = 'button';
  node.addEventListener('click', action);
  return node;
}

function validSource(value) {
  return value && typeof value === 'object' && !Array.isArray(value) && Object.keys(value).every(key => ['url', 'title', 'description', 'siteName', 'imageID', 'hideImage'].includes(key)) && ['url', 'title', 'description', 'siteName', 'imageID'].every(key => value[key] === undefined || typeof value[key] === 'string') &&
    (value.hideImage === undefined || typeof value.hideImage === 'boolean') && (!value.imageID || imageID.test(value.imageID));
}

function initialize(root) {
  root.querySelectorAll('[data-link-editor]').forEach(section => {
    if (initialized.has(section)) return;
    initialized.add(section);
    const source = section.querySelector('[name="link"]');
    const form = source.form;
    const controls = section.querySelector('[data-link-controls]');
    const status = section.querySelector('[data-link-status]');
    let link = {};
    let request;
    const lifetime = new AbortController();
    const fields = {};
    for (const [key, title, limit] of [['url', 'URL', 2048], ['title', 'Title', 240], ['description', 'Description (optional)', 1000], ['siteName', 'Site name (optional)', 200]]) {
      const label = element('label', title);
      const input = element(key === 'description' ? 'textarea' : 'input');
      input.id = 'link-' + key;
      input.maxLength = limit;
      if (key === 'url') input.type = 'url';
      if (key === 'description') input.rows = 2;
      if (key === 'url' || key === 'title') input.required = true;
      label.append(input);
      fields[key] = input;
      input.addEventListener('input', () => {
        link[key] = input.value;
        markField(input, false);
        serialize();
        render();
      });
      controls.append(label);
    }
    fields.url.placeholder = 'https://example.com/article';
    const fetchButton = button('Fetch preview', () => {
      if (!fields.url.reportValidity()) return;
      if (link.title || link.description || link.siteName || link.imageID) {
        window.blogConfirm({title: 'Replace preview details?', message: 'Fetching again replaces the title, description, site name, and image. Your post text stays as it is.', action: 'Replace details', cancel: 'Keep editing'}, fetchPreview);
      } else fetchPreview();
    });
    fetchButton.dataset.linkFetch = '';
    fields.url.parentElement.after(fetchButton);
    const imageControls = element('div', '', 'link-image-controls');
    const toggle = element('label', '', 'link-image-toggle');
    const showImage = element('input');
    showImage.type = 'checkbox';
    showImage.addEventListener('change', () => { link.hideImage = !showImage.checked; serialize(); render(); });
    toggle.append(showImage, document.createTextNode('Show image'));
    const uploadInput = element('input');
    uploadInput.type = 'file';
    uploadInput.accept = 'image/png,image/jpeg,image/gif';
    uploadInput.hidden = true;
    const uploadButton = button('Upload image', () => uploadInput.click());
    uploadInput.addEventListener('change', () => {
      const file = uploadInput.files[0];
      uploadInput.value = '';
      if (!file) return;
      runFormUpload(form, async () => {
        try {
          const path = await uploadImage(file);
          if (!section.isConnected) return;
          link.imageID = path.split('/').pop();
          link.hideImage = false;
          serialize(); render();
          setMediaStatus(status, 'Image added. Save the post to apply changes.');
        } catch (error) { if (section.isConnected) showUploadFailure(status, error, () => uploadButton.click()); }
      });
    });
    imageControls.append(uploadButton, uploadInput);
    if (form.dataset.mediaLibrary !== 'false') {
      imageControls.append(button('Choose image', () => openMediaLibrary(path => {
        if (!section.isConnected) return;
        link.imageID = path.split('/').pop(); link.hideImage = false;
        serialize(); render();
      })));
    }
    const removeImage = button('Remove image', () => { delete link.imageID; delete link.hideImage; serialize(); render(); });
    imageControls.append(toggle, removeImage);
    const preview = element('div', '', 'link-editor-preview');
    preview.setAttribute('aria-label', 'Card preview');
    controls.append(imageControls, preview);

    function serialize() {
      source.value = Object.values(link).some(value => value) ? JSON.stringify(link) : '';
      source.dispatchEvent(new Event('input', {bubbles: true}));
    }

    function render() {
      fetchButton.textContent = link.title ? 'Refresh preview' : 'Fetch preview';
      toggle.hidden = removeImage.hidden = !link.imageID;
      showImage.checked = !link.hideImage;
      preview.replaceChildren();
      let destination;
      try { destination = new URL(link.url); } catch { return; }
      if (!['http:', 'https:'].includes(destination.protocol) || destination.username || destination.password) return;
      const card = element('a', '', 'link-preview-card');
      card.href = destination.href;
      card.target = '_blank';
      card.rel = 'noopener';
      card.setAttribute('hx-boost', 'false');
      const details = element('span', '', 'link-preview-details');
      details.append(element('span', destination.hostname, 'link-preview-domain'), element('strong', link.title || destination.hostname, 'link-preview-title'));
      if (link.description) details.append(element('span', link.description, 'link-preview-description'));
      if (link.siteName) details.append(element('span', link.siteName, 'link-preview-site'));
      card.append(details);
      if (link.imageID && !link.hideImage) {
        const image = element('img');
        image.src = '/images/' + link.imageID + '?variant=thumbnail';
        image.alt = '';
        card.append(image);
      }
      preview.append(card);
    }

    function read() {
      request?.abort();
      try {
        const next = source.value.trim() ? JSON.parse(source.value) : {};
        if (!validSource(next)) throw new Error();
        link = next;
      } catch {
        source.hidden = false;
        section.querySelector('[data-link-source-label]').hidden = false;
        controls.hidden = true;
        controls.disabled = true;
        setMediaStatus(status, 'The link preview source is invalid. Correct it before saving.', true);
        return;
      }
      source.hidden = true;
      section.querySelector('[data-link-source-label]').hidden = true;
      controls.hidden = false;
      controls.disabled = section.hidden;
      for (const [key, input] of Object.entries(fields)) input.value = link[key] || '';
      render();
      setMediaStatus(status, '');
    }

    async function fetchPreview() {
      if (!section.isConnected || form.dataset.uploading) return;
      const before = source.value;
      const url = fields.url.value.trim();
      request?.abort();
      request = new AbortController();
      const active = request;
      await runFormUpload(form, async () => {
        setMediaStatus(status, 'Fetching preview…');
        try {
          const response = await fetch('/post/link-preview', {method: 'POST', body: new URLSearchParams({url}), signal: active.signal, redirect: 'error'});
          if (!(response.headers.get('Content-Type') || '').includes('application/json')) throw new Error('Unable to fetch this preview. You can enter the details yourself.');
          const result = await response.json();
          if (!response.ok) throw Object.assign(new Error(result.error || 'Unable to fetch this preview.'), {code: result.code});
          if (!validSource(result.link) || new URL(result.link.url).href !== new URL(url).href) throw new Error('Unable to read the preview. You can enter the details yourself.');
          if (!section.isConnected || active.signal.aborted) return;
          if (source.value !== before) { setMediaStatus(status, 'Your details changed while the preview loaded. Fetch again to replace them.'); return; }
          link = result.link;
          serialize(); read();
          setMediaStatus(status, result.warning || 'Preview ready. Edit the details or save the post.');
        } catch (error) {
          if (section.isConnected && !active.signal.aborted) {
            showUploadFailure(status, error, fetchPreview);
            const retry = status.querySelector('[data-upload-retry]');
            if (retry) retry.textContent = 'Retry preview';
          }
        }
      });
    }

    source.addEventListener('change', read);
    registerComponent(section, {
      snapshot: () => source.value,
      clear: () => { source.value = ''; read(); },
      restore: copy => { source.value = copy; read(); },
      focus: () => (source.hidden ? fields.url : source).focus()
    });
    document.addEventListener('htmx:before:cleanup', event => {
      if (event.target.contains?.(form)) { request?.abort(); lifetime.abort(); }
    }, {signal: lifetime.signal});
    read();
  });
}

document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
