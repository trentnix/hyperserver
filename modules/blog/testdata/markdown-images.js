(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const article = document.querySelector('#article-content');
  const openerFor = description => article.querySelector(`img[alt="${description}"]`)?.closest('a');
  const open = async opener => {
    opener.focus();
    opener.click();
    const viewer = document.querySelector('.album-viewer:modal');
    const image = viewer?.querySelector('[data-album-image]');
    await waitForComponent(() => image?.naturalWidth > 0 && !image.hidden, 'enlarged image');
    assert(image.src === opener.href && getComputedStyle(image).objectFit === 'contain', 'The viewer must show the uncropped Markdown image.');
    return {viewer, image};
  };
  try {
    const wide = openerFor('Wide diagram');
    const linkedImage = openerFor('Linked image');
    assert(wide?.hasAttribute('data-view-image') && wide.getAttribute('aria-label') === 'Enlarge image: Wide diagram', 'The image must have a named viewer link.');
    assert(linkedImage.getAttribute('href') === '/page/image-review' && !linkedImage.hasAttribute('data-view-image'), 'An authored image link must keep its destination.');
    assert(!openerFor('Invalid image'), 'An unsafe image URL must stay out of the viewer.');
    assert(openerFor('Callout screenshot')?.hasAttribute('data-view-image'), 'Markdown images in components must be expandable.');

    const inline = wide.querySelector('img');
    await waitForComponent(() => inline.naturalWidth > 0, 'inline image');
    const inlineBox = inline.getBoundingClientRect();
    const linkBox = wide.getBoundingClientRect();
    const badge = wide.querySelector('.markdown-image-hint').getBoundingClientRect();
    assert(Math.abs(inlineBox.width - linkBox.width) < 1 && badge.left >= inlineBox.left && badge.right <= inlineBox.right &&
      document.documentElement.scrollWidth <= innerWidth + 1, 'The image and enlarge hint must fit the content without horizontal overflow.');
    if (innerWidth === 320) assert(Math.abs(inlineBox.width - innerWidth) < 1, 'Wide images must still use the phone gutters.');

    const {viewer, image} = await open(wide);
    const collapse = viewer.querySelector('[data-album-close]');
    assert(image.alt === inline.alt && collapse === document.activeElement && collapse.getAttribute('aria-label') === 'Collapse image', 'The viewer must preserve alt text and focus the collapse control.');
    const point = image.getBoundingClientRect();
    for (const type of ['pointerdown', 'pointerup']) {
      image.dispatchEvent(new PointerEvent(type, {
        bubbles: true, pointerType: 'touch', pointerId: 1, button: 0, clientX: point.x + point.width / 2, clientY: point.y + point.height / 2
      }));
    }
    image.click();
    await waitForComponent(() => !viewer.isConnected, 'tap to collapse');
    assert(document.activeElement === wide, 'Closing the viewer must restore focus to the image.');

    await htmx.ajax('GET', '/page/image-review', {target: '#article-content', swap: 'innerHTML'});
    await waitForComponent(() => openerFor('')?.hasAttribute('data-view-image'), 'page image after HTMX navigation');
    const small = openerFor('');
    assert(small.getAttribute('aria-label') === 'Enlarge image', 'An image without a description must still have a named control.');
    const page = await open(small);
    page.viewer.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true}));
    await waitForComponent(() => !page.viewer.isConnected, 'Escape to collapse');
    assert(document.activeElement === small, 'Escape must restore focus to the page image.');
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
