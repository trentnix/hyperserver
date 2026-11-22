(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const marker = '{size=custom max-width=110 max-height=360}';
  const imageMarkdown = '![Tall image](/test/image.svg?size=tall)';
  const introduction = 'Café 🌱\n\n';
  const initial = introduction + imageMarkdown + '\n\n' + imageMarkdown;
  const saved = introduction + imageMarkdown + '\n\n' + marker + imageMarkdown;
  const checkBounds = (image, message) => {
    const bounds = image.getBoundingClientRect();
    assert(bounds.width <= 111 && bounds.height <= 361 &&
      Math.abs(bounds.width / bounds.height - image.naturalWidth / image.naturalHeight) < .01, message);
    return bounds;
  };
  try {
    if (location.pathname === '/post/s/markdown-image-size') {
      const [full, limited] = document.querySelectorAll('.post-body img[data-markdown-image]');
      await waitForComponent(() => full?.naturalWidth && limited?.naturalWidth, 'published images');
      assert(!full.dataset.markdownImageSize && limited.dataset.markdownImageSize === 'custom' &&
        full.getBoundingClientRect().height > 360, 'Only the selected image should have custom limits after saving.');
      const bounds = checkBounds(limited, 'The published image must preserve its proportions and respect the custom limits.');
      const link = limited.closest('[data-view-image]');
      const hint = link.querySelector('.markdown-image-hint').getBoundingClientRect();
      assert(hint.left >= bounds.left && hint.right <= bounds.right, 'The enlarge control must sit on the limited image.');
      link.click();
      const enlarged = document.querySelector('.album-viewer:modal [data-album-image]');
      await waitForComponent(() => enlarged?.naturalHeight === limited.naturalHeight && !enlarged.hidden, 'full-size image');
      assert(getComputedStyle(enlarged).maxHeight !== '360px', 'The full-screen viewer must have its own sizing.');
      await report('PASS');
      return;
    }

    const form = document.querySelector('#post-form');
    await waitForComponent(() => form?.querySelector('.CodeMirror'), 'post editor');
    form.elements.title.value = 'Markdown image size';
    form.elements.slug.value = 'markdown-image-size';
    const source = form.querySelector('.CodeMirror').CodeMirror;
    source.setValue(initial);
    source.setCursor({line: 0, ch: 0});
    const buttons = () => form.querySelectorAll('.CodeMirror .markdown-image-size-button');
    await waitForComponent(() => buttons().length === 2, 'inline Image size buttons');
    const [first, second] = buttons();
    await waitForComponent(() => second.querySelector('img')?.naturalWidth, 'clickable image thumbnail');
    assert(second.title === 'Image size · Automatic', 'The thumbnail should name the current preset.');
    assert(first.getBoundingClientRect().top < second.getBoundingClientRect().top, 'Each image must get a button on its Markdown line.');
    const preview = form.querySelector('.tool-preview');
    const openSize = async (button = second) => {
      button.click();
      await waitForComponent(() => document.querySelector('.markdown-image-dialog:modal'), 'Image size dialog');
      const dialog = document.querySelector('.markdown-image-dialog:modal');
      assert(!dialog.querySelector('select'), 'The inline button must open the matching settings without an image picker.');
      return dialog;
    };
    let dialog = await openSize();
    const choices = [...dialog.querySelectorAll('.markdown-image-presets > label')];
    const responsive = choices.find(label => label.querySelector('input').value === 'limit-height');
    assert(choices.length === 5 && choices.every(label => label.getBoundingClientRect().height >= 44 &&
      Math.abs(label.getBoundingClientRect().width - choices[0].getBoundingClientRect().width) < 1) &&
      responsive.textContent.includes('240–560 px height cap'), 'Presets must have full-width touch targets and explain their limits.');
    dialog.querySelector('input[value="compact"]').click();
    dialog.querySelector('[data-image-size-cancel]').click();
    await waitForComponent(() => !dialog.isConnected, 'dialog to close');
    assert(source.getValue() === initial && document.activeElement === second, 'Cancel must leave the source intact and restore focus.');

    // Click an existing button before its Markdown offsets have been refreshed.
    source.replaceRange('\n', {line: 0, ch: 0});
    dialog = await openSize();
    assert(dialog.querySelector('[data-image-size-name]').textContent === 'Tall image', 'The button must follow its image after nearby edits.');
    dialog.close();
    await waitForComponent(() => !dialog.isConnected, 'shifted dialog to close');
    source.replaceRange('', {line: 0, ch: 0}, {line: 1, ch: 0});

    dialog = await openSize();
    const settings = dialog.querySelector('form');
    const {width, height} = settings.elements;
    const apply = dialog.querySelector('[type="submit"]');
    dialog.querySelector('input[value="custom"]').click();
    apply.click();
    assert(dialog.open && !width.validity.valid, 'Custom sizing needs at least one limit.');
    width.value = '110';
    width.dispatchEvent(new Event('input', {bubbles: true}));
    height.value = '360';
    apply.click();
    await waitForComponent(() => !dialog.isConnected, 'applied image size');
    assert(source.getValue() === saved, 'Apply must edit only the selected occurrence and handle multibyte text.');
    assert(buttons()[1] === second && document.activeElement === second, 'Folding the size must keep the thumbnail and restore focus to it.');
    await waitForComponent(() => second.title === 'Image size · Max width 110 px, Max height 360 px', 'custom size on the thumbnail');
    const visibleSource = form.querySelector('.CodeMirror-code').textContent;
    assert(visibleSource.includes('![Tall image]') && !visibleSource.includes(marker),
      'The source must show the image Markdown and fold its size marker into the thumbnail.');

    preview.click();
    await waitForComponent(() => form.querySelector('.editor-preview-active img[data-markdown-image-size="custom"]')?.naturalWidth, 'read-only preview');
    assert(preview.textContent === 'Edit Markdown' && preview.tabIndex === 0 && getComputedStyle(preview).pointerEvents !== 'none' &&
      [...buttons()].every(button => getComputedStyle(button).visibility === 'hidden'),
      'Preview must offer a visible, clickable and keyboard-reachable way back to the editor.');
    assert(source.getOption('readOnly') === 'nocursor' && source.getInputField().inert,
      'Preview must take the hidden source editor out of the tab order and prevent typing in it.');
    const image = form.querySelector('.editor-preview-active img[data-markdown-image-size="custom"]');
    assert(!image.closest('.editor-preview').querySelector('input, select'), 'Preview must stay read-only.');
    checkBounds(image, 'Preview must render the custom dimensions at the correct proportions.');
    preview.click();
    assert(!form.querySelector('.editor-preview-active') && preview.textContent === 'Preview' && source.getValue() === saved &&
      !source.getOption('readOnly') && !source.getInputField().inert,
      'Edit Markdown must close Preview and preserve content.');
    buttons()[1].click();
    await waitForComponent(() => document.querySelector('.markdown-image-dialog:modal'), 'reopening Image size');
    dialog = document.querySelector('.markdown-image-dialog:modal');
    assert(dialog.querySelector('input[name="preset"]:checked').value === 'custom' && dialog.querySelector('input[name="height"]').value === '360',
      'Reopening image size must restore the selected image and its limits.');
    dialog.querySelector('input[name="preset"][value=""]').click();
    dialog.querySelector('[type="submit"]').click();
    await waitForComponent(() => !dialog.isConnected, 'reset image size');
    assert(source.getValue() === initial && buttons().length === 2, 'Automatic must remove the size marker and keep the thumbnail.');
    source.undo();
    await waitForComponent(() => source.getValue() === saved && buttons()[1]?.hasAttribute('data-image-sized') &&
      !form.querySelector('.CodeMirror-code').textContent.includes(marker), 'undo to restore the folded size');
    const start = source.posFromIndex(saved.lastIndexOf('!['));
    source.replaceRange('Before the photo ', start, start, '+input');
    assert(source.getValue() === saved.replace(marker, 'Before the photo ' + marker),
      'Typing before the image syntax must keep its folded size marker with the image.');
    source.undo();
    await waitForComponent(() => buttons()[1]?.hasAttribute('data-image-sized') &&
      !form.querySelector('.CodeMirror-code').textContent.includes(marker), 'folded size after undo');
    source.replaceRange('', start, source.posFromIndex(saved.length), '+delete');
    assert(source.getValue() === introduction + imageMarkdown + '\n\n', 'Deleting the photo must also remove its folded size marker.');
    source.undo();
    assert(source.getValue() === saved, 'Undo must recover the photo and size marker together.');
    if (innerWidth === 1100) {
      source.replaceRange('\n\n![Relative](diagram.png)', source.posFromIndex(saved.length));
      await waitForComponent(() => buttons().length === 3, 'relative image settings');
      assert(!buttons()[2].querySelector('img').hasAttribute('src'), 'Relative URLs must not load a thumbnail from the editor URL.');
      const fetchOriginal = window.fetch;
      window.fetch = (path, options) => path === '/post/preview' && options.body.has('imageSettings') ?
        Promise.resolve(new Response('', {status: 503})) : fetchOriginal(path, options);
      source.undo();
      dialog = await openSize(buttons()[1]);
      window.fetch = fetchOriginal;
      assert(!dialog.querySelector('[data-image-size-status]').hidden && dialog.querySelector('[type="submit"]').disabled,
        'An unavailable settings lookup must explain the failure and prevent a stale edit.');
      dialog.close();
    }
    form.querySelector('[name="published"][value="on"]').checked = true;
    form.requestSubmit();
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
