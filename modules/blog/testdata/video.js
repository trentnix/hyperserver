(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const wait = async predicate => {
    const end = Date.now() + 12000;
    while (!predicate()) { if (Date.now() > end) throw new Error('Timed out: ' + predicate); await new Promise(resolve => setTimeout(resolve, 25)); }
  };
  const set = (input, value) => { input.value = value; input.dispatchEvent(new Event('input', {bubbles: true})); };
  const title = 'A video title that wraps across several lines on a narrow phone screen';
  const caption = 'A caption with <markup> and & characters.\nA second line stays with the video.';
  try {
    const stage = sessionStorage.getItem('video-stage');
    if (!stage) {
      await wait(() => document.querySelector('[data-video-editor] [name="video"]').hidden && document.querySelector('#post-form .CodeMirror'));
      const form = document.querySelector('#post-form');
      const section = form.querySelector('[data-video-editor]');
      assert(section.hidden && section.querySelector('fieldset').disabled, 'An unused video component must not block validation.');
      form.elements.title.value = 'Video component review'; form.elements.slug.value = 'video-component-review';
      form.querySelector('[data-component-picker] summary').click();
      const options = [...form.querySelectorAll('[data-component-add]:not([hidden])')];
      assert(options.every(button => Math.abs(button.getBoundingClientRect().height - options[0].getBoundingClientRect().height) < 1), 'Component picker entries must have equal heights.');
      form.querySelector('[data-component-add="video"]').click();
      assert(!section.hidden && document.activeElement.id === 'video-url' && !section.querySelector('fieldset').disabled, 'Adding Video must focus its URL and enable its fields.');
      set(form.querySelector('#video-url'), 'https://attacker.test/watch?v=M7lc1UVf-VE');
      set(form.querySelector('#video-title'), title);
      set(form.querySelector('#video-caption'), caption);
      form.requestSubmit();
      await wait(() => form.querySelector('#editor-errors').textContent.includes('YouTube or Vimeo'));
      assert(form.querySelector('#video-title').value === title && form.querySelector('#video-caption').value === caption, 'Validation must preserve video details.');
      set(form.querySelector('#video-url'), 'https://youtu.be/M7lc1UVf-VE?t=1m30s');
      const saved = form.elements.video.value;
      section.querySelector('[data-component-remove]').click();
      assert(section.hidden && section.querySelector('fieldset').disabled && !form.elements.video.value, 'Removing Video must clear and disable its fields.');
      form.querySelector('[data-component-undo="video"]').click();
      assert(form.elements.video.value === saved && form.querySelector('#video-title').value === title, 'Undo must restore the unsaved video.');
      form.querySelector('[data-component-picker] summary').click();
      form.querySelector('[data-component-add="link-preview"]').click();
      section.querySelector('[data-component-move="down"]').click();
      assert(form.elements.componentOrder.value === '["link-preview","video"]', 'Video must use the shared component ordering.');
      form.querySelector('[data-post-component="link-preview"] [data-component-remove]').click();
      assert(!form.elements.content.required, 'A video-only post must be allowed.');
      form.querySelector('[name="published"][value="on"]').checked = true;
      form.dispatchEvent(new Event('input', {bubbles: true}));
      await wait(() => Object.keys(localStorage).some(key => key.startsWith('hyperserver.blog.draft.v1:') && localStorage.getItem(key).includes('M7lc1UVf-VE')));
      sessionStorage.setItem('video-stage', 'recover'); location.reload(); return;
    }
    if (stage === 'recover') {
      await wait(() => document.querySelector('[data-draft-recovery]') && !document.querySelector('[data-draft-recovery]').hidden);
      document.querySelector('[data-draft-restore]').click();
      const form = document.querySelector('#post-form');
      await wait(() => form.querySelector('#video-title').value === title);
      assert(form.querySelector('#video-caption').value === caption && form.elements.componentOrder.value === '["video"]', 'Recovery must retain video details and order.');
      sessionStorage.setItem('video-stage', 'youtube'); form.requestSubmit(); return;
    }
    await wait(() => document.querySelector('.post-body .video-component iframe'));
    const video = document.querySelector('.post-body .video-component');
    assert(video.textContent.includes(title) && video.textContent.includes(caption) && !video.querySelector('markup'), 'The video title and caption must be safely rendered.');
    assert(document.documentElement.scrollWidth <= innerWidth + 1, 'Video components must fit all tested viewports.');
    const frame = video.querySelector('iframe');
    assert(frame && frame.title === title && frame.referrerPolicy === 'strict-origin' && frame.allowFullscreen && !frame.allow.includes('autoplay'), 'Player controls, title, fullscreen, and origin-only referrer policy must be set without autoplay permission.');
    assert(frame.loading === 'lazy' && !video.querySelector('button') && new URL(frame.src).searchParams.get('autoplay') === '0', 'The provider preview must appear without an extra click and wait for Play.');
    assert(frame.getBoundingClientRect().height >= 200, 'The player must remain usable on narrow screens.');
    if (stage === 'youtube') {
      assert(frame.src === 'https://www.youtube-nocookie.com/embed/M7lc1UVf-VE?autoplay=0&playsinline=1&start=90', 'YouTube must use its validated ID and requested start time.');
      assert(video.querySelector('a').href === 'https://www.youtube.com/watch?v=M7lc1UVf-VE&t=90', 'An ordinary viewing link must remain available.');
      await htmx.ajax('GET', document.querySelector('a[href^="/post/update/"]').getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
      await wait(() => document.querySelector('#video-title'));
      const form = document.querySelector('#post-form');
      assert(form.querySelector('#video-title').value === title && form.querySelector('#video-caption').value === caption, 'Reopening the editor must preserve video fields.');
      set(form.querySelector('#video-url'), 'https://vimeo.com/123456789/5e2d1c1e6d#t=45s');
      sessionStorage.setItem('video-stage', 'vimeo'); form.requestSubmit(); return;
    }
    assert(frame.src === 'https://player.vimeo.com/video/123456789?h=5e2d1c1e6d&autoplay=0&dnt=1#t=45s', 'Vimeo must retain its unlisted hash and timestamp.');
    assert(video.querySelector('a').href === 'https://vimeo.com/123456789/5e2d1c1e6d#t=45s', 'Vimeo must have a usable direct viewing link.');
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
