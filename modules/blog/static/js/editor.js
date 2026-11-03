import {openMediaLibrary, uploadImage, showUploadFailure} from './media.js';

const editors = new WeakMap();

// Native suggestions replace the last comma-separated tag, keeping earlier tags.
// Without JavaScript, the same field still accepts ordinary comma-separated input.
const tagChoices = new WeakMap();
function updateTagSuggestions(input) {
  if (!tagChoices.has(input)) tagChoices.set(input, [...input.list.options].map(option => option.dataset.tag));
  const prefix = input.value.slice(0, input.value.lastIndexOf(',') + 1);
  const used = prefix.split(',').map(tag => tag.trim().normalize('NFC').toLowerCase());
  input.list.replaceChildren(...tagChoices.get(input).filter(tag => !used.includes(tag)).map(tag => {
    const option = new Option(tag, prefix ? prefix.trimEnd() + ' ' + tag : tag);
    option.dataset.tag = tag;
    return option;
  }));
}

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
  const button = form?.querySelector('[data-publication-save]');
  if (button) {
    const choice = form.elements.published.value;
    button.textContent = choice === 'scheduled' ? button.dataset.scheduleLabel :
      choice === 'on' ? button.dataset.publishLabel : button.dataset.draftLabel;
  }
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
  // the page to the first option. Returning focus must not move it either.
  choices.closest('details').open = false;
  choices.value = '';
  input.dispatchEvent(new Event('input', {bubbles: true}));
  // Return to the toggle, not the text input, so touch selection does not
  // summon the mobile keyboard and reposition the page.
  choices.closest('details').querySelector('summary').focus({preventScroll: true});
});
document.addEventListener('click', event => {
  const toggle = event.target.closest('.post-time-menu summary');
  if (!toggle || toggle.parentElement.open) return;
  const menu = toggle.parentElement;
  const field = menu.closest('.post-time-field').getBoundingClientRect();
  const above = field.top;
  const below = innerHeight - field.bottom;
  const opensBelow = above < Math.min(200, below);
  // Keep options inside the scroll viewport. Native option clicks otherwise
  // scroll the page to expose the clipped part of the list.
  menu.classList.toggle('post-time-menu-below', opensBelow);
  menu.style.setProperty('--time-menu-height', Math.max(44, (opensBelow ? below : above) - 8) + 'px');
});
for (const name of ['input', 'change']) {
  document.addEventListener(name, event => {
    const form = event.target.closest('form');
    updateSlugPlaceholder(form);
    updatePublication(form);
    if (event.target.matches('input[name="tags"]')) updateTagSuggestions(event.target);
  });
}

function initialize(root) {
  root.querySelectorAll('#post-form, #page-form').forEach(updatePublication);
  root.querySelectorAll('#post-form, #page-form').forEach(updateSlugPlaceholder);
  root.querySelectorAll('input[name="tags"]').forEach(updateTagSuggestions);
  const textarea = root.querySelector('#post-content');
  if (textarea && window.EasyMDE && !editors.has(textarea)) {
    // EasyMDE replaces native textarea validation. The server remains authoritative.
    const required = textarea.required;
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
      imageUploadFunction(file, onSuccess) {
        const status = document.createElement('p');
        status.setAttribute('role', 'status');
        editor.codemirror.getWrapperElement().after(status);
        const attempt = async () => {
          if (!textarea.isConnected) return;
          status.textContent = 'Uploading ' + file.name + '…';
          try {
            const path = await uploadImage(file);
            if (!textarea.isConnected) return;
            onSuccess(path);
            status.remove();
          } catch (error) {
            showUploadFailure(status, error, attempt);
          }
        };
        attempt();
      },
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
    // Preserve the textarea's label/help and let Tab leave the rich editor.
    const input = editor.codemirror.getInputField();
    input.id = 'markdown-input';
    input.setAttribute('aria-labelledby', 'markdown-label');
    input.setAttribute('aria-describedby', textarea.getAttribute('aria-describedby'));
    if (required) input.setAttribute('aria-required', 'true');
    root.querySelector('#markdown-label').htmlFor = input.id;
    editor.codemirror.setOption('extraKeys', {...editor.codemirror.getOption('extraKeys'), Tab: false, 'Shift-Tab': false});
    editors.set(textarea, {editor, cancelPreview: () => previewRequest?.abort()});
    editor.codemirror.on('change', () => textarea.dispatchEvent(new Event('input', {bubbles: true})));
  }
}

const approvedSlugs = new WeakMap();
document.addEventListener('submit', event => {
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

document.addEventListener('htmx:afterSwap', event => initialize(event.target));
initialize(document);
