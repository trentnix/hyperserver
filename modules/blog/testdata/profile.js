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
    if (location.pathname === '/' && ['saved', 'background', 'cleared'].includes(stage)) {
      assert(document.querySelector('#notifications').textContent.includes('Profile saved'), 'Missing profile confirmation.');
      if (stage === 'saved') {
        assert(document.querySelector('.profile-name').textContent === 'Updated Writer', 'Header did not reload.');
        assert(document.title.startsWith('Updated Writer') && !document.title.includes('Site Profile'), 'Title did not update.');
        assert(document.querySelector('meta[name="description"]').content === 'An updated biography.', 'Description did not update.');
        assert(document.querySelector('link[rel="icon"]').getAttribute('href') === sessionStorage.getItem('faviconURL'), 'Favicon did not update.');
        assert(document.querySelector('.portrait').getAttribute('src') === sessionStorage.getItem('imageURL'), 'Portrait did not update.');
        assert(getComputedStyle(document.body).backgroundImage === 'none' && getComputedStyle(document.querySelector('.page-top')).backgroundImage === 'none', 'Cleared background is still visible.');
      }
      location.href = '/profile';
      return;
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
        assert(input.hidden && getComputedStyle(input).display === 'none', 'Native file controls should not duplicate the upload button.');
        const uploadButton = fieldset.querySelector('[data-image-upload]');
        const browseButton = fieldset.querySelector('[data-media-browse]');
        assert(uploadButton.textContent === 'Upload image' && browseButton.textContent === 'Browse library', 'Image actions should have clear, consistent labels.');
        const uploadBounds = uploadButton.getBoundingClientRect();
        const browseBounds = browseButton.getBoundingClientRect();
        assert(Math.abs(uploadBounds.top - browseBounds.top) < 1 && Math.abs(uploadBounds.width - browseBounds.width) < 1 && Math.abs(uploadBounds.height - browseBounds.height) < 1, 'Upload and library buttons must share a row and size.');
        let filePickerOpened = false;
        const interceptPicker = event => { event.preventDefault(); filePickerOpened = true; };
        input.addEventListener('click', interceptPicker, {once: true});
        uploadButton.click();
        assert(filePickerOpened, 'Upload image did not open the file picker.');
        const preview = fieldset.querySelector('.profile-image-preview').getBoundingClientRect();
        const reset = fieldset.querySelector('[data-image-reset]').getBoundingClientRect();
        assert(reset.top >= preview.bottom && Math.abs(reset.left + reset.right - preview.left - preview.right) < 1, 'Reset action must be centered below the preview.');
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
      // Losing authentication must preserve the entire form and selected File.
      const originalName = form.elements.name.value;
      form.elements.name.value = 'Unsaved profile name';
      const canvas = document.createElement('canvas');
      canvas.width = canvas.height = 12;
      canvas.getContext('2d').fillRect(0, 0, 12, 12);
      const pendingImage = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
      await fetch('/auth/logout', {method: 'POST', redirect: 'manual'});
      selectFile('imageURL', new File([pendingImage], 'pending.png', {type: 'image/png'}));
      await wait(() => !form.dataset.uploading);
      const authStatus = picker('imageURL').querySelector('[data-image-status]');
      assert(authStatus.textContent.includes("You're signed out"), 'Expired login was not explained.');
      assert(getComputedStyle(authStatus).borderLeftWidth === '4px' && authStatus.getAttribute('role') === 'alert', 'Site Profile must use the shared upload-error treatment.');
      assert(document.querySelector('#profile-form') === form && form.elements.name.value === 'Unsaved profile name', 'Authentication failure lost unsaved profile edits.');
      const signIn = authStatus.querySelector('a');
      assert(signIn?.getAttribute('href') === '/login' && signIn.target === '_blank' && signIn.rel === 'noopener' && signIn.getAttribute('hx-boost') === 'false', 'Recovery must sign in separately without navigating the editor.');
      authStatus.querySelector('[data-upload-retry]').click();
      await wait(() => !form.dataset.uploading);
      assert(authStatus.textContent.includes("You're signed out") && !form.querySelector('[type="submit"]').disabled, 'Failed retry lost the recovery flow or disabled Save.');
      form.requestSubmit();
      await wait(() => form.querySelector('#profile-errors .page-message'));
      const failure = form.querySelector('#profile-errors');
      assert(getComputedStyle(failure).borderLeftWidth === '4px' && getComputedStyle(failure.querySelector('.page-message')).borderLeftWidth === '0px', 'General errors inside an editor must have one border, not nested panels.');
      const login = await fetch('/login', {method: 'POST', redirect: 'manual', body: new URLSearchParams({provider: 'email', email: 'trentnix@gmail.com', password: 'Sample-Password-123!'})});
      assert(login.type === 'opaqueredirect', 'Reauthentication did not succeed.');
      authStatus.querySelector('[data-upload-retry]').click();
      await wait(() => !form.dataset.uploading);
      assert(authStatus.textContent === 'Image uploaded. Save to apply.' && !authStatus.querySelector('a'), 'Retained image did not upload after signing in again.');
      assert(form.elements.name.value === 'Unsaved profile name', 'Retry lost unsaved edits.');
      assert(form.elements.imageURL.value !== sessionStorage.getItem('imageURL'), 'Retry did not use the retained file.');
      form.elements.name.value = originalName;
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
      // The Markdown toolbar must offer the same recovery without losing writing.
      await htmx.ajax('GET', '/post/create', {target: '#article-content'});
      await wait(() => document.querySelector('.CodeMirror')?.CodeMirror);
      await wait(() => !document.querySelector('#article-content').classList.contains('htmx-settling'));
      const cm = document.querySelector('.CodeMirror').CodeMirror;
      cm.setValue('Unsaved writing');
      const assertErrorPanel = element => {
        const style = getComputedStyle(element);
        assert(element.getAttribute('role') === 'alert', 'Post error must be announced.');
        assert(style.color === 'rgb(161, 38, 38)' && style.backgroundColor === 'rgb(255, 243, 242)' && style.borderLeftWidth === '4px' && style.borderRightWidth === '1px', 'Post errors need consistent red text, tint, and border.');
        assert(document.documentElement.scrollWidth <= innerWidth, 'Post error overflows the viewport.');
      };
      const postForm = document.querySelector('#post-form');
      const assertInvalid = (input, frame = input) => {
        assert(input.getAttribute('aria-invalid') === 'true' && input.getAttribute('aria-describedby').split(' ').includes('editor-errors'), 'Invalid fields must identify their error for assistive technology.');
        assert(getComputedStyle(frame).borderTopColor === 'rgb(161, 38, 38)', 'Invalid controls need a red border.');
      };
      assert(!postForm.querySelector('[aria-invalid=true]') && getComputedStyle(postForm.elements.title).borderTopColor !== 'rgb(161, 38, 38)', 'A fresh form must not flag untouched fields.');
      let popupSuppressed = false;
      postForm.elements.title.addEventListener('invalid', event => { popupSuppressed = event.defaultPrevented; }, {once: true});
      postForm.requestSubmit();
      await wait(() => document.activeElement === postForm.elements.title);
      assert(getComputedStyle(postForm.elements.title).borderTopColor === 'rgb(161, 38, 38)', 'Native validation must mark the required title.');
      const titleError = document.querySelector('#post-title-error');
      assert(popupSuppressed && titleError?.textContent === 'Enter a title.', 'Validation must replace the browser popup with a short inline message.');
      assert(postForm.elements.title.getAttribute('aria-describedby') === titleError.id, 'The title must describe its inline error.');
      const titleStyle = getComputedStyle(postForm.elements.title);
      assert(titleStyle.outlineColor === 'rgb(161, 38, 38)' && titleStyle.outlineWidth === '2px' && titleStyle.outlineOffset === '0px' && titleStyle.boxShadow === 'none', 'Invalid focus must use one red outline, without a second ring or shadow.');
      assert(getComputedStyle(titleError).fontSize === '13.6px' && getComputedStyle(titleError).borderTopWidth === '0px', 'Inline errors must use compact text, not another panel.');
      postForm.requestSubmit();
      assert(postForm.querySelectorAll('#post-title-error').length === 1, 'Repeated submission must not duplicate inline errors.');
      postForm.elements.title.value = 'Error style test';
      postForm.elements.title.dispatchEvent(new Event('input', {bubbles: true}));
      assert(!document.querySelector('#post-title-error') && !postForm.elements.title.hasAttribute('aria-invalid'), 'Editing the title must clear its inline error.');
      cm.setValue('');
      postForm.requestSubmit();
      await wait(() => document.querySelector('#editor-errors').textContent.trim());
      assertErrorPanel(document.querySelector('#editor-errors'));
      assertInvalid(cm.getInputField(), cm.getWrapperElement());
      cm.setValue('Unsaved writing');
      assert(!cm.getInputField().hasAttribute('aria-invalid') && getComputedStyle(cm.getWrapperElement()).borderTopColor !== 'rgb(161, 38, 38)', 'Editing Markdown must clear its field error.');
      assert(cm.getInputField().getAttribute('aria-describedby') === 'markdown-help', 'Clearing an error must retain the writing help.');
      postForm.elements.tags.value = 't'.repeat(51);
      postForm.requestSubmit();
      await wait(() => postForm.elements.tags.getAttribute('aria-invalid') === 'true');
      assertInvalid(postForm.elements.tags);
      assert(!cm.getInputField().hasAttribute('aria-invalid'), 'New validation must not leave stale Markdown errors.');
      postForm.elements.tags.value = 'valid';
      postForm.elements.tags.dispatchEvent(new Event('input', {bubbles: true}));
      assert(!postForm.elements.tags.hasAttribute('aria-invalid') && postForm.elements.tags.getAttribute('aria-describedby') === 'tags-help', 'Editing tags must clear the error and preserve help.');
      postForm.querySelector('[name=published][value=scheduled]').click();
      postForm.requestSubmit();
      await wait(() => document.activeElement === postForm.elements.publishDate);
      assert(document.querySelector('#post-publish-date-error')?.textContent === 'Choose a publication date.' && document.querySelector('#post-publish-time-error')?.textContent === 'Enter a publication time.', 'Missing schedule fields must show inline errors and focus the first field.');
      assert(postForm.querySelector('.post-time-field').nextElementSibling.id === 'post-publish-time-error', 'Time validation must sit below the whole time control.');
      postForm.elements.publishDate.value = '2000-01-01';
      postForm.elements.publishDate.dispatchEvent(new Event('input', {bubbles: true}));
      postForm.elements.publishTime.value = '9:30 AM';
      postForm.elements.publishTime.dispatchEvent(new Event('input', {bubbles: true}));
      postForm.requestSubmit();
      await wait(() => postForm.elements.publishTime.getAttribute('aria-invalid') === 'true');
      assertInvalid(postForm.elements.publishDate);
      assertInvalid(postForm.elements.publishTime, postForm.querySelector('.post-time-field'));
      postForm.querySelector('[name=published][value=""]').click();
      assert(!postForm.elements.publishDate.hasAttribute('aria-invalid') && !postForm.elements.publishTime.hasAttribute('aria-invalid'), 'Switching to Draft must clear errors for the inactive schedule.');
      const previewInput = postForm.querySelector('[data-image-picker] input[type=file]');
      const invalidFile = new DataTransfer();
      invalidFile.items.add(new File(['invalid image'], 'invalid.png', {type: 'image/png'}));
      previewInput.files = invalidFile.files;
      previewInput.dispatchEvent(new Event('change', {bubbles: true}));
      await wait(() => !postForm.dataset.uploading);
      const previewStatus = postForm.querySelector('[data-image-status]');
      assertErrorPanel(previewStatus);
      assert(getComputedStyle(previewInput.closest('fieldset')).borderTopColor === 'rgb(161, 38, 38)', 'Failed preview uploads must mark the image picker.');
      postForm.querySelector('[data-image-value]').dispatchEvent(new Event('change', {bubbles: true}));
      assert(!previewStatus.classList.contains('upload-error') && previewStatus.getAttribute('role') === 'status' && previewStatus.textContent === '', 'Replacing an image must clear its error treatment.');
      await fetch('/auth/logout', {method: 'POST', redirect: 'manual'});
      const canvas = document.createElement('canvas');
      canvas.width = canvas.height = 2;
      const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
      document.querySelector('.tool-image').click();
      const input = document.querySelector('.imageInput');
      const transfer = new DataTransfer();
      transfer.items.add(new File([blob], 'editor.png', {type: 'image/png'}));
      input.files = transfer.files;
      input.dispatchEvent(new Event('change'));
      await wait(() => document.querySelector('[data-upload-retry]'));
      assertErrorPanel(document.querySelector('[data-upload-retry]').closest('p'));
      assert(cm.getValue() === 'Unsaved writing', 'Failed Markdown upload changed the writing.');
      const link = document.querySelector('.image-upload-recovery a');
      assert(link?.getAttribute('href') === '/login' && link.target === '_blank', 'Markdown upload lacks sign-in recovery.');
      await fetch('/login', {method: 'POST', redirect: 'manual', body: new URLSearchParams({provider: 'email', email: 'trentnix@gmail.com', password: 'Sample-Password-123!'})});
      cm.setCursor(cm.lineCount(), 0);
      document.querySelector('[data-upload-retry]').click();
      assert(!document.querySelector('.upload-error'), 'Retry must clear stale error styling while uploading.');
      await wait(() => cm.getValue().includes('/images/'));
      assert(cm.getValue().includes('Unsaved writing') && !document.querySelector('[data-upload-retry]'), 'Markdown retry lost writing or left stale recovery actions.');
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
