// Short landscape windows must preserve browsing space during error recovery.
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
  try {
    if (!sessionStorage.getItem('landscape-editor')) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('landscape-editor', '1');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    await htmx.ajax('GET', '/post/create', {target: '#article-content', swap: 'innerHTML'});
    await wait(() => document.querySelector('[data-album-browse]') && !document.querySelector('.album-editor-controls').hidden);
    await document.fonts.ready;
    const form = document.querySelector('#post-form');
    form.elements.title.value = 'Unsaved landscape post';
    const browserFetch = window.fetch;
    let failBrowse = false;
    window.fetch = (input, options) => {
      if (failBrowse && new URL(input.url || input, location.href).pathname === '/media') {
        return Promise.resolve(new Response('Temporary service failure', {status: 503, headers: {'Content-Type': 'text/plain'}}));
      }
      return browserFetch(input, options);
    };
    const {openMediaLibrary} = await import('/assets/js/media.js');
    for (const multiple of [false, true, 'full']) {
      const opener = form.querySelector(multiple ? '[data-album-browse]' : '[data-media-browse]');
      opener.focus();
      if (multiple === 'full') openMediaLibrary(() => {}, {multiple: true, limit: 1});
      else opener.click();
      const dialog = document.querySelector('.media-browser');
      const results = dialog.querySelector('[data-media-results]');
      await wait(() => results.querySelector('[data-media-select]') && !results.hasAttribute('aria-busy'));
      const filters = results.querySelector('[data-media-filters]');
      const search = filters.elements.q.getBoundingClientRect();
      const usage = filters.elements.usage.getBoundingClientRect();
      assert(search.top === usage.top, 'Landscape search and usage filters must share a row.');
      if (multiple) results.querySelector('[data-media-select]').click();
      function checkLayout() {
        const bounds = dialog.getBoundingClientRect();
        const gallery = results.getBoundingClientRect();
        assert(bounds.top >= 0 && bounds.top <= 9 && bounds.bottom >= innerHeight - 9 && bounds.bottom <= innerHeight, 'Landscape choosers must use the available height inside a narrow frame.');
        assert(dialog.scrollHeight <= dialog.clientHeight && dialog.scrollWidth <= dialog.clientWidth, 'Chooser chrome must remain within the dialog.');
        assert(gallery.height >= innerHeight - 216, 'Errors and selections must leave usable gallery space.');
        const retry = dialog.querySelector('[data-media-retry]');
        if (retry) {
          const box = retry.getBoundingClientRect();
          assert(box.height >= 44 && box.top >= bounds.top && box.bottom <= gallery.top, 'Retry must remain touch-sized and above the gallery.');
        }
        for (const button of dialog.querySelectorAll('.media-browser-selection button, [data-close-dialog]')) {
          if (button.hidden) continue;
          const box = button.getBoundingClientRect();
          assert(box.height >= 44 && box.left >= bounds.left && box.right <= bounds.right && box.top >= bounds.top && box.bottom <= bounds.bottom, 'Chooser actions must remain touch-sized and inside the dialog.');
        }
        const image = results.querySelector('.media-grid img');
        image.scrollIntoView({block: 'center'});
        const photo = image.getBoundingClientRect();
        assert(photo.top >= gallery.top && photo.bottom <= gallery.bottom, 'The gallery must fit a complete photo preview while error feedback remains visible.');
      }
      checkLayout();
      failBrowse = true;
      filters.requestSubmit();
      await wait(() => dialog.querySelector('[data-media-retry]'));
      checkLayout();
      if (multiple) {
        dialog.querySelector('[data-media-show-selected]').click();
        const review = dialog.querySelector('.media-selection-review');
        assert(!review.hidden && review.getBoundingClientRect().height >= innerHeight - 216, 'Selected-photo review must retain usable space during a browsing failure.');
        dialog.querySelector('[data-media-clear-selection]').click();
        assert(review.hidden && !results.hidden && dialog.querySelector('[data-media-retry]'), 'Clearing selections must restore browsing without removing recovery feedback.');
        checkLayout();
      }
      failBrowse = false;
      dialog.querySelector('[data-media-retry]').click();
      await wait(() => !dialog.querySelector('[data-media-retry]') && !results.hasAttribute('aria-busy'));
      checkLayout();
      dialog.querySelector('[data-close-dialog]').click();
      await wait(() => !dialog.isConnected);
      assert(document.activeElement === opener && form.elements.title.value === 'Unsaved landscape post', 'Cancel must preserve writing and restore focus.');
    }
    window.fetch = browserFetch;
    await report('PASS');
  } catch (error) {
    await report(error.message + '\n' + error.stack);
  }
})();
