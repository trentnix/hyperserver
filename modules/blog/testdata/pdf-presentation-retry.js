(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const wait = async predicate => {
    const end = Date.now() + 12000;
    while (!predicate()) { if (Date.now() > end) throw new Error('Timed out: ' + predicate); await new Promise(resolve => setTimeout(resolve, 25)); }
  };
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  window.addEventListener('unhandledrejection', event => report('Unhandled rejection: ' + event.reason));
  document.addEventListener('securitypolicyviolation', event => report('CSP violation: ' + event.violatedDirective + ' ' + event.blockedURI));
  try {
    const viewer = document.querySelector('[data-pdf-presentation]');
    viewer.scrollIntoView({block: 'center'});
    await wait(() => viewer.querySelector('[data-pdf-fallback]'));
    assert(viewer.querySelector('[data-pdf-slide]').hidden && !viewer.querySelector('[data-pdf-retry]').hidden, 'A failed presentation must offer the native viewer and Retry.');
    assert(viewer.querySelector('[data-pdf-status]').textContent.includes('Unable to load') && document.querySelector('.pdf-preview-heading a').textContent === 'Open PDF', 'Failure must explain the available alternatives.');
    assert(document.documentElement.scrollWidth <= innerWidth + 1, 'Recovery controls must fit a narrow viewport.');
    viewer.querySelector('[data-pdf-retry]').click();
    await wait(() => viewer.querySelector('[data-pdf-page]').textContent === 'Page 1 of 2');
    assert(!viewer.querySelector('[data-pdf-fallback]') && viewer.querySelector('[data-pdf-retry]').hidden && !viewer.querySelector('[data-pdf-status]').textContent, 'A successful retry must remove the fallback and error message.');
    const canvas = viewer.querySelector('canvas');
    const stage = viewer.querySelector('[data-pdf-slide]');
    const text = viewer.querySelector('[data-pdf-text]');
    const probe = window.pdfProbe;
    assert(canvas.clientWidth >= 0.9 * stage.clientWidth, 'A portrait page should fill the narrow stage.');
    assert(probe.releaseText && !text.textContent, 'The canvas should appear before text extraction completes.');
    probe.holdText = false;
    probe.releaseText();
    await wait(() => text.textContent.trim());
    viewer.style.width = '90%';
    await wait(() => viewer.querySelector('canvas') !== canvas);
    assert(probe.textReads.length === 1, 'Resizing a page should reuse its text.');

    const resizedCanvas = viewer.querySelector('canvas');
    window.dispatchEvent(new PageTransitionEvent('pagehide'));
    assert(resizedCanvas.width === 0 && stage.hidden, 'Leaving the page must release presentation resources.');
    window.dispatchEvent(new PageTransitionEvent('pageshow', {persisted: true}));
    await wait(() => viewer.querySelector('canvas').width > 0 && !viewer.querySelector('[data-pdf-slide]').hidden);
    viewer.querySelector('[data-pdf-next]').click();
    await wait(() => viewer.querySelector('[data-pdf-page]').textContent === 'Page 2 of 2');
    assert(probe.cleanedPages.includes(1), 'Leaving a page should release its rendering resources.');

    const fullscreen = viewer.querySelector('[data-pdf-fullscreen]');
    fullscreen.click();
    await wait(() => document.fullscreenElement === viewer && fullscreen.getAttribute('aria-label') === 'Exit fullscreen');
    probe.failPage = 1;
    viewer.querySelector('[data-pdf-previous]').click();
    await wait(() => viewer.querySelector('[data-pdf-fallback]'));
    assert(!document.fullscreenElement && fullscreen.hidden && viewer.querySelector('[data-pdf-retry]').getBoundingClientRect().width > 0, 'A fullscreen page failure should return to the inline recovery controls.');
    viewer.focus();
    const key = new KeyboardEvent('keydown', {key: 'PageDown', bubbles: true, cancelable: true});
    viewer.dispatchEvent(key);
    assert(!key.defaultPrevented, 'The failed presentation must allow normal keyboard scrolling.');
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
