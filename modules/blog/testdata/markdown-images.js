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

    const limited = article.querySelector('img[alt="Fit to screen"]');
    await waitForComponent(() => limited?.naturalWidth > 0, 'height-limited image');
    const expectedHeight = Math.max(240, Math.min(innerHeight * .65, 560));
    const limitedBox = limited.getBoundingClientRect();
    assert(Math.abs(limitedBox.height - expectedHeight) < 1 &&
      Math.abs(limitedBox.width / limitedBox.height - limited.naturalWidth / limited.naturalHeight) < .01,
      `Fit to screen should preserve proportions and cap the image at ${expectedHeight}px in a ${innerHeight}px viewport.`);

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

    const portrait = await open(openerFor('Fit to screen'));
    const portraitBox = portrait.image.getBoundingClientRect();
    const viewerBox = portrait.viewer.getBoundingClientRect();
    const portraitClose = portrait.viewer.querySelector('[data-album-close]');
    const top = portraitBox.top - viewerBox.top;
    const bottom = viewerBox.bottom - portraitBox.bottom;
    const compact = innerHeight <= 480;
    assert(Math.abs(top - (compact ? 8 : 12)) < 1 && Math.abs(bottom - top) < 1,
      'The fullscreen portrait must have small, equal margins.');
    const controls = portrait.viewer.querySelectorAll('[data-album-zoom], [data-album-close]');
    for (const control of controls) {
      const box = control.getBoundingClientRect();
      const hit = document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2);
      assert(Math.abs(viewerBox.bottom - box.bottom - (compact ? 4 : 10)) < 1 && box.width >= 44 && box.height >= 44 && control.contains(hit),
        'The fullscreen controls must stay reachable in their original position.');
    }
    if (innerWidth === 1100 && !compact) {
      const tools = portraitClose.parentElement;
      const move = pointerType => portrait.image.dispatchEvent(new PointerEvent('pointermove', {bubbles: true, pointerType}));
      const visible = () => getComputedStyle(tools).opacity === '1';
      portrait.image.parentElement.focus();
      move('mouse');
      await waitForComponent(() => getComputedStyle(tools).opacity === '0', 'idle mouse controls to fade');
      assert(getComputedStyle(portraitClose).pointerEvents === 'none', 'Faded controls must not intercept clicks.');
      move('mouse');
      await waitForComponent(visible, 'mouse movement to reveal controls');
      await waitForComponent(() => getComputedStyle(tools).opacity === '0', 'mouse controls to fade again');
      portraitClose.focus();
      await waitForComponent(visible, 'keyboard focus to reveal controls');
      portrait.viewer.dispatchEvent(new KeyboardEvent('keydown', {key: 'Tab', bubbles: true}));
      assert(!tools.hasAttribute('data-idle'), 'Keyboard input must cancel the idle state.');
      portrait.image.parentElement.focus();
      move('mouse');
      move('touch');
      await new Promise(resolve => setTimeout(resolve, 3100));
      assert(visible() && !tools.hasAttribute('data-idle'), 'Touch controls must stay visible.');
    }
    portraitClose.click();
    await waitForComponent(() => !portrait.viewer.isConnected, 'close fullscreen portrait');

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
