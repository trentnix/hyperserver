// Exercise the full-page reload required when shared profile content changes.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate.toString());
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    let stage = sessionStorage.getItem('profile-stage');
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('profile-stage', 'logged-in');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'logged-in') {
      sessionStorage.setItem('profile-stage', 'edit');
      if (innerWidth < 768) document.querySelector('.account-menu summary').click();
      document.querySelector('.account-nav a[href="/profile"]').click();
      await wait(() => document.querySelector('#profile-form') && !document.querySelector('#article-content').classList.contains('htmx-settling'));
      stage = 'edit';
    }
    const form = document.querySelector('#profile-form');
    const formStyle = getComputedStyle(form);
    assert(formStyle.fontSize === '16px' && formStyle.lineHeight === '24px', 'Profile typography differs from other administrative forms.');
    assert(document.documentElement.scrollWidth <= innerWidth, 'Profile controls overflow the viewport.');
    const headerPortrait = document.querySelector('.portrait');
    await headerPortrait.decode();
    const portraitBounds = headerPortrait.getBoundingClientRect();
    assert(portraitBounds.width > 0 && portraitBounds.height <= 120 && Math.abs(portraitBounds.width - portraitBounds.height) < 1, 'Public portrait is not bounded to a square.');
    assert(getComputedStyle(headerPortrait).objectFit === 'cover', 'Public portrait is distorted instead of cropped.');
    const picker = field => form.elements[field].closest('[data-image-picker]');
    const selectFile = (field, file) => {
      const input = picker(field).querySelector('input[type="file"]');
      const files = new DataTransfer();
      files.items.add(file);
      input.files = files.files;
      input.dispatchEvent(new Event('change', {bubbles: true}));
    };
    const upload = async (field, color, width = 80, height = 60) => {
      const canvas = document.createElement('canvas');
      canvas.width = width; canvas.height = height;
      const context = canvas.getContext('2d');
      context.fillStyle = color;
      context.fillRect(0, 0, width, height);
      const image = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
      selectFile(field, new File([image], 'profile.png', {type: 'image/png'}));
      assert(form.querySelector('[type="submit"]').disabled, 'Save was not disabled during upload.');
      assert([...form.querySelectorAll('[data-image-picker] input, [data-image-reset]')].every(control => control.disabled), 'Another image can be changed during upload.');
      for (const control of form.querySelectorAll('button:disabled, input[type=file]:disabled')) {
        const style = getComputedStyle(control);
        assert(style.cursor === 'not-allowed' && Number(style.opacity) < 1, 'Disabled upload controls still look available.');
      }
      const submit = new Event('submit', {bubbles: true, cancelable: true});
      form.dispatchEvent(submit);
      assert(submit.defaultPrevented, 'Pending upload did not block submission.');
      await wait(() => !form.dataset.uploading);
      const path = form.elements[field].value;
      assert(path.startsWith('/images/') && path.endsWith('.png'), 'Upload did not select a stored image.');
      const preview = picker(field).querySelector('[data-image-preview]');
      await wait(() => preview.complete && preview.naturalWidth > 0);
      assert(!preview.hidden && preview.getAttribute('src') === path, 'Preview did not update.');
      if (field === 'imageURL') {
        const bounds = preview.getBoundingClientRect();
        assert(bounds.width === 120 && bounds.height === 120 && getComputedStyle(preview).objectFit === 'cover', 'Portrait preview is not bounded and cropped.');
        assert(preview.naturalWidth === width && preview.naturalHeight === height, 'Upload changed the original image dimensions.');
      }
      assert(!form.querySelector('[type="submit"]').disabled, 'Save was not restored.');
      sessionStorage.setItem(field, path);
    };
    if (stage === 'edit') {
      for (const fieldset of form.querySelectorAll('[data-image-picker]')) {
        const input = fieldset.querySelector('input[type=file]');
        const fileButton = getComputedStyle(input, '::file-selector-button');
        assert(getComputedStyle(input).borderTopWidth === '0px' && fileButton.padding === '8px 12px' && fileButton.borderRadius === '4px', 'Profile upload must use shared file-button styling.');
        const preview = fieldset.querySelector('.profile-image-preview').getBoundingClientRect();
        const controls = fieldset.querySelector('.profile-image-controls').getBoundingClientRect();
        const stacked = innerWidth < 768 || fieldset.dataset.imageField === 'backgroundURL';
        assert(stacked ? controls.top >= preview.bottom : controls.left > preview.right, 'Image controls do not follow the responsive layout.');
        const details = fieldset.querySelector('details');
        assert(!details.open, 'Image URL should start collapsed.');
        details.querySelector('summary').click();
        assert(details.open && fieldset.querySelector('[data-image-value]').getBoundingClientRect().width > 0, 'Image URL cannot be opened.');
        details.open = false;
      }
      const portrait = picker('imageURL').querySelector('[data-image-preview]');
      assert(getComputedStyle(portrait).borderRadius === '50%', 'Portrait preview is not rounded like the site portrait.');
      const favicon = picker('faviconURL').querySelector('[data-image-preview]').getBoundingClientRect();
      assert(favicon.width === 16 && favicon.height === 16, 'Favicon is not shown at browser-tab size.');
      const background = picker('backgroundURL').querySelector('.profile-image-preview').getBoundingClientRect();
      assert(background.width > background.height, 'Background preview is not landscape-shaped.');
      assert(form.elements.name.value === 'Trent Nix', 'Editor did not load stored profile.');
      form.elements.name.value = 'Updated Writer';
      form.elements.bio.value = 'An updated biography.';
      form.elements.faviconURL.value = 'javascript:alert(1)';
      form.requestSubmit();
      await wait(() => document.querySelector('#profile-errors').textContent.includes('HTTPS'));
      assert(document.querySelector('#profile-form') === form && form.elements.name.value === 'Updated Writer', 'Validation lost the profile draft.');
      form.elements.faviconURL.value = '';
      const originalPortrait = form.elements.imageURL.value;
      selectFile('imageURL', new File(['<svg onload="alert(1)"></svg>'], 'bad.png', {type: 'image/png'}));
      await wait(() => !form.dataset.uploading);
      assert(form.elements.imageURL.value === originalPortrait && picker('imageURL').textContent.includes('unchanged'), 'Invalid image replaced the portrait.');
      selectFile('imageURL', new File([new Uint8Array(8 * 1024 * 1024 + 1)], 'large.png', {type: 'image/png'}));
      assert(!form.dataset.uploading && picker('imageURL').textContent.includes('8 MiB'), 'Oversized upload was not rejected.');

      await upload('imageURL', '#aa3344', 1, 1000);
      await upload('faviconURL', '#4477aa');
      // Uploads must not publish anything until the profile is saved.
      const before = await (await fetch('/')).text();
      assert(!before.includes(sessionStorage.getItem('imageURL')) && !before.includes(sessionStorage.getItem('faviconURL')), 'Upload published unsaved profile changes.');
      picker('backgroundURL').querySelector('[data-image-reset]').click();
      assert(form.elements.backgroundURL.value === '' && picker('backgroundURL').querySelector('[data-image-preview]').hidden, 'Background reset failed.');
      sessionStorage.setItem('profile-stage', 'saved');
      form.requestSubmit();
      return;
    }
    if (stage === 'saved') {
      assert(document.querySelector('.profile-name').textContent === 'Updated Writer', 'Header did not reload.');
      assert(document.title === 'Updated Writer | Site Profile', 'Title did not update.');
      assert(document.querySelector('meta[name="description"]').content === 'An updated biography.', 'Description did not update.');
      assert(document.querySelector('link[rel="icon"]').getAttribute('href') === sessionStorage.getItem('faviconURL'), 'Favicon did not update.');
      assert(document.querySelector('.portrait').getAttribute('src') === sessionStorage.getItem('imageURL'), 'Portrait did not update.');
      assert(getComputedStyle(document.body).backgroundImage === 'none' && getComputedStyle(document.querySelector('.page-top')).backgroundImage === 'none', 'Cleared background is still visible.');
      // A failed network request must leave the saved image selected and controls usable.
      const originalFetch = window.fetch;
      window.fetch = (...args) => args[0] === '/post/images/upload' ? Promise.reject(new Error('Network unavailable')) : originalFetch(...args);
      selectFile('imageURL', new File(['image'], 'failed.png', {type: 'image/png'}));
      await wait(() => !form.dataset.uploading);
      window.fetch = originalFetch;
      assert(form.elements.imageURL.value === sessionStorage.getItem('imageURL') && picker('imageURL').textContent.includes('unchanged'), 'Network failure lost the saved image.');
      assert(!form.querySelector('[type="submit"]').disabled, 'Network failure left Save disabled.');
      for (const response of [
        new Response('{"error":"Unable to save the image."}', {status: 500, headers: {'Content-Type': 'application/json'}}),
        new Response('Sign in again', {status: 403, headers: {'Content-Type': 'text/html'}}),
        new Response('{}', {headers: {'Content-Type': 'application/json'}}),
        new Response('null', {headers: {'Content-Type': 'application/json'}})
      ]) {
        window.fetch = (...args) => args[0] === '/post/images/upload' ? Promise.resolve(response) : originalFetch(...args);
        selectFile('imageURL', new File(['image'], 'failed.png', {type: 'image/png'}));
        await wait(() => !form.dataset.uploading);
        window.fetch = originalFetch;
        assert(form.elements.imageURL.value === sessionStorage.getItem('imageURL'), 'Failed response replaced the selected image.');
        assert([...form.querySelectorAll('[data-image-picker] input, [data-image-reset], [type="submit"]')].every(control => !control.disabled), 'Failed response left controls disabled.');
      }
      await upload('imageURL', '#44aa66', 1000, 1);
      await upload('backgroundURL', '#aabbcc');
      const backgroundImage = picker('backgroundURL').querySelector('[data-image-preview]').getBoundingClientRect();
      const backgroundFrame = picker('backgroundURL').querySelector('.profile-image-preview').getBoundingClientRect();
      assert(Math.abs(backgroundFrame.left + backgroundFrame.right - backgroundImage.left - backgroundImage.right) < 1 && Math.abs(backgroundFrame.top + backgroundFrame.bottom - backgroundImage.top - backgroundImage.bottom) < 1, 'Background preview is not centered.');
      assert(backgroundImage.width <= backgroundFrame.width - 26 && backgroundImage.height <= backgroundFrame.height - 26, 'Background preview exceeds its frame.');
      assert(Math.abs(backgroundImage.width / backgroundImage.height - 80 / 60) < 0.01, 'Background preview distorts the image proportions.');
      sessionStorage.setItem('profile-stage', 'background');
      form.requestSubmit();
      return;
    }
    if (stage === 'cleared') {
      assert(document.querySelector('.portrait').getAttribute('src') === '/assets/img/profile-default.svg', 'Portrait default was not restored.');
      assert(document.querySelector('link[rel="icon"]').getAttribute('href') === '/assets/img/favicon.svg', 'Favicon default was not restored.');
      assert(getComputedStyle(document.body).backgroundImage === 'none' && getComputedStyle(document.querySelector('.page-top')).backgroundImage === 'none', 'Background reset was not saved.');
      await report('PASS');
      return;
    }
    assert(stage === 'background', 'Unexpected profile stage.');
    const target = document.body;
    assert(getComputedStyle(target).backgroundImage.includes(sessionStorage.getItem('backgroundURL')), 'Uploaded background was not rendered.');
    assert(document.querySelector('.portrait').getAttribute('src') === sessionStorage.getItem('imageURL'), 'Replacement portrait was not saved.');
    const feed = await (await fetch('/rss')).text();
    assert(feed.includes('Updated Writer') && feed.includes('An updated biography.'), 'RSS did not update.');
    for (const field of ['imageURL', 'faviconURL', 'backgroundURL']) {
      assert(form.elements[field].value === sessionStorage.getItem(field), 'Saved image was not loaded into the editor.');
      picker(field).querySelector('[data-image-reset]').click();
      assert(form.elements[field].value === '', 'Reset retained the image URL.');
    }
    sessionStorage.setItem('profile-stage', 'cleared');
    form.requestSubmit();
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
