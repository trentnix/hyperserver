(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const visible = node => node?.getBoundingClientRect().height > 0;
  const noticeVisible = root => {
    const notice = root.querySelector('.upload-storage-notice');
    if (!visible(notice)) return false;
    const style = getComputedStyle(notice);
    return style.color === 'rgb(161, 38, 38)' && style.backgroundColor === 'rgb(255, 243, 242)' && style.borderLeftWidth === '4px';
  };
  try {
    const form = document.querySelector(':is(#post-form, #page-form, #profile-form)');
    if (new URLSearchParams(location.search).has('fill')) {
      await waitForComponent(() => form.querySelector('.tool-image'), 'Markdown toolbar');
      const panel = await addTestComponent(form.querySelector('[data-component-add="file-downloads"]'));
      const input = panel.querySelector('input[type="file"]');
      assert(!input.disabled && !form.hasAttribute('data-upload-storage-full'), 'There must be room to upload when the editor opens.');
      const selected = new DataTransfer();
      selected.items.add(new File(['fit'], 'exact-fit.txt', {type: 'text/plain'}));
      selected.items.add(new File(['skip'], 'next.txt', {type: 'text/plain'}));
      const fetchOriginal = window.fetch;
      let requests = 0;
      window.fetch = (path, options) => {
        if (path === '/post/files/upload') requests++;
        return fetchOriginal(path, options);
      };
      input.files = selected.files;
      input.dispatchEvent(new Event('change', {bubbles: true}));
      await waitForComponent(() => form.hasAttribute('data-upload-storage-full') && !form.dataset.uploading, 'storage filled by upload');
      window.fetch = fetchOriginal;
      const entries = JSON.parse(panel.querySelector('textarea[name]').value);
      assert(entries.length === 1 && entries[0].label === 'exact-fit.txt' && requests === 1, 'Keep the successful file and stop the batch when storage fills.');
      assert(input.disabled && panel.querySelector('[data-download-upload]').disabled && !panel.querySelector('[data-download-browse]').disabled &&
        noticeVisible(panel), 'A full response must update the open component without blocking the library.');
      form.querySelector('[data-component-cancel]').click();
      await waitForComponent(() => !form.querySelector('.component-dialog[open]'), 'closed settings');
    }
    assert(form.hasAttribute('data-upload-storage-full'), 'The editor must receive the full storage state.');
    for (const picker of form.querySelectorAll('.image-picker[data-image-picker]')) {
      assert(picker.querySelector('[data-image-upload]').disabled && picker.querySelector('input[type="file"]').disabled, 'Image upload controls must be disabled.');
      assert(!picker.querySelector('[data-media-browse]').disabled, 'Browsing existing images must stay available.');
    }
    assert(noticeVisible(form) && !form.querySelector('button[type="submit"]').disabled, 'The notice must be visible and saving must stay available.');
    if (form.id === 'profile-form') {
      assert(form.querySelector('#profile-image-help').previousElementSibling.matches('.upload-storage-notice'), 'The profile quota warning must come before image help.');
      await report('PASS'); return;
    }

    const previewControls = form.querySelector('.preview-image-controls .image-upload-controls');
    assert(previewControls.nextElementSibling.matches('.upload-storage-notice'), 'The preview image quota warning must sit directly below the buttons.');
    assert(form.querySelector('#markdown-help').previousElementSibling.matches('.upload-storage-notice'), 'The quota warning must come before Markdown help.');

    await waitForComponent(() => form.querySelector('.tool-image'), 'Markdown toolbar');
    assert(form.querySelector('.tool-image').disabled && form.querySelector('.imageInput').disabled && !form.querySelector('.tool-library').disabled,
      'The Markdown toolbar must disable uploading and keep the library available.');
    const kinds = form.id === 'post-form' ? ['pdf-preview', 'file-downloads', 'audio', 'photo-album', 'image-comparison', 'event', 'social-post', 'link-preview'] : ['pdf-preview'];
    for (const kind of kinds) {
      const panel = await addTestComponent(form.querySelector(`[data-component-add="${kind}"]`));
      assert(noticeVisible(panel), `${kind} must explain why uploads are paused.`);
      const inputs = [...panel.querySelectorAll('input[type="file"]')];
      assert(inputs.length && inputs.every(input => input.disabled), `${kind} file inputs must be disabled.`);
      const uploads = [...panel.querySelectorAll('button')].filter(button => /^Upload/.test(button.textContent.trim()));
      assert(uploads.length && uploads.every(button => button.disabled), `${kind} upload buttons must be disabled.`);
      const library = [...panel.querySelectorAll('button')].find(button => /^(Library|Choose image)$/.test(button.textContent.trim()));
      assert(library && !library.disabled, `${kind} must still allow browsing the library.`);
      if (kind === 'pdf-preview') {
        assert(panel.querySelector('.downloads-limits').previousElementSibling.matches('.upload-storage-notice'), 'The PDF quota warning must come before upload limits.');
        library.click();
        await waitForComponent(() => document.querySelector('.download-library input[type="radio"]'), 'existing PDF');
        document.querySelector('.download-library input[type="radio"]').click();
        document.querySelector('.download-library .form-actions button:last-child').click();
        await waitForComponent(() => panel.querySelector('[data-pdf-document] a[href]'), 'selected PDF');
      }
      if (kind === 'audio') {
        assert(panel.querySelector('.audio-file-actions').nextElementSibling.matches('.upload-storage-notice'), 'The audio quota warning must sit directly below the buttons.');
      }
      if (kind === 'link-preview') {
        let metadataOnly;
        const fetchOriginal = window.fetch;
        window.fetch = (path, options) => {
          if (path !== '/post/link-preview') return fetchOriginal(path, options);
          metadataOnly = options.body.get('image') === 'false';
          return Promise.resolve(new Response(JSON.stringify({link: {url: options.body.get('url'), title: 'Preview title'}}), {headers: {'Content-Type': 'application/json'}}));
        };
        const url = panel.querySelector('input[type="url"]');
        url.value = 'https://example.test/article';
        url.dispatchEvent(new Event('input', {bubbles: true}));
        panel.querySelector('[data-link-fetch]').click();
        await waitForComponent(() => panel.querySelector('[data-link-status]').textContent.includes('Preview ready'), 'link preview details');
        assert(metadataOnly, 'Link previews must fetch text details without importing an image.');
        window.fetch = fetchOriginal;
      }
      form.querySelector('[data-component-cancel]').click();
      await waitForComponent(() => !form.querySelector('.component-dialog[open]'), 'closed settings');
    }
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
