// Exercise real selection controls without uploading a second copy or losing edits.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate);
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  window.addEventListener('error', event => report(event.message));
  const library = () => document.querySelector('.media-browser[open]');
  function assertFilterHeights(form) {
    const search = form.elements.q.getBoundingClientRect();
    for (const control of [form.elements.usage, form.querySelector('button')]) {
      assert(Math.abs(control.getBoundingClientRect().height - search.height) < 1, 'Search field, Usage dropdown, and Search button must have equal heights.');
    }
  }
  async function open(button) {
    button.focus();
    button.click();
    await wait(() => library()?.querySelector('[data-media-select]'));
    const cards = [...library().querySelectorAll('.media-card')];
    assert(cards.every(card => card.tagName === 'BUTTON' && card.type === 'button' && card.dataset.mediaSelect && card.getAttribute('aria-label').startsWith('Select ')), 'Chooser cards must be accessible selection buttons.');
    cards[0].focus({preventScroll: true});
    const focusedCard = getComputedStyle(cards[0]);
    assert(focusedCard.outlineStyle === 'solid' && focusedCard.outlineWidth === '2px' && focusedCard.outlineOffset === '0px', 'Keyboard focus must outline the whole image card.');
    const hoverRule = [...document.styleSheets].flatMap(sheet => [...sheet.cssRules]).find(rule => rule.selectorText === '.media-grid button.media-card:hover:not(:disabled)');
    assert(hoverRule?.style.outlineStyle === 'solid' && hoverRule.style.outlineWidth === '2px' && hoverRule.style.outlineOffset === '0px', 'Hover must use an outer outline that the image cannot cover.');
    assert(!library().querySelector('.media-grid a, .media-grid button:not(.media-card)'), 'Choosing an image must not require a separate button or open another window.');
    assert(!library().querySelector('[name=format]'), 'Browsing must not require an image-format filter.');
    assert(library().getBoundingClientRect().right <= innerWidth, 'Library overflows viewport.');
    assert(library().scrollWidth <= library().clientWidth, 'Library content overflows horizontally.');
    const filters = library().querySelector('[data-media-filters]');
    assertFilterHeights(filters);
    for (const control of filters.querySelectorAll('input:not([type=hidden]), select, button')) {
      const box = control.getBoundingClientRect();
      assert(box.left >= 0 && box.right <= innerWidth, 'Search controls overflow the viewport.');
    }
    const bounds = library().getBoundingClientRect();
    assert(bounds.top >= innerHeight * .09 && bounds.bottom <= innerHeight * .91, 'Chooser must leave space above and below its distinct frame.');
    const heading = library().querySelector('header');
    const headingTop = heading.getBoundingClientRect().top;
    const results = library().querySelector('[data-media-results]');
    results.scrollTop = results.scrollHeight;
    assert(heading.getBoundingClientRect().top === headingTop && library().scrollTop === 0, 'Scrolling images must not move the heading or Cancel button.');
    results.scrollTop = 0;
  }
  async function choose(button) {
    await open(button);
    const select = library().querySelector('[data-media-select]');
    const id = select.dataset.mediaSelect;
    select.querySelector('img').click();
    await wait(() => !document.querySelector('.media-browser'));
    return id;
  }
  async function filter(values) {
    const form = library().querySelector('[data-media-filters]');
    const layout = element => {
      const origin = element.getBoundingClientRect();
      return [...element.querySelectorAll('input:not([type=hidden]), select, button')].map(control => {
        const box = control.getBoundingClientRect();
        return [box.left - origin.left, box.top - origin.top, box.width, box.height];
      });
    };
    const before = layout(form);
    for (const [name, value] of Object.entries(values)) form.elements[name].value = value;
    form.requestSubmit();
    await wait(() => !library().querySelector('[data-media-results]').hasAttribute('aria-busy'));
    assert(library().open, 'Filtering closed the chooser.');
    const updated = library().querySelector('[data-media-filters]');
    const after = layout(updated);
    assert(before.every((box, i) => box.every((value, j) => Math.abs(value - after[i][j]) < 1)), 'Clear filters must not resize or move the search controls.');
    const clear = library().querySelector('.media-clear-filters');
    if (clear) assert(clear.getBoundingClientRect().top >= updated.getBoundingClientRect().bottom, 'Clear filters must appear below the controls.');
  }
  function assertDefaultResetIsNoOp(form) {
    const picker = form.querySelector('.page-preview');
    const value = picker.querySelector('[data-image-value]');
    const status = picker.querySelector('[data-image-status]');
    const draftStatus = form.querySelector('[data-draft-status]');
    const before = [status.textContent, draftStatus.outerHTML, picker.querySelector('img').getAttribute('src')];
    let changes = 0;
    const changed = () => changes++;
    form.addEventListener('input', changed);
    form.addEventListener('change', changed);
    assert(value.value === '', 'No-op reset test requires the default selection.');
    picker.querySelector('[data-image-reset]').click();
    form.removeEventListener('input', changed);
    form.removeEventListener('change', changed);
    assert(changes === 0 && value.value === '', 'Resetting the default image must not emit changes.');
    assert(status.textContent === before[0] && draftStatus.outerHTML === before[1] && picker.querySelector('img').getAttribute('src') === before[2], 'Resetting the default image must preserve status, draft state, and preview.');
  }
  try {
    const stage = sessionStorage.getItem('library-stage');
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('library-stage', 'logged-in');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'logged-in') {
      sessionStorage.setItem('library-stage', 'post');
      location.assign('/post/create');
      return;
    }
    if (stage === 'post' || stage === 'page') {
      const form = document.querySelector(stage === 'post' ? '#post-form' : '#page-form');
      await wait(() => form.querySelector('.CodeMirror'));
      for (const [selector, filename, label] of [['.tool-image', 'image-up.svg', 'Upload image'], ['.tool-library', 'images.svg', 'Choose existing image']]) {
        const button = form.querySelector(selector);
        const icon = getComputedStyle(button, '::before');
        assert(button.title === label && icon.maskImage.includes('/lib/lucide/' + filename), 'Image toolbar must use the matching SVG and descriptive tooltip.');
        assert(icon.width === '16px' && icon.height === '16px', 'Image icons must have consistent dimensions.');
        assert(new DOMMatrix(icon.transform).m42 === -1, 'SVG icons must align optically with the text-based toolbar icons.');
        const image = new Image();
        image.src = '/assets/lib/lucide/' + filename;
        await image.decode();
        assert(image.naturalWidth === 24, 'Vendored toolbar icon must load as an SVG.');
      }
      assertDefaultResetIsNoOp(form);
      form.elements.title.value = 'Unsaved ' + stage;
      form.elements.title.dispatchEvent(new Event('input', {bubbles: true}));
      const editor = form.querySelector('.CodeMirror').CodeMirror;
      editor.setValue('Keep this writing.\n\n');
      editor.setCursor({line: 2, ch: 0});
      const browse = form.querySelector('[data-media-browse]');
      const preview = form.querySelector('.page-preview');
      const frame = preview.querySelector('.preview-image-frame');
      const previewBounds = preview.getBoundingClientRect();
      if (innerWidth < 768) {
        const imageBox = frame.getBoundingClientRect();
        const actionsBox = preview.querySelector('.preview-image-actions').getBoundingClientRect();
        assert(Math.abs(imageBox.left - actionsBox.left) < 1 && Math.abs(imageBox.right - actionsBox.right) < 1, 'Mobile preview must span the full width of the upload and library buttons.');
      } else {
        assert(frame.getBoundingClientRect().width === 160, 'Desktop editor preview should remain a compact thumbnail.');
      }
      const defaultHint = preview.querySelector('.preview-image-controls .form-hint').textContent;
      assert(stage === 'post' ? defaultHint === 'By default, the preview uses the first suitable image in the post, or the site portrait if none is available.' : defaultHint.startsWith('Use default image '), 'Default-image guidance must describe the correct fallback for the content type.');
      const help = preview.querySelector('.preview-image-help');
      assert(!help.closest('details') && help.getBoundingClientRect().height > 0, 'Image requirements and privacy must be visible without expanding a disclosure.');
      assert(help.tagName === 'P' && help.textContent === 'PNG, JPEG, or GIF images up to 8 MiB and 20 million pixels stay private unless used in public content.', 'Visible help must combine limits and privacy in one sentence.');
      const helpStyle = getComputedStyle(help);
      assert(helpStyle.marginTop === helpStyle.paddingTop && parseInt(helpStyle.marginTop) >= 16, 'Divider spacing must be balanced above and below.');
      const upload = preview.querySelector('input[type=file]');
      const uploadButton = preview.querySelector('[data-image-upload]');
      assert(uploadButton.textContent === 'Upload image' && uploadButton.getAttribute('aria-describedby') === help.id, 'Upload button must name its action and describe requirements.');
      assert(browse.textContent === 'Browse library', 'Library button must describe browsing images.');
      assert(getComputedStyle(upload).display === 'none', 'Native file control must not consume editor space.');
      const uploadBounds = uploadButton.getBoundingClientRect();
      const browseBounds = browse.getBoundingClientRect();
      assert(Math.abs(uploadBounds.width - browseBounds.width) < 1 && uploadBounds.height === browseBounds.height && uploadBounds.top === browseBounds.top, 'Upload and Browse must be equal-sized buttons on the same row.');
      const uploadStyle = getComputedStyle(uploadButton);
      for (const button of [uploadButton, browse]) {
        const style = getComputedStyle(button);
        for (const property of ['fontSize', 'lineHeight', 'padding', 'borderRadius', 'backgroundColor']) {
          assert(style[property] === uploadStyle[property], 'Image action styling differs: ' + property);
        }
        assert(button.getBoundingClientRect().height >= 44, 'Image actions need touch-sized targets.');
      }
      const reset = preview.querySelector('[data-image-reset]');
      assert(reset.textContent === 'Use default image' && reset.classList.contains('text-button'), 'Reset must be a default-image text action.');
      const resetBox = reset.getBoundingClientRect();
      const frameBox = frame.getBoundingClientRect();
      assert(resetBox.top >= frameBox.bottom && Math.abs(resetBox.left + resetBox.width / 2 - frameBox.left - frameBox.width / 2) < 1, 'Default-image action must be centered directly below the preview.');
      const tips = form.querySelector('.editor-help');
      assert(tips && !tips.open && tips.textContent.includes('256 KiB'), 'Secondary writing guidance must be available without taking up editor space.');
      let fileClicks = 0;
      const fileClick = event => { event.preventDefault(); fileClicks++; };
      upload.addEventListener('click', fileClick);
      uploadButton.click();
      upload.removeEventListener('click', fileClick);
      assert(fileClicks === 1, 'Upload action must activate the native file picker.');
      const originalUploadFetch = window.fetch;
      let finishUpload;
      window.fetch = (path, options) => path === '/post/images/upload' ? new Promise(resolve => { finishUpload = resolve; }) : originalUploadFetch(path, options);
      const files = new DataTransfer();
      files.items.add(new File(['invalid'], 'invalid.png', {type: 'image/png'}));
      upload.files = files.files;
      upload.dispatchEvent(new Event('change', {bubbles: true}));
      await wait(() => finishUpload);
      assert(uploadButton.disabled && browse.disabled && reset.disabled, 'All image actions must be disabled while an upload is pending.');
      finishUpload(new Response(JSON.stringify({error: 'Upload failed.'}), {status: 500, headers: {'Content-Type': 'application/json'}}));
      await wait(() => !form.dataset.uploading);
      window.fetch = originalUploadFetch;
      assert(!uploadButton.disabled && !browse.disabled && !reset.disabled && upload.value === '', 'Failed uploads must restore all image actions.');
      assert(preview.querySelector('[data-image-status]').textContent.includes('Upload failed'), 'Failed upload must remain visible.');
      assert(preview.scrollWidth <= preview.clientWidth, 'Preview picker must not overflow horizontally.');
      for (const control of preview.querySelectorAll('button')) {
        const box = control.getBoundingClientRect();
        assert(box.left >= previewBounds.left && box.right <= previewBounds.right, 'Preview controls must fit the editor at every viewport.');
      }
      if (stage === 'post') assert(getComputedStyle(frame.querySelector('.image-empty')).display !== 'none', 'An empty post selection should explain the automatic image.');
      // Portraits must grow taller rather than be cropped into a landscape slot.
      for (const [width, height] of [[120, 1200], [1200, 120], [96, 96]]) {
        const key = 'preview-test-' + width + '-' + height;
        if (stage === 'post') {
          const canvas = document.createElement('canvas');
          canvas.width = width; canvas.height = height;
          canvas.getContext('2d').fillRect(0, 0, width, height);
          const files = new DataTransfer();
          files.items.add(new File([await new Promise(resolve => canvas.toBlob(resolve))], 'preview.png', {type: 'image/png'}));
          upload.files = files.files;
          upload.dispatchEvent(new Event('change', {bubbles: true}));
          await wait(() => !form.dataset.uploading);
          sessionStorage.setItem(key, form.elements.previewImage.value);
        } else {
          form.elements.previewImage.value = sessionStorage.getItem(key);
          form.elements.previewImage.dispatchEvent(new Event('change', {bubbles: true}));
        }
        assert(form.elements.previewImage.value, 'Preview test upload did not succeed.');
        const image = frame.querySelector('img');
        await wait(() => image.complete && image.currentSrc === image.src);
        await image.decode();
        const box = image.getBoundingClientRect();
        const bounds = frame.getBoundingClientRect();
        assert(Math.abs(box.width - box.height * width / height) < 1, `Selected preview must preserve image proportions: ${stage} ${width}x${height}, displayed ${box.width}x${box.height}, natural ${image.naturalWidth}x${image.naturalHeight}, ${getComputedStyle(image).aspectRatio}.`);
        assert(box.left >= bounds.left && box.right <= bounds.right && box.top >= bounds.top && box.bottom <= bounds.bottom, 'Selected preview must fit without clipping.');
        assert(box.width <= image.naturalWidth && box.height <= 240, 'Selected preview must remain bounded without enlarging small images.');
        if (height > width) assert(box.height === 240, 'Tall preview must get more height than the landscape placeholder.');
      }
      reset.click();
      await open(browse);
      // A late response must not replace a newer search, even if fetch ignores cancellation.
      if (stage === 'post') {
        const originalFetch = window.fetch;
        let release;
        window.fetch = (path, options) => String(path).includes('q=stale-search') ? new Promise(resolve => { release = resolve; }) : originalFetch(path, options);
        const search = library().querySelector('[data-media-filters]');
        search.elements.q.value = 'stale-search';
        search.requestSubmit();
        await wait(() => release);
        await filter({q: '.png'});
        release(new Response('<section data-media-library>Stale results</section>'));
        await new Promise(resolve => setTimeout(resolve, 0));
        assert(library().querySelector('[name=q]').value === '.png', 'An old response replaced the current search.');
        window.fetch = originalFetch;
      }
      await filter({q: 'not-an-existing-image'});
      assert(!library().querySelector('[data-media-select]') && library().textContent.includes('No images match'), 'Empty search did not explain the result.');
      assert(form.elements.title.value === 'Unsaved ' + stage && editor.getValue() === 'Keep this writing.\n\n', 'Filtering lost unsaved writing.');
      library().querySelector('[data-media-page]').click();
      await wait(() => !library().querySelector('[data-media-results]').hasAttribute('aria-busy'));
      assert(library().querySelectorAll('[data-media-select]').length === 24, 'Clearing filters did not restore images.');
      library().querySelector('[data-close-dialog]').click();
      await wait(() => !document.querySelector('.media-browser'));
      assert(form.elements.previewImage.value === '', 'Cancel changed the selection.');
      assert(document.activeElement === browse, 'Cancel did not restore focus.');
      await open(browse);
      await filter({q: '.png', usage: 'unused'});
      library().querySelector('.media-pagination [data-media-page]').click();
      await wait(() => library()?.textContent.includes('Page 2 of 2') && library().querySelector('[data-media-select]'));
      assert(library().querySelectorAll('[data-media-select]').length === 4, 'Final page must contain the remaining fixture and three preview-test uploads.');
      assert(library().querySelector('[name=q]').value === '.png' && library().querySelector('[name=usage]').value === 'unused', 'Pagination lost search filters.');
      const choice = library().querySelector('[data-media-select]');
      const id = choice.dataset.mediaSelect;
      choice.querySelector('.media-card-caption').click();
      // close() clears open before the close event removes the dialog. Wait for
      // removal before clicking another chooser, which guards against duplicates.
      await wait(() => !document.querySelector('.media-browser'));
      assert(form.elements.previewImage.value === id, 'Preview selection stored the wrong ID.');
      assert(form.querySelector('[data-image-preview]').getAttribute('src') === '/images/' + id, 'Preview did not update.');
      if (stage === 'post') assert(getComputedStyle(frame.querySelector('.image-empty')).display === 'none', 'The automatic-image placeholder must disappear after selection.');
      assert(form.elements.title.value === 'Unsaved ' + stage && editor.getValue() === 'Keep this writing.\n\n', 'Selection lost unsaved writing.');
      reset.click();
      assert(form.elements.previewImage.value === '', 'Default-image action must clear the explicit selection.');
      assert(preview.querySelector('[data-image-status]').textContent === 'Selection cleared. Save to apply.', 'Clearing a custom image must still request saving.');
      assertDefaultResetIsNoOp(form);
      const resetPreview = preview.querySelector('[data-image-preview]');
      assert(stage === 'post' ? resetPreview.hidden : resetPreview.getAttribute('src') === preview.dataset.imageDefault, 'Default-image action must restore the appropriate preview.');
      assert(form.elements.title.value === 'Unsaved ' + stage && editor.getValue() === 'Keep this writing.\n\n', 'Default-image action lost unsaved writing.');
      const bodyImage = await choose(form.querySelector('.tool-library'));
      assert(editor.getValue().includes('![](/images/' + bodyImage + ')'), 'Existing image was not inserted into Markdown.');
      assert(form.elements.content.value === editor.getValue(), 'Markdown textarea was not synchronized.');
      if (stage === 'post') {
        // The same library must stack above an HTMX editor without closing it.
        await htmx.ajax('GET', '/pages/create', {target: '#dialog-host'});
        const parent = document.querySelector('#dialog-host dialog[open]');
        assert(parent, 'Editor dialog did not open.');
        await choose(parent.querySelector('[data-media-browse]'));
        assert(parent.open && parent.querySelector('[data-image-value]').value, 'Choosing closed or failed to update the editor dialog.');
        parent.close();
        parent.remove();
      }
      sessionStorage.setItem('library-stage', stage === 'post' ? 'page' : 'profile');
      location.assign(stage === 'post' ? '/pages/create' : '/profile');
      return;
    }
    if (stage === 'upload') {
      assertFilterHeights(document.querySelector('[data-media-filters]'));
      const form = document.querySelector('[data-media-upload]');
      assert(form && !form.hidden, 'Media page must offer direct uploads.');
      const disclosure = form.querySelector('details');
      assert(disclosure && !disclosure.open && form.getBoundingClientRect().height <= 60, 'Upload controls must start collapsed so browsing stays prominent.');
      const summary = disclosure.querySelector('summary');
      assert(summary.getBoundingClientRect().height >= 44, 'Upload disclosure must have a touch-sized target.');
      if (innerWidth < 768) {
        const filters = document.querySelector('[data-media-filters]');
        assert(filters.getBoundingClientRect().height < 170, 'Mobile search controls should fit into two compact rows.');
        const usage = filters.elements.usage.getBoundingClientRect();
        const submit = filters.querySelector('button').getBoundingClientRect();
        assert(Math.abs(usage.bottom - submit.bottom) < 1 && submit.left > usage.right, 'Usage and Search must align on the second row.');
      }
      summary.click();
      assert(disclosure.open, 'Upload controls must expand without leaving the Media page.');
      const bounds = form.getBoundingClientRect();
      for (const control of form.querySelectorAll('input, button')) {
        const box = control.getBoundingClientRect();
        assert(box.left >= bounds.left && box.right <= bounds.right, 'Upload controls must fit the panel at every viewport.');
      }
      if (innerWidth >= 768) assert(bounds.height < 200, 'Desktop upload panel should leave the images in view.');
      for (const card of document.querySelectorAll('.media-card')) {
        assert(card.querySelector('.media-filename') && card.querySelector('time'), 'Image and metadata must share the details link.');
        assert(getComputedStyle(card.querySelector('time')).whiteSpace === 'nowrap', 'Dates must not split across lines.');
        assert(card.querySelector('.media-card-meta').textContent.match(/(?:B|KiB|MiB) stored/), 'Cards must show readable storage units.');
      }
      assert(document.querySelector('a[href="/media/unused"]').classList.contains('page-create'), 'Cleanup must use the management button style.');
      const input = form.elements.image;
      const chooseFile = file => { const transfer = new DataTransfer(); transfer.items.add(file); input.files = transfer.files; };
      chooseFile(new File(['not an image'], 'invalid.png', {type: 'image/png'}));
      form.requestSubmit();
      await wait(() => form.querySelector('[data-upload-status]').textContent.includes('valid PNG'));
      assert(!input.disabled && !form.querySelector('button').disabled, 'Upload failure must allow retry.');
      const canvas = document.createElement('canvas');
      canvas.width = canvas.height = 12;
      const context = canvas.getContext('2d');
      context.fillStyle = '#ce7835'; context.fillRect(0, 0, 12, 12);
      chooseFile(new File([await new Promise(resolve => canvas.toBlob(resolve))], 'library.png', {type: 'image/png'}));
      sessionStorage.setItem('library-stage', 'uploaded');
      form.requestSubmit();
      return;
    }
    if (stage === 'uploaded') {
      assert(location.pathname.startsWith('/media/') && document.querySelector('h1').textContent === 'Image details', 'Upload must open its details.');
      assert(document.body.textContent.includes('Not used by saved posts'), 'New upload should show no saved uses.');
      const image = document.querySelector('.media-detail-preview img');
      await wait(() => image.complete);
      assert(image.naturalWidth === 12, 'Direct upload did not produce the expected image.');
      const preview = document.querySelector('.media-detail-preview').getBoundingClientRect();
      const info = document.querySelector('.media-detail-info').getBoundingClientRect();
      if (innerWidth >= 768) {
        assert(info.left > preview.right && Math.abs(info.top - preview.top) < 1, 'Image details must use side-by-side columns on wider screens.');
      } else {
        assert(info.top > preview.bottom && Math.abs(info.left - preview.left) < 1, 'Mobile image details must stack in one column.');
      }
      const layout = document.querySelector('.media-detail-layout').getBoundingClientRect();
      const usage = document.querySelector('.media-detail-usage').getBoundingClientRect();
      assert(usage.top > layout.bottom && Math.abs(usage.width - layout.width) < 1, 'Image usage must span the width below the preview and label.');
      const sections = document.querySelectorAll('.media-detail-usage > section');
      assert(sections.length === 2 && sections[1].textContent.includes('No saved versions use this image.'), 'Usage lists must show clear empty states.');
      const current = sections[0].getBoundingClientRect();
      const history = sections[1].getBoundingClientRect();
      if (innerWidth >= 768) {
        assert(history.left > current.right && Math.abs(history.top - current.top) < 1 && Math.abs(history.width - current.width) < 1, 'Usage lists must occupy equal columns on wider screens.');
      } else {
        assert(history.top > current.bottom && Math.abs(history.left - current.left) < 1, 'Usage lists must stack on mobile.');
      }
      for (const section of sections) {
        const heading = getComputedStyle(section.querySelector('h2'));
        assert(heading.backgroundColor === 'rgb(243, 244, 248)' && heading.borderTopWidth === '0px' && heading.borderBottomWidth === '0px', 'Usage headings must have a shaded background without borders.');
      }
      assert(document.documentElement.scrollWidth <= innerWidth, 'Image details overflow the viewport.');
      const listed = new DOMParser().parseFromString(await (await fetch('/media')).text(), 'text/html');
      assert(listed.querySelector('.media-grid a').getAttribute('href') === location.pathname, 'Newest upload should link to its details.');
      assert(document.body.textContent.includes('Original filename: library.png'), 'Original filename was not retained.');
      sessionStorage.setItem('library-stage', 'renamed');
      sessionStorage.setItem('library-upload-path', location.pathname);
      const form = document.querySelector('.media-label-form');
      form.elements.label.value = 'Été 100%_photo';
      form.requestSubmit();
      return;
    }
    if (stage === 'renamed') {
      assert(document.querySelector('[name=label]').value === 'Été 100%_photo', 'Renaming lost the label.');
      sessionStorage.setItem('library-stage', 'search-page');
      location.assign('/media');
      return;
    }
    if (stage === 'search-page') {
      sessionStorage.setItem('library-stage', 'filtered-page');
      const form = document.querySelector('[data-media-filters]');
      form.elements.q.value = 'ÉTÉ 100%_';
      form.elements.usage.value = 'unused';
      form.requestSubmit();
      return;
    }
    if (stage === 'filtered-page') {
      const filters = document.querySelector('[data-media-filters]');
      const clear = document.querySelector('.media-clear-filters');
      assert(clear && !filters.contains(clear) && clear.getBoundingClientRect().top >= filters.getBoundingClientRect().bottom, 'Full-page Clear filters must sit below the search controls.');
      const cards = document.querySelectorAll('.media-card');
      assert(cards.length === 1 && cards[0].getAttribute('href') === sessionStorage.getItem('library-upload-path'), 'Full-page search returned the wrong image.');
      assert(cards[0].textContent.includes('Été 100%_photo') && document.body.textContent.includes('1 image found'), 'Search label or count is incorrect.');
      await report('PASS');
      return;
    }
    const form = document.querySelector('#profile-form');
    for (const field of ['imageURL', 'faviconURL', 'backgroundURL']) {
      const picker = form.elements[field].closest('[data-image-picker]');
      const id = await choose(picker.querySelector('[data-media-browse]'));
      assert(form.elements[field].value === '/images/' + id, 'Profile selection must store the URL.');
      assert(picker.querySelector('[data-image-preview]').getAttribute('src') === '/images/' + id, 'Profile preview did not update.');
    }
    // A selected file can disappear between listing and choosing. Do not replace the old selection.
    const picker = form.elements.imageURL.closest('[data-image-picker]');
    const previous = form.elements.imageURL.value;
    const originalFetch = window.fetch;
    window.fetch = (path, options) => path === '/media?pick=1' ? Promise.resolve(new Response('', {status: 500})) : originalFetch(path, options);
    picker.querySelector('[data-media-browse]').click();
    await wait(() => library()?.querySelector('[data-media-status]').textContent.includes('Unable to load'));
    assert(form.elements.imageURL.value === previous, 'Failed library load changed the selection.');
    library().querySelector('[data-close-dialog]').click();
    await wait(() => !document.querySelector('.media-browser'));
    window.fetch = originalFetch;

    await open(picker.querySelector('[data-media-browse]'));
    window.fetch = (path, options) => options?.method === 'HEAD' ? Promise.resolve(new Response('', {status: 500})) : originalFetch(path, options);
    library().querySelector('[data-media-select]').click();
    await wait(() => library()?.querySelector('[data-media-status]').textContent.includes('Unable to check'));
    assert(form.elements.imageURL.value === previous, 'Failed image check changed the selection.');
    library().querySelector('[data-close-dialog]').click();
    await wait(() => !document.querySelector('.media-browser'));
    window.fetch = originalFetch;

    await open(picker.querySelector('[data-media-browse]'));
    const choice = library().querySelector('[data-media-select]');
    const confirmation = await fetch('/media/delete/' + choice.dataset.mediaSelect);
    const doc = new DOMParser().parseFromString(await confirmation.text(), 'text/html');
    const version = doc.querySelector('[name=version]').value;
    const deletion = await fetch('/media/delete/' + choice.dataset.mediaSelect, {method: 'POST', body: new URLSearchParams({version})});
    assert(deletion.ok, 'Fixture deletion failed.');
    choice.click();
    await wait(() => library()?.querySelector('[data-media-status]').textContent.includes('no longer available'));
    assert(form.elements.imageURL.value === previous, 'Failed selection overwrote the previous value.');
    library().querySelector('[data-close-dialog]').click();
    await wait(() => !document.querySelector('.media-browser'));
    const listed = await fetch('/media?pick=1');
    const remaining = new DOMParser().parseFromString(await listed.text(), 'text/html');
    assert(remaining.querySelectorAll('[data-media-select]').length === 24 && remaining.body.textContent.includes('27 images') && remaining.body.textContent.includes('Page 1 of 2'), 'Selection uploaded copies beyond the three preview-test images.');
    sessionStorage.setItem('library-stage', 'upload');
    location.assign('/media');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
