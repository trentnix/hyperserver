(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const wait = async predicate => {
    const end = Date.now() + 12000;
    while (!predicate()) { if (Date.now() > end) throw new Error('Timed out: ' + predicate); await new Promise(resolve => setTimeout(resolve, 25)); }
  };
  const set = (field, value) => { field.value = value; field.dispatchEvent(new Event('input', {bubbles: true})); };
  const caption = 'A comparison with <markup> and a caption long enough to wrap on a narrow phone.\nA second line of context.';
  const beforeLabel = 'Original <image> & source';
  const afterLabel = 'Revised image with a long label that stays on one line in a narrow image frame';
  try {
    const phase = sessionStorage.getItem('comparison-phase');
    if (!phase) {
      await wait(() => document.querySelector('[data-comparison-editor] [name="comparison"]').hidden && document.querySelector('#post-form .CodeMirror'));
      const form = document.querySelector('#post-form');
      const section = form.querySelector('[data-comparison-editor]');
      assert(section.hidden && section.querySelector('[data-comparison-controls]').disabled, 'An unused comparison must not block Save.');
      form.elements.title.value = 'Comparison review'; form.elements.slug.value = 'comparison-review';
      form.querySelector('[data-component-picker] summary').click();
      await addTestComponent(form.querySelector('[data-component-add="image-comparison"]'));
      assert(!section.hidden && document.activeElement.id === 'comparison-title', 'Adding a comparison must focus its first field.');
      assert(section.querySelector('#comparison-before-label').placeholder === 'Before' && section.querySelector('#comparison-after-label').placeholder === 'After', 'Label fields must show the defaults.');
      const choose = async (side, index) => {
        section.querySelector(`[data-comparison-side="${side}"] [data-media-browse]`).click();
        await wait(() => document.querySelectorAll('.media-browser [data-media-select]').length === 2);
        document.querySelectorAll('.media-browser [data-media-select]')[index].click();
        await wait(() => !document.querySelector('.media-browser'));
      };
      await choose('before', 0);
      await choose('after', 1);
      const canvas = document.createElement('canvas');
      canvas.width = 32; canvas.height = 24;
      canvas.getContext('2d').fillRect(0, 0, 16, 24);
      const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
      const transfer = new DataTransfer(); transfer.items.add(new File([blob], 'After.png', {type: 'image/png'}));
      const upload = section.querySelector('[data-comparison-side="after"] input[type="file"]');
      upload.files = transfer.files; upload.dispatchEvent(new Event('change', {bubbles: true}));
      assert(form.dataset.uploading && form.querySelector('[type="submit"]').disabled, 'Save must wait for image uploads.');
      await wait(() => !form.dataset.uploading && section.querySelector('[data-comparison-side="after"] [data-image-status]').textContent.includes('uploaded'));
      const selected = JSON.parse(form.elements.comparison.value);
      assert(selected.beforeID && selected.afterID && selected.beforeID !== selected.afterID, 'The two library choices must remain distinct.');
      set(section.querySelector('#comparison-before-alt'), 'The original image');
      set(section.querySelector('#comparison-after-alt'), 'The edited image');
      set(section.querySelector('#comparison-before-label'), beforeLabel);
      set(section.querySelector('#comparison-after-label'), afterLabel);
      set(section.querySelector('#comparison-title'), 'Before <markup> & after');
      set(section.querySelector('#comparison-caption'), caption);
      await closeComponentSettings(form);
      form.querySelector('.tool-preview').click();
      await wait(() => form.querySelector('.editor-preview .comparison-ready'));
      const preview = form.querySelector('.editor-preview .comparison-ready');
      set(preview.querySelector('input[type="range"]'), '25');
      assert(preview.querySelector('.comparison-stage').style.getPropertyValue('--comparison-position') === '25%', 'The comparison must be interactive in the post preview.');
      form.querySelector('.tool-preview').click();
      const saved = form.elements.comparison.value;
      await removeTestComponent(section.querySelector('[data-component-remove]'));
      assert(section.hidden && !form.elements.comparison.value, 'Remove must clear comparison source.');
      await undoTestComponent(form.querySelector('[data-component-undo="image-comparison"]'));
      assert(form.elements.comparison.value === saved && section.querySelector('#comparison-caption').value === caption, 'Undo must restore both images and their text.');
      form.querySelector('[data-component-picker] summary').click();
      setTestComponentSource(await addTestComponent(form.querySelector('[data-component-add="video"]')), {url: 'https://youtu.be/M7lc1UVf-VE'});
      await moveTestComponent(section, 'down');
      assert(form.elements.componentOrder.value === '["video","image-comparison"]', 'Comparisons must use shared component ordering.');
      await removeTestComponent(form.querySelector('[data-video-editor] [data-component-remove]'));
      assert(!form.elements.content.required, 'A comparison-only post must be allowed.');
      form.querySelector('[name="published"][value="on"]').checked = true;
      form.dispatchEvent(new Event('input', {bubbles: true}));
      await wait(() => Object.keys(localStorage).some(key => key.startsWith('hyperserver.blog.draft.v1:') && localStorage.getItem(key).includes('original image')));
      sessionStorage.setItem('comparison-phase', 'recover'); location.reload(); return;
    }
    if (phase === 'recover') {
      await wait(() => document.querySelector('[data-draft-recovery]') && !document.querySelector('[data-draft-recovery]').hidden);
      document.querySelector('[data-draft-restore]').click();
      const form = document.querySelector('#post-form');
      assert(form.querySelector('#comparison-caption').value === caption && form.querySelector('#comparison-before-label').value === beforeLabel && form.querySelector('#comparison-after-label').value === afterLabel && JSON.parse(form.elements.comparison.value).afterID && form.elements.componentOrder.value === '["image-comparison"]', 'Draft recovery must preserve images, text, labels, and order.');
      sessionStorage.setItem('comparison-phase', 'reader'); form.requestSubmit(); return;
    }
    await wait(() => document.querySelector('.post-body .comparison-ready'));
    const comparison = document.querySelector('.post-body .image-comparison');
    const slider = comparison.querySelector('input[type="range"]');
    const stage = comparison.querySelector('.comparison-stage');
    const handle = comparison.querySelector('.comparison-handle');
    comparison.scrollIntoView({block: 'center'});
    await wait(() => [...comparison.querySelectorAll('img')].every(image => image.complete && image.naturalWidth > 0));
    assert([...comparison.querySelectorAll('img')].every(image => !image.draggable), 'Native image dragging must not cancel the comparison gesture.');
    assert(comparison.querySelector('h3').textContent === 'Before <markup> & after' && comparison.querySelector('figcaption').textContent === caption && !comparison.querySelector('markup'), 'Authored text must be safely rendered.');
    const before = comparison.querySelector('.comparison-before');
    const after = comparison.querySelector('.comparison-after');
    assert(before.querySelector('.comparison-label').textContent === beforeLabel && after.querySelector('.comparison-label').textContent === afterLabel && !comparison.querySelector('image'), 'Custom labels must render as text.');
    const label = after.querySelector('.comparison-label');
    assert(getComputedStyle(label).whiteSpace === 'nowrap' && getComputedStyle(label).textOverflow === 'ellipsis' && label.getBoundingClientRect().width <= after.getBoundingClientRect().width && label.title === afterLabel, 'Long labels must stay inside the image frame with the full label available.');
    assert(before.getBoundingClientRect().width === after.getBoundingClientRect().width && before.getBoundingClientRect().height === after.getBoundingClientRect().height, 'Both images must share the same frame.');
    for (const value of ['0', '25', '100']) {
      set(slider, value);
      assert(stage.style.getPropertyValue('--comparison-position') === value + '%' && slider.getAttribute('aria-valuetext') === `${value}% ${beforeLabel}, ${100 - Number(value)}% ${afterLabel}`, 'The slider must reveal the requested amount and use custom labels accessibly.');
      const box = handle.getBoundingClientRect();
      const frame = stage.getBoundingClientRect();
      assert(box.left >= frame.left && box.right <= frame.right, 'The handle and its focus ring must stay inside the frame at either endpoint.');
    }
    slider.focus();
    assert(document.activeElement === slider && stage.contains(slider) && !comparison.querySelector('.comparison-slider') && !comparison.textContent.includes('Drag to compare'), 'The image handle must be the only comparison control and remain keyboard focusable.');
    assert(getComputedStyle(before).clipPath.includes('0%'), 'The last slider position must fully reveal Before.');
    assert(getComputedStyle(stage).touchAction === 'pan-y', 'Touch readers must still be able to scroll vertically.');
    assert(document.documentElement.scrollWidth <= innerWidth + 1 && handle.getBoundingClientRect().height >= 44 && handle.getBoundingClientRect().width >= 44, 'The comparison and touch controls must fit the viewport.');
    comparison.classList.remove('comparison-ready');
    assert(getComputedStyle(stage).display === 'grid' && getComputedStyle(before).position === 'relative', 'Without enhancement, both labeled images must remain visible side by side.');
    comparison.classList.add('comparison-ready');
    await htmx.ajax('GET', document.querySelector('a[href^="/post/update/"]').getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
    await wait(() => document.querySelector('#comparison-caption'));
    assert(document.querySelector('#comparison-caption').value === caption && document.querySelector('#comparison-before-label').value === beforeLabel && document.querySelector('#comparison-after-label').value === afterLabel && !document.querySelector('[data-comparison-editor] [data-component-unsaved]:not([hidden])'), 'Reopening must preserve comparison content and labels with a clean saved indicator.');
    const editor = document.querySelector('[data-comparison-editor]');
    editor.querySelector('[data-image-reset]').click();
    assert(!JSON.parse(document.querySelector('[name="comparison"]').value).beforeID && !editor.querySelector('.image-empty').hidden, 'Clearing an image must preserve the rest of the comparison.');
    sessionStorage.removeItem('comparison-phase');
    await report('PASS');
  } catch (error) { await report(String(error) + '\n' + (error.stack || '')); }
})();
