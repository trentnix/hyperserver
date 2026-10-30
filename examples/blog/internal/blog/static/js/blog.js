/* EasyMDE owns editing, toolbar actions, preview presentation, and image uploads.
 * The server validates posts and renders Markdown for both preview and publication. */
(() => {
  const editors = new WeakMap();
  document.documentElement.classList.add('js');

  // Move one archive to keep visual and keyboard order aligned without reloading it.
  // This breakpoint matches the sidebar layout in blog.css.
  const sidebarLayout = matchMedia('(min-width: 1680px)');
  const archive = document.querySelector('#post-archive');
  const sidebar = document.querySelector('.site-sidebar');
  const footer = document.querySelector('.site-footer');
  function positionArchive() {
    if (sidebarLayout.matches) sidebar.append(archive);
    else footer.prepend(archive);
  }
  sidebarLayout.addEventListener('change', positionArchive);
  positionArchive();

  // Keep ordinary links available without JavaScript. Only mobile collapses them.
  const mobileLayout = matchMedia('(max-width: 767px)');
  const menuToggle = document.querySelector('.menu-toggle');
  const menu = document.querySelector('#site-menu');
  const childToggles = [...menu.querySelectorAll('.child-toggle')];
  function closeMenu() {
    menu.hidden = mobileLayout.matches;
    menuToggle.hidden = !mobileLayout.matches;
    menuToggle.setAttribute('aria-expanded', String(!menu.hidden));
    for (const toggle of childToggles) {
      const children = document.getElementById(toggle.getAttribute('aria-controls'));
      children.hidden = mobileLayout.matches;
      toggle.hidden = !mobileLayout.matches;
      toggle.setAttribute('aria-expanded', String(!children.hidden));
    }
  }
  for (const toggle of childToggles) {
    toggle.addEventListener('click', () => {
      const children = document.getElementById(toggle.getAttribute('aria-controls'));
      children.hidden = !children.hidden;
      toggle.setAttribute('aria-expanded', String(!children.hidden));
    });
  }
  menuToggle.addEventListener('click', () => {
    menu.hidden = !menu.hidden;
    menuToggle.setAttribute('aria-expanded', String(!menu.hidden));
  });
  menu.addEventListener('click', event => {
    if (mobileLayout.matches && event.target.closest('a')) {
      closeMenu();
      menuToggle.focus({preventScroll: true});
    }
  });
  document.querySelector('.header-menu').addEventListener('keydown', event => {
    if (event.key === 'Escape' && mobileLayout.matches && !menu.hidden) {
      closeMenu();
      menuToggle.focus();
    }
  });
  mobileLayout.addEventListener('change', closeMenu);
  closeMenu();

  // Keep one set of admin links: expanded on desktop, a disclosure on mobile.
  const accountMenu = document.querySelector('.account-menu');
  function positionAccountMenu() {
    if (!accountMenu) return;
    const summary = accountMenu.querySelector('summary');
    const focused = document.activeElement;
    const hadFocus = accountMenu.contains(focused);
    accountMenu.open = !mobileLayout.matches;
    summary.hidden = !mobileLayout.matches;
    if (mobileLayout.matches && hadFocus) summary.focus({preventScroll: true});
    else if (focused === summary) accountMenu.querySelector('a').focus({preventScroll: true});
  }
  mobileLayout.addEventListener('change', positionAccountMenu);
  positionAccountMenu();

  // Native disclosure controls work without JavaScript. Add familiar dismissal
  // behavior, including for action menus inserted by HTMX navigation.
  document.addEventListener('click', event => {
    for (const menu of document.querySelectorAll('.action-menu[open], .post-time-menu[open]')) {
      if (menu.querySelector('summary').hidden) continue;
      if (!menu.contains(event.target) || event.target.closest('a, button')) menu.open = false;
    }
  });
  document.addEventListener('keydown', event => {
    if (event.key !== 'Escape') return;
    const menu = event.target.closest('.action-menu[open], .post-time-menu[open]');
    if (menu && !menu.querySelector('summary').hidden) {
      menu.open = false;
      menu.querySelector('summary').focus({preventScroll: true});
      event.preventDefault();
    }
  });
  document.addEventListener('focusin', event => {
    for (const menu of document.querySelectorAll('.post-time-menu[open]')) {
      if (!menu.contains(event.target)) menu.open = false;
    }
  });

  // CSS owns timing, including hover/focus pauses and reduced-motion preferences.
  // Delegation also handles notifications appended by HTMX out-of-band swaps.
  document.addEventListener('animationend', event => {
    if (event.animationName === 'notification-fade' && event.target.matches('.notification--success')) {
      event.target.remove();
    }
  });

  // This is a suggestion, not a reservation. The server resolves collisions on save.
  function updateSlugPlaceholder(form) {
    const slug = form?.querySelector('input[name="slug"]');
    if (!slug) return;
    const title = form.elements.title.value;
    const suggestion = title.replace(/[^A-Za-z0-9]+/g, '-').toLowerCase()
      .replace(/^-+/, '').slice(0, 120).replace(/-+$/, '');
    slug.placeholder = suggestion || (title.trim() ? 'Enter a custom slug' : 'Generated from title on save');
  }
  function updatePublication(form) {
    const field = form?.querySelector('[data-publication-time]');
    if (!field) return;
    field.hidden = form.elements.published.value !== 'scheduled';
    for (const input of field.querySelectorAll('input')) {
      input.disabled = field.hidden;
      input.required = !field.hidden;
    }
    const menu = field.querySelector('.post-time-menu');
    menu.hidden = false;
    if (field.hidden) menu.open = false;
  }
  document.addEventListener('change', event => {
    const choices = event.target.closest('[data-time-choice]');
    if (!choices?.value) return;
    const input = choices.form.elements.publishTime;
    input.value = choices.value;
    // Hide the list before resetting its selection so the browser cannot scroll
    // the dialog to the first option. Returning focus must not move it either.
    choices.closest('details').open = false;
    choices.value = '';
    input.dispatchEvent(new Event('input', {bubbles: true}));
    // Return to the toggle, not the text input, so touch selection does not
    // summon the mobile keyboard and reposition the dialog.
    choices.closest('details').querySelector('summary').focus({preventScroll: true});
  });
  document.addEventListener('click', event => {
    const toggle = event.target.closest('.post-time-menu summary');
    if (!toggle || toggle.parentElement.open) return;
    const menu = toggle.parentElement;
    const field = menu.closest('.post-time-field').getBoundingClientRect();
    const dialog = menu.closest('dialog')?.getBoundingClientRect();
    const above = field.top - Math.max(0, dialog?.top ?? 0);
    const below = Math.min(innerHeight, dialog?.bottom ?? innerHeight) - field.bottom;
    const opensBelow = above < Math.min(200, below);
    // Keep options inside the scroll viewport. Native option clicks otherwise
    // scroll the dialog to expose the clipped part of the list.
    menu.classList.toggle('post-time-menu-below', opensBelow);
    menu.style.setProperty('--time-menu-height', Math.max(44, (opensBelow ? below : above) - 8) + 'px');
  });
  for (const name of ['input', 'change']) {
    document.addEventListener(name, event => {
      const form = event.target.closest('form');
      updateSlugPlaceholder(form);
      updatePublication(form);
    });
  }

  function initialize(root) {
    root.querySelectorAll('#post-form').forEach(updatePublication);
    root.querySelectorAll('#post-form, #page-form').forEach(updateSlugPlaceholder);
    root.querySelectorAll('dialog:not([open])').forEach(dialog => dialog.showModal());
    root.querySelectorAll('.image-upload-controls').forEach(controls => { controls.hidden = false; });
    root.querySelectorAll('[data-media-upload]').forEach(form => { form.hidden = false; });
    const textarea = root.querySelector('#post-content');
    if (textarea && window.EasyMDE && !editors.has(textarea)) {
      // EasyMDE replaces native textarea validation. The server remains authoritative.
      textarea.required = false;
      let previewRequest;
      const editor = new EasyMDE({
        element: textarea,
        autoDownloadFontAwesome: false,
        spellChecker: false,
        forceSync: true,
        status: false,
        uploadImage: true,
        imageUploadEndpoint: '/post/images/upload',
        imagePathAbsolute: true,
        imageMaxSize: 8 * 1024 * 1024,
        imageAccept: 'image/png,image/jpeg,image/gif',
        toolbar: [
          {name: 'bold', action: EasyMDE.toggleBold, className: 'tool-bold', title: 'Bold'},
          {name: 'italic', action: EasyMDE.toggleItalic, className: 'tool-italic', title: 'Italic'},
          {name: 'heading', action: EasyMDE.toggleHeadingSmaller, className: 'tool-heading', title: 'Heading'},
          '|',
          {name: 'quote', action: EasyMDE.toggleBlockquote, className: 'tool-quote', title: 'Quote'},
          {name: 'unordered-list', action: EasyMDE.toggleUnorderedList, className: 'tool-list', title: 'List'},
          {name: 'link', action: EasyMDE.drawLink, className: 'tool-link', title: 'Link'},
          {name: 'upload-image', action: EasyMDE.drawUploadedImage, className: 'tool-image', title: 'Upload image'},
          {name: 'media-library', action: () => openMediaLibrary(path => {
            if (textarea.isConnected) editor.codemirror.replaceSelection(`![](${path})`);
          }), className: 'tool-library', title: 'Choose existing image'},
          '|',
          {name: 'preview', action: EasyMDE.togglePreview, className: 'tool-preview', title: 'Preview'}
        ],
        previewRender(markdown, preview) {
          previewRequest?.abort();
          previewRequest = new AbortController();
          fetch('/post/preview', {
            method: 'POST', body: new URLSearchParams({content: markdown}), signal: previewRequest.signal
          }).then(async response => {
            const html = await response.text();
            if (!response.ok) throw new Error('Unable to preview this content.');
            // The server uses the same safe Markdown renderer as published posts.
            preview.innerHTML = html;
          }).catch(error => {
            if (error.name !== 'AbortError') preview.textContent = 'Unable to preview this content.';
          });
          return 'Loading preview…';
        }
      });
      editors.set(textarea, {editor, cancelPreview: () => previewRequest?.abort()});
      editor.codemirror.on('change', () => textarea.dispatchEvent(new Event('input', {bubbles: true})));
    }
  }

  // A separate native dialog keeps the current editor and its unsaved writing intact.
  // Cancel pending reads when paging or closing. Selection never uploads another copy.
  function openMediaLibrary(select) {
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
    const dismiss = event.target.closest('[data-dismiss-notification]');
    if (dismiss) dismiss.closest('.notification').remove();
    const close = event.target.closest('[data-close-dialog]');
    if (close) close.closest('dialog').close();
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
  const approvedSlugs = new WeakMap();
  document.addEventListener('submit', event => {
    if (event.target.dataset.uploading) {
      event.preventDefault();
      event.stopImmediatePropagation();
      return;
    }
    const slug = event.target.querySelector('[data-published-slug], [data-scheduled-slug]');
    let publishedSlug = slug?.dataset.publishedSlug;
    // A scheduled post can become public while its editor remains open.
    if (slug?.dataset.scheduledSlug !== undefined && Date.now() >= Number(slug.dataset.scheduledAt) * 1000) {
      publishedSlug = slug.dataset.scheduledSlug;
    }
    const approved = approvedSlugs.get(event.target);
    approvedSlugs.delete(event.target);
    if (publishedSlug !== undefined && slug.value.trim() !== publishedSlug && approved !== slug.value) {
      event.preventDefault();
      event.stopImmediatePropagation();
      const form = event.target;
      const value = slug.value;
      window.blogConfirm({title: 'Change this URL?', message: 'Changing this URL can break existing links to this content.', action: 'Save with new URL', cancel: 'Keep editing'}, () => {
        if (!form.isConnected || slug.value !== value) return;
        approvedSlugs.set(form, value);
        form.requestSubmit(event.submitter ?? undefined);
      });
    }
  }, true);
  document.addEventListener('htmx:beforeCleanupElement', event => {
    const textarea = event.detail.elt;
    const state = editors.get(textarea);
    if (state) {
      state.cancelPreview();
      state.editor.cleanup();
      state.editor.toTextArea();
      editors.delete(textarea);
    }
  });

  function showRequestError(detail, message) {
    // Keep failures inside an open modal or the submitting form without replacing its inputs.
    const container = detail.elt?.closest('form') || detail.target?.closest('form') || detail.target?.querySelector('form') ||
      document.querySelector('dialog[open]') || document.querySelector('#notifications');
    let notice = container.querySelector('[data-request-error]');
    if (!notice) {
      notice = document.createElement('p');
      notice.dataset.requestError = '';
      notice.className = 'field-error';
      notice.setAttribute('role', 'alert');
      container.prepend(notice);
    }
    notice.textContent = message;
  }

  // Render styled HTML errors in their existing target. Never insert plain-text errors as HTML.
  document.addEventListener('htmx:beforeSwap', event => {
    const {xhr} = event.detail;
    if (xhr.status < 400 || xhr.status > 599) return;
    const type = (xhr.getResponseHeader('Content-Type') || '').split(';')[0].trim().toLowerCase();
    if (type === 'text/html') {
      event.detail.shouldSwap = true;
      event.detail.isError = false;
    } else {
      event.detail.shouldSwap = false;
      const message = type === 'text/plain' ? xhr.responseText.trim() : '';
      showRequestError(event.detail, message || `Request failed (HTTP ${xhr.status}). Please try again.`);
    }
  });
  for (const name of ['htmx:sendError', 'htmx:timeout']) {
    document.addEventListener(name, event => showRequestError(event.detail,
      'The connection failed. Check whether the operation completed before retrying.'));
  }
  document.addEventListener('blog:revision-deleted', event => {
    const dialog = event.target.closest('dialog');
    if (dialog) {
      dialog.close();
      dialog.remove();
    }
    for (const link of document.querySelectorAll('.revision-list a')) {
      if (link.dataset.revision === event.detail.revision) link.closest('li').remove();
    }
    htmx.ajax('GET', event.detail.next, {target: '#revision-preview', swap: 'innerHTML show:none'});
  });

  // History swaps omit detail.target but still dispatch on the swapped element.
  document.addEventListener('htmx:afterSwap', event => {
    initialize(event.target);
    if (event.target.id !== 'revision-preview') return;
    const selected = event.target.querySelector('[data-revision]')?.dataset.revision;
    if (!selected) return;
    // Keep the list in place. Only mark a version after its preview arrives.
    for (const link of document.querySelectorAll('.revision-list a')) {
      if (link.dataset.revision === selected) link.setAttribute('aria-current', 'true');
      else link.removeAttribute('aria-current');
    }
  });
  initialize(document);
})();
