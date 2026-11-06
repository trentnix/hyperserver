import {openMediaLibrary, uploadImage, showUploadFailure, setMediaStatus} from './media.js';
import {markField} from './forms.js';
import {createMarkdownEditor, getMarkdownEditor} from './markdown-editor.js';

document.addEventListener('blog:field-validation', event => getMarkdownEditor(event.target)?.syncValidation());

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
    if (field.hidden) markField(input, false);
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
  if (textarea && !getMarkdownEditor(textarea)) {
    let previewRequest;
    createMarkdownEditor(textarea, {
      upload(file, onSuccess, status) {
        const attempt = async () => {
          if (!textarea.isConnected) return;
          setMediaStatus(status, 'Uploading ' + file.name + '…');
          try {
            const path = await uploadImage(file);
            if (!textarea.isConnected) return;
            onSuccess(path);
            status.remove();
          } catch (error) {
            if (textarea.isConnected) showUploadFailure(status, error, attempt);
          }
        };
        attempt();
      },
      chooseImage: openMediaLibrary,
      preview(markdown, preview) {
        previewRequest?.abort();
        previewRequest = new AbortController();
        const request = previewRequest;
        fetch('/post/preview', {
          method: 'POST', body: new URLSearchParams({content: markdown}), signal: request.signal
        }).then(async response => {
          const html = await response.text();
          if (!response.ok) throw new Error('Unable to preview this content.');
          // The server uses the same safe Markdown renderer as published posts.
          if (!request.signal.aborted && textarea.isConnected) preview.innerHTML = html;
        }).catch(error => {
          if (!request.signal.aborted && textarea.isConnected && error.name !== 'AbortError') preview.textContent = 'Unable to preview this content.';
        });
        return 'Loading preview…';
      },
      onDestroy: () => previewRequest?.abort()
    });
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
document.addEventListener('htmx:before:cleanup', event => {
  const textarea = event.target.querySelector('#post-content');
  getMarkdownEditor(textarea)?.destroy();
});

document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
