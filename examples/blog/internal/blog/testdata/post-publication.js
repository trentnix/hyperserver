// Publication choices apply on the first save and on subsequent edits.
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
  const editor = async path => {
    await htmx.ajax('GET', path, '#dialog-host');
    await wait(() => document.querySelector('dialog[open] .CodeMirror'));
    await wait(() => !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
    return document.querySelector('#post-form');
  };
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    const stage = sessionStorage.getItem('publication-stage');
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('publication-stage', 'create');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'create') {
      const form = await editor('/post/create');
      assert(form.querySelector('[data-image-preview]').getClientRects().length === 0, 'New post must not display an empty preview image.');
      assert(form.elements.published.value === '', 'New post must start as a draft.');
      assert(form.querySelector('[type="submit"]').textContent === 'Save post', 'Post save label must not force a draft step.');
      form.elements.title.value = 'First Save';
      assert(form.elements.slug.value === '' && !form.elements.slug.required, 'New post slug must be optional.');
      for (const [title, slug] of [['  Go & HTMX!  ', 'go-htmx'], ['İGo Kit', 'go-it'], ['世界', 'Enter a custom slug'], ['A'.repeat(119) + ' -- end', 'a'.repeat(119)], ['First Save', 'first-save']]) {
        form.elements.title.value = title;
        form.elements.title.dispatchEvent(new Event('input', {bubbles: true}));
        assert(form.elements.slug.placeholder === slug && form.elements.slug.value === '', 'Slug suggestion must follow the title without entering a value.');
      }
      form.elements.slug.value = 'custom-slug';
      form.elements.title.dispatchEvent(new Event('input', {bubbles: true}));
      assert(form.elements.slug.value === 'custom-slug', 'Updating the suggestion replaced an explicit slug.');
      form.elements.slug.value = '';
      const earlier = await fetch('/post', {method: 'POST', headers: {'HX-Request': 'true'}, body: new URLSearchParams({title: 'First Save', content: 'An earlier post with the same title.'})});
      assert(earlier.ok, 'Unable to prepare a title collision.');
      form.elements.published.value = 'on';
      const input = form.querySelector('#post-image');
      const selectFile = file => {
        const files = new DataTransfer();
        files.items.add(file);
        input.files = files.files;
        input.dispatchEvent(new Event('change', {bubbles: true}));
      };
      selectFile(new File(['invalid'], 'bad.png', {type: 'image/png'}));
      await wait(() => !form.dataset.uploading);
      assert(form.elements.previewImage.value === '' && form.querySelector('[data-image-status]').textContent.includes('unchanged'), 'Failed upload changed the preview.');
      const canvas = document.createElement('canvas');
      canvas.width = 100; canvas.height = 60;
      const image = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
      selectFile(new File([image], 'preview.png', {type: 'image/png'}));
      assert(form.querySelector('[type="submit"]').disabled, 'Save must wait for the preview upload.');
      await wait(() => !form.dataset.uploading);
      assert(form.elements.previewImage.value.endsWith('.png') && !form.querySelector('[data-image-preview]').hidden, 'Uploaded preview was not selected or displayed.');
      sessionStorage.setItem('post-preview', form.elements.previewImage.value);
      await wait(() => form.querySelector('[data-draft-status]').textContent === 'Saved locally. Use Save to apply changes.');
      const recoveryPrefix = 'hyperserver.blog.draft.v1:' + form.dataset.draftOwner + ':/post:';
      assert(Object.keys(localStorage).some(key => key.startsWith(recoveryPrefix) && JSON.parse(localStorage.getItem(key)).fields.previewImage === form.elements.previewImage.value), 'Recovery copy omitted the selected preview.');
      form.requestSubmit();
      await wait(() => document.querySelector('#editor-errors').textContent.includes('valid Markdown'));
      assert(form.elements.published.value === 'on', 'Validation failure cleared the publication choice.');
      assert(form.elements.previewImage.value === sessionStorage.getItem('post-preview'), 'Validation failure cleared the preview.');
      form.querySelector('.CodeMirror').CodeMirror.setValue('Public content on the first save.');
      sessionStorage.setItem('publication-stage', 'published');
      form.requestSubmit();
      return;
    }
    if (stage === 'published') {
      assert(location.pathname === '/post/s/first-save-2', 'Generated collision did not navigate to the suffixed URL.');
      assert(document.querySelector('meta[property="og:image"]').content.endsWith('/images/' + sessionStorage.getItem('post-preview')), 'Saved preview missing from sharing metadata.');
      assert(!document.querySelector('.post-body img'), 'Sharing preview appeared in article body.');
      const response = await fetch(location.pathname, {credentials: 'omit'});
      assert(response.ok && (await response.text()).includes('Public content on the first save.'), 'Checked first save did not publish the content.');
      const form = await editor(document.querySelector('a[href^="/post/update/"]').getAttribute('href'));
      assert(form.elements.slug.value === 'first-save-2' && !form.elements.slug.required, 'Post editor must load its generated slug and allow clearing it.');
      assert(form.elements.previewImage.value === sessionStorage.getItem('post-preview'), 'Reopening lost the preview.');
      form.querySelector('[data-image-reset]').click();
      assert(form.elements.previewImage.value === '' && form.querySelector('[data-image-preview]').hidden, 'Automatic selection did not clear the uploaded preview.');
      assert(form.querySelector('[data-image-preview]').getClientRects().length === 0, 'Cleared preview image must not occupy layout space.');
      form.elements.slug.value = '';
      assert(form.elements.published.value === 'on', 'Published post did not retain its publication setting.');
      form.elements.published.value = '';
      form.querySelector('.CodeMirror').CodeMirror.setValue('Private edited content.');
      let warnings = 0;
      window.blogConfirm = (options, proceed) => {
        assert(options.message.includes('Changing this URL'), 'Unexpected confirmation before saving.');
        warnings++;
        if (warnings > 1) queueMicrotask(proceed);
      };
      form.requestSubmit();
      assert(warnings === 1 && form.isConnected && form.elements.slug.value === '', 'Canceling the URL warning must preserve the editor.');
      sessionStorage.setItem('publication-stage', 'draft');
      form.requestSubmit();
      assert(warnings === 2, 'Saving a regenerated published URL must ask again after cancellation.');
      return;
    }
    if (stage === 'draft') {
      assert((await fetch(location.pathname, {credentials: 'omit'})).status === 404, 'Unchecked save left the post public.');
      assert(document.querySelector('.post-body').textContent.includes('Private edited content.'), 'Unpublishing failed to save the content edit.');
      const form = await editor(document.querySelector('a[href^="/post/update/"]').getAttribute('href'));
      assert(form.elements.published.value === '', 'Draft opened with the wrong publication setting.');
      assert(form.elements.previewImage.value === '', 'Removed preview was not saved.');
      form.elements.published.value = 'on';
      sessionStorage.setItem('publication-stage', 'republished');
      form.requestSubmit();
      return;
    }
    if (stage === 'republished') {
      const response = await fetch(location.pathname, {credentials: 'omit'});
      assert(response.ok && (await response.text()).includes('Private edited content.'), 'Checking Publish post did not republish the saved content.');
      await report('PASS');
    }
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
