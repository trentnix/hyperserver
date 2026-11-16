(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const wait = async predicate => {
    const end = Date.now() + 12000;
    while (!predicate()) { if (Date.now() > end) throw new Error('Timed out: ' + predicate); await new Promise(resolve => setTimeout(resolve, 25)); }
  };
  const set = (field, value) => { field.value = value; field.dispatchEvent(new Event('input', {bubbles: true})); };
  const title = 'A recording title that wraps on a narrow mobile screen';
  const description = 'Description with <markup> & quotes.\nA second line.';
  const transcript = '**Words from the recording.**\n\nAnother paragraph.\n\n<script>window.unsafeTranscript = true</script>';
  const file = () => new File([Uint8Array.from(atob(testMP3Data), char => char.charCodeAt(0))], 'New recording.mp3', {type: 'audio/mpeg'});
  try {
    const stage = sessionStorage.getItem('audio-stage');
    if (!stage) {
      await wait(() => document.querySelector('[name="audio"]').hidden && document.querySelector('#post-form .CodeMirror'));
      const form = document.querySelector('#post-form');
      const section = form.querySelector('[data-audio-editor]');
      assert(section.hidden && section.querySelector('fieldset').disabled, 'Unused audio must not block validation.');
      form.elements.title.value = 'Audio component review'; form.elements.slug.value = 'audio-component-review';
      form.querySelector('[data-component-add="audio"]').click();
      assert(!section.hidden && document.activeElement === section.querySelector('[data-audio-upload]'), 'Adding audio must focus Upload MP3.');
      form.noValidate = true;
      form.requestSubmit();
      await wait(() => section.querySelector('[data-component-error]:not([hidden])'));
      form.noValidate = false;
      section.querySelector('[data-audio-browse]').click();
      await wait(() => document.querySelector('.download-library input[type="radio"]'));
      const dialog = document.querySelector('.download-library');
      assert(dialog.querySelectorAll('input[type="radio"]').length === 1 && !dialog.textContent.includes('Notes.txt') && !dialog.textContent.includes('preview.pdf'), 'The audio library must show only MP3 files.');
      dialog.querySelector('input[type="radio"]').click();
      dialog.querySelector('.form-actions button:last-child').click();
      assert(!section.querySelector('fieldset').disabled && !section.querySelector('audio').hidden, 'Selecting an MP3 must enable fields and preview.');
      assert(!form.querySelector('#editor-errors').textContent.trim() && !section.querySelector('[data-component-error]:not([hidden])'), 'Choosing a valid MP3 must clear both error messages.');
      set(form.querySelector('#audio-title'), title);
      set(form.querySelector('#audio-description'), description);
      set(form.querySelector('#audio-transcript'), transcript);
      section.querySelector('[data-audio-download]').click();
      assert(JSON.parse(form.elements.audio.value).hideDownload && !section.querySelector('[data-component-unsaved]').hidden, 'The download preference must be saved and marked unsaved.');
      const original = form.elements.audio.value;
      let originalName = section.querySelector('[data-audio-name]').textContent;
      assert(originalName.endsWith('.mp3'), 'The selected filename must be visible separately from the optional title.');
      const upload = section.querySelector('[data-audio-file]');
      const transfer = new DataTransfer(); transfer.items.add(file());
      upload.files = transfer.files; upload.dispatchEvent(new Event('change', {bubbles: true}));
      await wait(() => !form.dataset.uploading && section.querySelector('[data-audio-status]').textContent === 'MP3 uploaded.');
      assert(form.querySelector('#audio-title').value === title && form.querySelector('#audio-transcript').value === transcript, 'Replacement uploads must preserve authored fields.');
      originalName = section.querySelector('[data-audio-name]').textContent;
      // Use a different valid recording ID to exercise replacement and Undo.
      const second = new DataTransfer();
      const bytes = Uint8Array.from(atob(testMP3Data), char => char.charCodeAt(0)); bytes[bytes.length - 1] ^= 1;
      second.items.add(new File([bytes], 'Replacement.mp3', {type: 'audio/mpeg'}));
      upload.files = second.files; upload.dispatchEvent(new Event('change', {bubbles: true}));
      await wait(() => !form.dataset.uploading && !section.querySelector('[data-audio-recovery]').hidden);
      assert(form.querySelector('#audio-title').value === title && JSON.parse(form.elements.audio.value).hideDownload, 'Replacing audio must preserve title and download setting.');
      assert(section.querySelector('[data-audio-name]').textContent === 'Replacement.mp3', 'Replacement must display its filename.');
      section.querySelector('[data-audio-recovery] button').click();
      assert(section.querySelector('[data-audio-name]').textContent === originalName, 'Undo must restore the selected filename.');
      assert(form.elements.audio.value === original, 'Undo replacement must restore the full audio source.');
      section.querySelector('[data-component-remove]').click();
      assert(section.hidden && !form.elements.audio.value && section.querySelector('fieldset').disabled, 'Removal must clear and disable audio fields.');
      form.querySelector('[data-component-undo="audio"]').click();
      assert(form.elements.audio.value === original && form.querySelector('#audio-transcript').value === transcript, 'Component Undo must restore all details.');
      form.querySelector('[data-component-add="video"]').click();
      section.querySelector('[data-component-move="down"]').click();
      assert(form.elements.componentOrder.value === '["video","audio"]', 'Audio must use shared component ordering.');
      form.querySelector('[data-video-editor] [data-component-remove]').click();
      assert(!form.elements.content.required, 'An audio-only post must be allowed.');
      form.querySelector('[name="published"][value="on"]').checked = true;
      form.dispatchEvent(new Event('input', {bubbles: true}));
      await wait(() => Object.keys(localStorage).some(key => key.startsWith('hyperserver.blog.draft.v1:') && localStorage.getItem(key).includes('Words from the recording.')));
      sessionStorage.setItem('audio-filename', originalName);
      sessionStorage.setItem('audio-stage', 'recover'); location.reload(); return;
    }
    if (stage === 'recover') {
      await wait(() => !document.querySelector('[data-draft-recovery]').hidden);
      document.querySelector('[data-draft-restore]').click();
      const form = document.querySelector('#post-form');
      assert(form.querySelector('#audio-title').value === title && form.querySelector('#audio-transcript').value === transcript && form.elements.componentOrder.value === '["audio"]', 'Recovery must preserve audio and component order.');
      assert(form.querySelector('[data-audio-name]').textContent === 'Selected MP3', 'Recovered audio must show a readable label while its filename loads.');
      await wait(() => form.querySelector('[data-audio-name]').textContent === sessionStorage.getItem('audio-filename'));
      sessionStorage.setItem('audio-stage', 'reader'); form.requestSubmit(); return;
    }
    await wait(() => document.querySelector('.post-body .audio-component audio'));
    const component = document.querySelector('.post-body .audio-component');
    const player = component.querySelector('audio');
    assert(player.controls && player.preload === 'none' && !player.autoplay && player.paused, 'Native playback must wait for the reader.');
    assert(component.querySelector('h3').textContent === title && component.textContent.includes(description) && !component.querySelector('markup') && !window.unsafeTranscript, 'Audio details must render safely.');
    assert(!component.querySelector('[download]'), 'The download preference must hide the separate download link.');
    const details = component.querySelector('details'); details.querySelector('summary').click();
    assert(details.open && details.querySelector('strong').textContent === 'Words from the recording.' && !details.querySelector('script'), 'The transcript must expand below the player as safe Markdown.');
    assert(document.documentElement.scrollWidth <= innerWidth + 1, 'Audio must fit the viewport.');
    player.load();
    await wait(() => player.readyState >= 1 || player.error);
    assert(!player.error && player.duration > 2, 'The browser must decode uploaded MP3 bytes.');
    player.currentTime = 1;
    await wait(() => !player.seeking);
    assert(Math.abs(player.currentTime - 1) < .15, 'Seeking must work.');
    await htmx.ajax('GET', document.querySelector('a[href^="/post/update/"]').getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
    await wait(() => document.querySelector('#audio-title'));
    assert(document.querySelector('#audio-title').value === title && !document.querySelector('[data-audio-editor] [data-component-unsaved]:not([hidden])'), 'Reopening must retain details with a clean saved indicator.');
    assert(document.querySelector('[data-audio-name]').textContent === 'Selected MP3', 'Reopened audio must not flash a storage ID before its filename loads.');
    await wait(() => document.querySelector('[data-audio-name]').textContent === sessionStorage.getItem('audio-filename'));
    sessionStorage.removeItem('audio-filename');
    sessionStorage.removeItem('audio-stage'); await report('PASS');
  } catch (error) { await report(error.stack || String(error)); }
})();
