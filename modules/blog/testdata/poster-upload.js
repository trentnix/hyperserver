(async () => {
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 10000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate.toString());
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    const stage = sessionStorage.getItem('poster-upload-stage');
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('poster-upload-stage', 'signed-in');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'signed-in') {
      sessionStorage.setItem('poster-upload-stage', 'editing');
      location.assign('/post/create');
      return;
    }
    await wait(() => document.querySelector('.editor-toolbar .tool-image'));
    assert(!document.querySelector('.tool-library, [data-media-browse]'), 'Poster can browse the shared library.');
    const button = document.querySelector('[data-image-upload]');
    await wait(() => button.getBoundingClientRect().height > 0);
    const canvas = document.createElement('canvas');
    canvas.width = canvas.height = 16;
    canvas.getContext('2d').fillRect(0, 0, 16, 16);
    const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
    const transfer = new DataTransfer();
    transfer.items.add(new File([blob], 'poster.png', {type: 'image/png'}));
    const picker = document.querySelector('[data-image-picker]');
    const input = picker.querySelector('input[type=file]');
    input.files = transfer.files;
    input.dispatchEvent(new Event('change', {bubbles: true}));
    await wait(() => picker.querySelector('[data-image-value]').value !== '');
    const preview = picker.querySelector('[data-image-preview]');
    await wait(() => preview.complete && preview.naturalWidth > 0);
    assert(!picker.querySelector('.upload-error'), 'Poster upload failed.');
    assert(document.documentElement.scrollWidth <= Math.max(320, document.documentElement.clientWidth), 'Poster editor overflows.');
    await report('PASS');
  } catch (error) { await report(String(error) + '\n' + error.stack); }
})();
