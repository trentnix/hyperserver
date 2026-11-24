(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  if (location.pathname === '/post/s/imported-markdown') {
    try {
      const article = document.querySelector('#article-content');
      const tall = article.querySelector('img[alt="Tall diagram"]');
      const first = article.querySelector('img[alt="First"]');
      const second = article.querySelector('img[alt="Second"]');
      assert(article.querySelector('.page-status')?.textContent.includes('Draft preview'), 'The imported post must be saved as a draft.');
      assert(tall?.dataset.markdownImageSize === 'custom' && tall.title === 'Overview' &&
        tall.style.getPropertyValue('--markdown-image-max-height') === '480px', 'The saved image must retain its size, alt text, and title.');
      assert(first?.closest('a').getAttribute('href') === '/about' && first.src !== second?.src,
        'The saved images must retain authored links and point to the intended different files.');
      await waitForComponent(() => [tall, first, second].every(image => image?.naturalWidth > 0), 'imported images to load');
      await report('PASS');
    } catch (error) { await report(error.message + '\n' + (error.stack || '')); }
    return;
  }
  try {
    const original = document.querySelector('#post-form');
    const mobile = matchMedia('(max-width: 767px)').matches;
    const recoveredSummary = 'Keep this recovered summary.';
    const recoveredBody = mobile ? 'Recover this writing.' : '';
    await waitForComponent(() => original.querySelector('.CodeMirror')?.CodeMirror &&
      !original.querySelector('[data-draft-status]').hidden, 'new-post editor and recovery');
    original.elements.summary.value = recoveredSummary;
    original.elements.summary.dispatchEvent(new Event('input', {bubbles: true}));
    if (recoveredBody) original.querySelector('.CodeMirror').CodeMirror.setValue(recoveredBody);
    const navigation = htmx.ajax('GET', '/post/create', '#article-content');
    document.querySelector('.editor-confirmation[open] [data-confirm-accept]').click();
    await navigation;
    await waitForComponent(() => !original.isConnected &&
      document.querySelector('[data-draft-recovery]:not([hidden])'), 'reopened editor and recovery copy');

    const form = document.querySelector('#post-form');
    form.querySelector('[data-draft-restore]').click();
    assert(form.elements.summary.value === recoveredSummary && form.elements.content.value === recoveredBody,
      'Restore copy must recover the summary and body.');
    const actions = document.querySelector('[data-markdown-import-actions]');
    const directImport = actions.querySelector(':scope > button');
    const menu = actions.querySelector('details');
    const more = menu.querySelector('summary');
    const menuImport = menu.querySelector('button');
    const opener = mobile ? more : directImport;
    const visible = element => element.getClientRects().length > 0;
    const components = [];
    if (mobile) {
      const addCallout = form.querySelector('[data-component-add="callout"]');
      const fetchPosition = window.fetch;
      let finishPosition;
      window.fetch = (url, options) => {
        if (url !== '/post/component-position') return fetchPosition(url, options);
        window.fetch = fetchPosition;
        return new Promise(resolve => { finishPosition = () => resolve(fetchPosition(url, options)); });
      };
      for (const title of ['First draft callout', 'Second draft callout']) {
        const adding = addTestComponent(addCallout);
        if (!components.length) {
          await waitForComponent(() => finishPosition && menuImport.disabled, 'Import blocked during component placement');
          finishPosition();
        }
        const callout = await adding;
        setTestComponentSource(callout, {title, body: 'Unsaved component content.'});
        await closeComponentSettings(form);
        const source = callout.querySelector('textarea[name]');
        assert(!source.disabled && form.elements.componentOrder.value.includes(source.name), 'The test must have a component to replace.');
        components.push(source);
      }
      form.querySelector('[name="published"][value="on"]').click();
    }
    const beforeBody = form.elements.content.value;
    const beforeOrder = form.elements.componentOrder.value;
    const beforePublication = form.elements.published.value;
    const beforeComponents = components.map(source => source.value);
    function openImport() {
      opener.focus();
      opener.click();
      if (mobile) {
        assert(visible(menuImport), 'The More menu must show Import.');
        menuImport.focus();
        menuImport.click();
        assert(!menu.open, 'Opening Import must close the More menu.');
      }
    }
    const dialog = document.querySelector('[data-markdown-import]');
    const source = dialog.querySelector('#markdown-import-source');
    const folder = dialog.querySelector('#markdown-import-folder');
    const picker = dialog.querySelector('[data-markdown-import-picker]');
    const pickFolder = dialog.querySelector('[data-markdown-import-pick-folder]');
    const selection = dialog.querySelector('[data-markdown-import-selection]');
    const openDraft = dialog.querySelector('[data-markdown-import-submit]');
    const cancel = dialog.querySelector('[data-markdown-import-cancel]');
    const status = dialog.querySelector('[data-markdown-import-status]');
    async function expectPickerError(label, message) {
      await waitForComponent(() => status.textContent.includes(message), message);
      assert(status.textContent.startsWith(`Could not import “${label}”:\n`) && getComputedStyle(status).whiteSpace === 'pre-line' &&
        status.getAttribute('role') === 'alert' &&
        !picker.hidden && selection.hidden && visible(pickFolder) &&
        status.getBoundingClientRect().bottom <= picker.getBoundingClientRect().top,
        'A failure must identify the source and show the picker directly below the error.');
    }
    async function rejectSelection(label, message) {
      await expectPickerError(label, message);
      assert(openDraft.disabled && document.activeElement === pickFolder && !folder.files.length && !source.files.length,
        'An invalid selection must block submission and allow choosing the same source again.');
    }
    const select = (input, ...files) => {
      const transfer = new DataTransfer();
      for (const file of files) transfer.items.add(file);
      input.files = transfer.files;
      input.dispatchEvent(new Event('change', {bubbles: true}));
    };
    async function image(name, relative, color) {
      const canvas = document.createElement('canvas');
      canvas.width = canvas.height = 16;
      const context = canvas.getContext('2d');
      context.fillStyle = color;
      context.fillRect(0, 0, 16, 16);
      const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
      const file = new File([blob], name, {type: 'image/png'});
      Object.defineProperty(file, 'webkitRelativePath', {value: relative});
      return file;
    }
    const fromMarkdown = (contents, name, relative = '') => {
      const file = new File([contents], name, {type: 'text/markdown'});
      Object.defineProperty(file, 'webkitRelativePath', {value: relative});
      return file;
    };
    const fileEntry = file => ({name: file.name, isFile: true, file: resolve => resolve(file)});
    const folderEntry = (name, ...entries) => ({
      name, isDirectory: true,
      createReader() {
        let cursor = 0;
        return {readEntries: resolve => resolve(entries.slice(cursor, ++cursor))};
      }
    });
    function drop(entry) {
      const transfer = new DataTransfer();
      const item = transfer.items.add(new File([''], entry.name));
      Object.defineProperty(item, 'webkitGetAsEntry', {value: () => entry});
      const over = new DragEvent('dragover', {dataTransfer: transfer, cancelable: true});
      picker.dispatchEvent(over);
      assert(over.defaultPrevented && picker.hasAttribute('data-drag-over'), 'A file drag should highlight the import target.');
      picker.dispatchEvent(new DragEvent('drop', {dataTransfer: transfer, cancelable: true}));
      assert(!picker.hasAttribute('data-drag-over'), 'The drop should clear the highlight.');
    }

    await waitForComponent(() => !actions.hidden, 'Markdown import control');
    assert(visible(more) === mobile && visible(directImport) !== mobile && !visible(menuImport),
      'Phone widths must put Import inside More. Wider screens must show Import directly.');
    openImport();
    assert(dialog.matches(':modal') && pickFolder.getClientRects().length, 'The import dialog must offer folder selection.');
    assert(!source.getClientRects().length && !folder.getClientRects().length && selection.hidden,
      'The native folder and Markdown inputs must stay hidden behind the picker actions.');
    assert(dialog.scrollWidth <= dialog.clientWidth + 1, 'Import dialog content must fit the viewport.');
    select(source, fromMarkdown('\x00', 'invalid.md'));
    await rejectSelection('invalid.md', 'UTF-8');
    select(source, fromMarkdown('', 'empty.md'));
    await rejectSelection('empty.md', 'The Markdown file is empty');
    select(source, fromMarkdown('A'.repeat(256 * 1024 + 1), 'too large.md'));
    await rejectSelection('too large.md', '256 KiB');
    select(source, fromMarkdown('Post body.\n\n<!-- hyperserver:component event -->', 'with-component.md'));
    await rejectSelection('with-component.md', 'Remove the blog component placement markers');
    select(folder, await image('unused.png', 'empty/unused.png', '#723d80'));
    await rejectSelection('empty', 'No Markdown file found');
    const firstArticle = fromMarkdown('# First', 'first.md', 'articles/first.md');
    const secondArticle = fromMarkdown('# Second', 'second.markdown', 'articles/notes/second.markdown');
    if (mobile) select(folder, firstArticle, secondArticle);
    else drop(folderEntry('articles', fileEntry(firstArticle), folderEntry('notes', fileEntry(secondArticle))));
    await rejectSelection('articles', 'more than one Markdown file');

    const picture = await image('photo.png', 'article/photo.png', '#432fa5');
    const missing = '![Photo](assets/photo.png)';
    select(source, fromMarkdown(missing, 'missing.md'));
    await rejectSelection('missing.md', 'This file uses local images. Use Choose folder to import the file and images together.');
    select(folder, fromMarkdown(missing, 'missing.md', 'article/missing.md'), picture);
    await rejectSelection('article', 'Missing image: assets/photo.png');
    select(folder, fromMarkdown('![Photo](../shared/photo.png)', 'outside.md', 'article/outside.md'), picture);
    await rejectSelection('article', 'Image is outside the chosen folder: ../shared/photo.png');
    select(folder, fromMarkdown('# Unused image', 'extra.md', 'article/extra.md'), picture);
    await rejectSelection('article', 'Extra file: photo.png');

    const broken = new File(['not an image'], 'broken.png', {type: 'image/png'});
    Object.defineProperty(broken, 'webkitRelativePath', {value: 'article/broken.png'});
    select(folder, fromMarkdown('![Broken](broken.png)', 'broken.md', 'article/broken.md'), broken);
    await waitForComponent(() => !openDraft.disabled, 'folder for a damaged image');
    openDraft.click();
    document.querySelector('.editor-confirmation[open] [data-confirm-accept]')?.click();
    await rejectSelection('article', '“broken.png” failed to upload. Choose a valid PNG, JPEG, or GIF');
    assert(dialog.open && form.elements.content.value === beforeBody &&
      form.elements.componentOrder.value === beforeOrder, 'A damaged image must fail import and preserve the post.');

    form.setAttribute('data-upload-storage-full', '');
    select(folder, fromMarkdown('![Photo](photo.png)', 'photo.md', 'article/photo.md'), picture);
    await rejectSelection('article', 'Upload storage is full.');
    const plain = fromMarkdown('# A text-only import', 'plain.md');
    if (innerWidth === 320) select(source, plain);
    else drop(fileEntry(plain));
    await waitForComponent(() => !openDraft.disabled, 'a text-only import with full upload storage');
    form.removeAttribute('data-upload-storage-full');
    cancel.click();
    await waitForComponent(() => !dialog.open, 'cancel import');
    assert(form.elements.content.value === beforeBody && document.activeElement === opener, 'Cancel must preserve writing and return focus.');

    const markdown = 'Café 🌱\r\n\r\n{size=custom max-height=480}![Tall diagram](art/tall%20view.png "Overview")\n\n' +
      '[![First][figure]](/about)\n\n![Second](b/compare.png)\n\n' +
      '[Online](https://example.com/guide) [Plain link][figure]\n\n[figure]: ../shared/compare.png';
    openImport();
    const first = await image('compare.png', 'bundle/shared/compare.png', '#48804e');
    const second = await image('compare.png', 'bundle/article/b/compare.png', '#904a37');
    const tall = await image('tall view.png', 'bundle/article/art/tall view.png', '#3577aa');
    const post = fromMarkdown(markdown, 'Imported Markdown.md', 'bundle/article/Imported Markdown.md');
    if (innerWidth === 320) {
      select(folder, post, first, second, tall);
    } else {
      drop(folderEntry('bundle', folderEntry('shared', fileEntry(first)),
        folderEntry('article', fileEntry(post), folderEntry('art', fileEntry(tall)), folderEntry('b', fileEntry(second)))));
    }
    await waitForComponent(() => !openDraft.disabled, 'all referenced images matched by folder paths');
    assert(!selection.hidden && !pickFolder.getClientRects().length &&
      selection.querySelector('[data-markdown-import-image-count]').textContent === '3 images ready' &&
      !source.getClientRects().length && !folder.getClientRects().length,
      'A complete folder should show one selection summary without any file chooser fields.');

    const fetchOriginal = window.fetch;
    const uploadNames = [];
    window.fetch = (url, options) => {
      if (url === '/post/images/upload') {
        uploadNames.push(options.body.get('image').name);
        if (uploadNames.length === 2) {
          return Promise.resolve(new Response(JSON.stringify({error: 'Temporary test failure.'}), {
            status: 503, headers: {'Content-Type': 'application/json'}
          }));
        }
        if (uploadNames.length === 3) {
          return Promise.resolve(new Response(null, {status: 401}));
        }
      }
      return fetchOriginal(url, options);
    };
    const confirmation = () => document.querySelector('.editor-confirmation[open]');
    const assertUnchanged = () => assert(form.elements.content.value === beforeBody &&
      form.elements.componentOrder.value === beforeOrder && form.elements.published.value === beforePublication &&
      components.every((source, index) => !source.disabled && source.value === beforeComponents[index]),
      'Canceled or failed import must preserve the writing, components, and publication choice.');
    openDraft.focus();
    openDraft.click();
    if (mobile) {
      const prompt = confirmation();
      assert(prompt?.querySelector('h2').textContent === 'Replace unsaved changes?' &&
        ['post content', 'components', 'publication setting'].every(label => prompt.textContent.includes(label)),
        'The overwrite confirmation must explain what will be replaced.');
      assert(uploadNames.length === 0 && document.activeElement === prompt.querySelector('[data-confirm-cancel]'),
        'Import must wait for confirmation before uploading and focus Cancel first.');
      prompt.querySelector('[data-confirm-cancel]').click();
      await Promise.resolve();
      assertUnchanged();
      assert(dialog.open && document.activeElement === openDraft && !openDraft.disabled && uploadNames.length === 0,
        'Declining replacement must keep the files selected and return focus to Open draft.');
      openDraft.click();
      confirmation().querySelector('[data-confirm-accept]').click();
    } else {
      assert(!confirmation(), 'A recovered summary alone must not trigger a warning because import keeps it.');
    }
    await expectPickerError('bundle', 'Temporary test failure.');
    assert(dialog.open && !pickFolder.disabled && !openDraft.disabled && openDraft.textContent === 'Retry import',
      'A temporary upload failure must allow a new selection or an immediate retry.');
    assertUnchanged();
    openDraft.click();
    assert(!confirmation(), 'Retrying an approved import must not ask again for the same changes.');
    await expectPickerError('bundle', "You're signed out.");
    assert(status.querySelector('a[href="/login"][target="_blank"]') &&
      !status.querySelector('[data-upload-retry]') && openDraft.textContent === 'Retry import',
      'Signing in again must preserve the source and show one retry action.');
    assertUnchanged();
    openDraft.click();
    await waitForComponent(() => !dialog.open && form.elements.content.value.includes('Café 🌱'), 'imported editor');
    window.fetch = fetchOriginal;
    assert(uploadNames.join(',') === 'tall view.png,compare.png,compare.png,compare.png,compare.png', 'Upload must follow the referenced image order and reuse a successful upload on retry.');
    assert(form.elements.title.value === 'Imported Markdown' && form.elements.published.value === '' &&
      form.elements.summary.value === recoveredSummary, 'Import should populate a new draft and preserve the recovered summary.');
    assert(visible(opener) && form.elements.content.value.includes('[Plain link][figure]'),
      'Import must preserve ordinary Markdown and remain available after importing.');
    assert(form.elements.componentOrder.value === '[]' &&
      components.every(source => !source.value && source.closest('[data-post-component]').hidden),
      'Replacing the body must also clear the old component and its settings.');

    form.requestSubmit();
  } catch (error) { await report(error.message + '\n' + (error.stack || '')); }
})();
