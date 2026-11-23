(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const article = document.querySelector('#article-content');
  const headingButtons = () => article.querySelectorAll('button[data-reading-copy="heading"]');
  let clipboardText;
  let blocked = false;
  try {
    // The fixture uses scripted clicks, which do not grant clipboard permission.
    navigator.clipboard.writeText = async text => {
      if (blocked) throw new DOMException('Clipboard blocked', 'NotAllowedError');
      clipboardText = text;
    };
    const status = document.querySelector('.reading-copy-status');
    const codeButtons = article.querySelectorAll('button[data-reading-copy="code"]');
    assert(codeButtons.length === 2, 'Both code blocks should have a copy button.');
    for (const button of codeButtons) {
      const pre = button.previousElementSibling;
      const style = getComputedStyle(pre);
      const codeBounds = pre.getBoundingClientRect();
      const firstLineCenter = codeBounds.top + parseFloat(style.paddingTop) + parseFloat(style.lineHeight) / 2;
      const bounds = button.getBoundingClientRect();
      assert(Math.abs(bounds.top + bounds.height / 2 - firstLineCenter) < 1, 'Copy must align with the first line of code.');
      assert(Math.abs(codeBounds.width - pre.parentElement.getBoundingClientRect().width) < 1, 'The scroll area should span the full block.');
      const cover = getComputedStyle(pre.parentElement, '::after');
      assert(cover.pointerEvents === 'none' && Math.abs(parseFloat(cover.height) - parseFloat(style.lineHeight)) < 1,
        'The copy control should cover only the first line at the edge and leave the scroll area interactive.');
      assert(button.querySelector('svg[aria-hidden="true"]') && button.getAttribute('aria-label') === 'Copy code' && !button.textContent.trim(),
        'Copy should use an icon and retain its accessible name.');
    }
    const longBlockButton = codeButtons[1];
    const longBlock = longBlockButton.previousElementSibling;
    assert(longBlock.scrollWidth > longBlock.clientWidth, 'The long command should scroll.');
    const buttonLeft = longBlockButton.getBoundingClientRect().left;
    longBlock.scrollLeft = longBlock.scrollWidth;
    const codeText = longBlock.querySelector('code').firstChild;
    const end = codeText.textContent.trimEnd().length;
    const tail = document.createRange();
    tail.setStart(codeText, end - 1);
    tail.setEnd(codeText, end);
    assert(longBlockButton.getBoundingClientRect().left === buttonLeft && tail.getBoundingClientRect().right <= buttonLeft,
      'The icon should stay in place while the end of the command scrolls clear of it.');
    longBlock.scrollLeft = 0;
    const codeButton = codeButtons[0];
    const block = codeButton.parentElement;
    const width = block.firstElementChild.getBoundingClientRect().width;
    codeButton.click();
    await waitForComponent(() => status.textContent === 'Code copied.', 'copy feedback');
    assert(clipboardText === 'if ready {\n\tstart()\n}\n', 'Copy should preserve code indentation and line breaks.');
    assert(block.firstElementChild.getBoundingClientRect().width === width, 'Copy feedback should not resize the code block.');

    const headings = headingButtons();
    assert(headings.length === 2, 'Each section heading should have one copy button.');
    headings[1].click();
    await waitForComponent(() => status.textContent === 'Heading link copied.', 'heading link feedback');
    assert(clipboardText === location.origin + '/post/s/reading-tools#section-caf%C3%A9-setup-2',
      'Copy the actual post link from the home page, with the matching Unicode section ID.');

    blocked = true;
    codeButton.click();
    await waitForComponent(() => status.textContent === 'Could not copy the code.', 'clipboard error feedback');
    assert(codeButton.textContent === '!' && codeButton.title === status.textContent && !codeButton.disabled,
      'Clipboard permission errors should be visible and let readers try again.');
    blocked = false;

    const pageLink = document.querySelector('.site-links a[href="/page/reading-tools"]');
    assert(pageLink, 'The seeded page should be linked in site navigation.');
    pageLink.click();
    await waitForComponent(() => location.pathname === '/page/reading-tools' &&
      article.querySelector('[data-reading-url="/page/reading-tools"] button[data-reading-copy="heading"]'), 'page navigation');
    assert(status.textContent === '', 'Copy feedback should clear when navigating away.');
    assert(headingButtons().length === 2, 'The heading buttons should initialize once after HTMX navigation.');
    headingButtons()[0].click();
    await waitForComponent(() => status.textContent === 'Heading link copied.', 'page heading link feedback');
    assert(clipboardText === location.origin + '/page/reading-tools#section-caf%C3%A9-setup',
      'The heading link should point to the new page.');

    history.back();
    await waitForComponent(() => location.pathname === '/' &&
      article.querySelector('[data-reading-url="/post/s/reading-tools"] button[data-reading-copy="heading"]'), 'home history restore');
    assert(headingButtons().length === 2, 'History restore should keep exactly one button per heading.');
    clipboardText = '';
    headingButtons()[0].click();
    await waitForComponent(() => clipboardText.endsWith('/post/s/reading-tools#section-caf%C3%A9-setup'), 'copy after history restore');
    await report('PASS');
  } catch (error) {
    await report(error.stack || String(error));
  }
})();
