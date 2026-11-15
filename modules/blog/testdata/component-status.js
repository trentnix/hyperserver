(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const paths = __EDITOR_PATHS__;
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const end = Date.now() + 12000;
    while (!predicate()) { if (Date.now() > end) throw new Error('Timed out: ' + predicate); await new Promise(resolve => setTimeout(resolve, 25)); }
  };
  const set = (input, value) => { input.value = value; input.dispatchEvent(new Event('input', {bubbles: true})); };
  const badge = section => section.querySelector('[data-component-unsaved]');
  const dirty = () => [...document.querySelectorAll('[data-component-unsaved]:not([hidden])')];
  const open = async path => {
    await htmx.ajax('GET', path, {target: '#article-content', swap: 'innerHTML'});
    await wait(() => document.querySelector('#post-form .CodeMirror') && !document.querySelector('[data-draft-status]').hidden);
  };
  try {
    const stage = sessionStorage.getItem('component-status-stage');
    if (!stage) {
      const newForm = document.querySelector('#post-form');
      newForm.querySelector('[data-component-add="video"]').click();
      assert(!badge(newForm.querySelector('[data-video-editor]')).hidden, 'An empty newly added component must be marked unsaved.');
      newForm.querySelector('[data-video-editor] [data-component-remove]').click();
      assert(dirty().length === 0, 'Removing a new empty component must clear its indicator.');
      await open(paths.editor);
      let form = document.querySelector('#post-form');
      const sections = [...form.querySelectorAll('[data-post-component]')];
      assert(sections.length === 6 && sections.every(section => !section.hidden) && dirty().length === 0, 'Saved components must start without unsaved indicators.');
      set(form.elements.title, form.elements.title.value + ' edited');
      assert(dirty().length === 0, 'Post fields must not mark unchanged components unsaved.');
      set(form.elements.title, 'Component status');
      for (const section of sections) {
        const input = section.querySelector('[data-album-caption], [data-downloads-list] input, [data-link-controls] input, #video-title, #audio-title');
        const saved = input.value;
        set(input, saved + ' edited');
        assert(!badge(section).hidden && dirty().length === 1, 'Only the edited component should be marked: ' + section.dataset.postComponent);
        const label = badge(section).getBoundingClientRect();
        assert(label.width > 0 && label.left >= 0 && label.right <= innerWidth, 'The component indicator must fit the viewport.');
        set(input, saved);
        assert(dirty().length === 0, 'Restoring saved values must clear the indicator: ' + section.dataset.postComponent);
        section.querySelector('[data-component-remove]').click();
        form.querySelector('[data-component-undo="' + section.dataset.postComponent + '"]').click();
        assert(dirty().length === 0, 'Undoing removal must restore the saved component and position.');
      }
      for (const name of ['video', 'pdf', 'audio']) {
        const source = form.elements.namedItem(name);
        const saved = source.value;
        const invalid = JSON.parse(saved);
        if (name === 'video' || name === 'audio') invalid.title = false;
        else invalid[0].description = false;
        source.value = JSON.stringify(invalid);
        source.dispatchEvent(new Event('change', {bubbles: true}));
        assert(!source.hidden && dirty().length === 1 && form.querySelector('[data-draft-status]').textContent === 'Unsaved changes.', 'Invalid optional field types must remain visible and unsaved: ' + name);
        source.value = saved;
        source.dispatchEvent(new Event('change', {bubbles: true}));
        assert(dirty().length === 0 && form.querySelector('[data-draft-status]').textContent === 'No unsaved changes.', 'Restoring valid source must clear the indicator: ' + name);
      }
      const pdf = form.querySelector('.pdf-editor');
      pdf.querySelector('input[type="checkbox"]').click();
      assert(!badge(pdf).hidden, 'Changing the download option must mark the PDF unsaved.');
      pdf.querySelector('input[type="checkbox"]').click();
      assert(dirty().length === 0, 'Restoring the download option must clear the PDF indicator.');
      const album = form.querySelector('[data-album-editor]');
      album.querySelector('[data-album-remove]').click();
      assert(!badge(album).hidden, 'Removing a photo must mark the album unsaved.');
      album.querySelector('[data-album-undo]').click();
      assert(dirty().length === 0, 'Undoing photo removal must clear the album indicator.');
      const first = form.querySelector('[data-post-component]');
      first.querySelector('[data-component-move="down"]').click();
      assert(dirty().length === 2, 'Changed component positions must be marked unsaved.');
      first.querySelector('[data-component-move="up"]').click();
      assert(dirty().length === 0, 'Restoring component order must clear the indicators.');
      await open(paths.history);
      form = document.querySelector('#post-form');
      assert(form.querySelector('#video-title').value === 'Earlier video' && dirty().length === 1 && !badge(form.querySelector('[data-video-editor]')).hidden, 'A historical version must be compared with the current saved component.');
      set(form.querySelector('#video-title'), '');
      assert(dirty().length === 0, 'Restoring the current values in a historical editor must clear the component indicator.');
      await open(paths.editor);
      form = document.querySelector('#post-form');
      set(form.querySelector('#video-title'), 'Recovered video');
      await wait(() => Object.keys(localStorage).some(key => key.startsWith('hyperserver.blog.draft.v1:') && localStorage.getItem(key).includes('Recovered video')));
      sessionStorage.setItem('component-status-stage', 'recover');
      location.assign(paths.editor);
      return;
    }
    if (stage === 'recover') {
      await wait(() => !document.querySelector('[data-draft-recovery]').hidden);
      assert(dirty().length === 0, 'An available recovery copy must not mark saved components before restoration.');
      document.querySelector('[data-draft-restore]').click();
      const form = document.querySelector('#post-form');
      assert(form.querySelector('#video-title').value === 'Recovered video' && dirty().length === 1, 'Recovered component edits must remain unsaved.');
      set(form.elements.slug, 'Invalid slug');
      // The browser pattern normally blocks this invalid value before the server.
      form.elements.slug.removeAttribute('pattern');
      form.requestSubmit();
      await wait(() => form.querySelector('#editor-errors').textContent.trim());
      assert(dirty().length === 1, 'A rejected save must retain the component indicator.');
      set(form.elements.slug, 'component-status');
      document.addEventListener('htmx:before:response', event => {
        if (event.detail.ctx.response.headers.get('HX-Trigger') === 'blog:content-saved') {
          sessionStorage.setItem('component-status-saved-clean', String(dirty().length === 0));
        }
      });
      sessionStorage.setItem('component-status-stage', 'saved');
      form.requestSubmit();
      return;
    }
    if (stage === 'saved') {
      assert(sessionStorage.getItem('component-status-saved-clean') === 'true', 'A confirmed save must clear the component indicators.');
      await open(paths.editor);
      assert(document.querySelector('#video-title').value === 'Recovered video' && dirty().length === 0, 'Saved components must reopen without unsaved indicators.');
      // Submit a native form from a clean editor to exercise a full-page error.
      const form = document.querySelector('#post-form');
      const submission = document.createElement('form');
      submission.method = 'post';
      submission.action = form.action;
      const values = new FormData(form);
      values.set('slug', 'Invalid slug');
      const video = JSON.parse(values.get('video'));
      video.title = 'Rejected video edit';
      values.set('video', JSON.stringify(video));
      for (const [name, value] of values) {
        if (typeof value !== 'string') continue;
        const input = document.createElement('input');
        input.type = 'hidden'; input.name = name; input.value = value;
        submission.append(input);
      }
      document.body.append(submission);
      sessionStorage.setItem('component-status-stage', 'rejected');
      submission.submit();
      return;
    }
    if (stage === 'rejected') {
      await wait(() => document.querySelector('#post-form .CodeMirror') && !document.querySelector('[data-draft-status]').hidden);
      const form = document.querySelector('#post-form');
      const video = form.querySelector('[data-video-editor]');
      assert(form.hasAttribute('data-unsaved') && form.elements.slug.value === 'Invalid slug' && form.querySelector('#video-title').value === 'Rejected video edit', 'Ordinary validation must retain submitted fields.');
      assert(dirty().length === 1 && !badge(video).hidden, 'Ordinary validation must not mark unchanged components unsaved.');
      set(form.querySelector('#video-title'), 'Recovered video');
      assert(dirty().length === 0 && form.querySelector('[data-draft-status]').textContent === 'Unsaved changes.', 'Restoring the saved component must clear its indicator without treating the rejected form as saved.');
      await report('PASS');
    }
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
