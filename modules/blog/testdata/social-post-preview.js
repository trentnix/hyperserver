(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const wait = async predicate => {
    const end = Date.now() + 12000;
    while (!predicate()) { if (Date.now() > end) throw new Error('Timed out: ' + predicate); await new Promise(resolve => setTimeout(resolve, 25)); }
  };
  const pause = () => new Promise(resolve => setTimeout(resolve, 100));
  const frameReady = async frame => {
    let ready = false;
    const origin = new URL(frame.src).origin;
    const receive = event => { if (event.source === frame.contentWindow && event.origin === origin && event.data === 'test:ready') ready = true; };
    window.addEventListener('message', receive);
    try { await wait(() => { frame.contentWindow.postMessage('test:ready', origin); return ready; }); }
    finally { window.removeEventListener('message', receive); }
  };
  const set = (field, value) => { field.value = value; field.dispatchEvent(new Event('input', {bubbles: true})); };
  try {
    const data = await (await fetch('/test/social-preview-data')).json();
    await wait(() => document.querySelector('[name="socialPost"]').hidden && document.querySelector('#post-form .CodeMirror'));
    const form = document.querySelector('#post-form');
    form.querySelector('[data-component-add="social-post"]').click();
    const editor = form.querySelector('[data-post-component="social-post"]');
    const source = form.elements.socialPost;
    const url = editor.querySelector('[data-social-post-field="url"]');
    const description = editor.querySelector('[data-social-post-field="description"]');
    const preview = editor.querySelector('[data-social-post-live-preview]');
    const card = () => preview.querySelector('.social-post-component, .x-post-component');
    const frame = () => preview.querySelector('iframe');
    async function load(sample) {
      url.focus();
      set(url, sample.URL);
      assert(preview.hidden && !frame(), 'Typing a URL must immediately remove the previous preview.');
      description.focus();
      await wait(() => card());
      preview.scrollIntoView({block: 'center'});
      await wait(() => frame());
      assert(sample.Kind === 'x-post' ? card().classList.contains('x-post-component') : card().dataset.socialProvider === sample.Kind, 'The preview must detect the current platform.');
    }
    for (const sample of data.Samples) {
      await load(sample);
      const before = frame();
      const context = 'Unsaved <markup> & a long description that wraps on a phone.\nMore context.';
      set(description, context);
      assert(card().querySelector('.social-post-description, .x-post-description').textContent === context && !card().querySelector('markup'), 'Description edits must appear safely in the preview.');
      await pause();
      assert(frame() === before, 'Description edits must not reload the provider.');
      assert(preview.compareDocumentPosition(description) & Node.DOCUMENT_POSITION_FOLLOWING, 'The preview must precede the description.');
      assert(document.documentElement.scrollWidth <= innerWidth + 1, 'The preview must fit the editor at narrow widths.');
    }
    const reddit = data.Samples.find(sample => sample.Kind === 'reddit-post');
    await load(reddit);
    const picker = editor.querySelector('[data-image-picker]');
    picker.querySelector('[data-media-browse]').click();
    await wait(() => document.querySelector('.media-browser [data-media-select]'));
    document.querySelector('.media-browser [data-media-select]').click();
    await wait(() => !document.querySelector('.media-browser') && card()?.querySelector('[data-social-post-fallback]'));
    preview.scrollIntoView({block: 'center'});
    await wait(() => frame());
    let fallback = card().querySelector('[data-social-post-fallback]');
    assert(!fallback.hidden, 'The unsaved screenshot must appear while the embed loads.');
    await frameReady(frame());
    frame().contentWindow.postMessage('test:render', new URL(frame().src).origin);
    await wait(() => card().querySelector('.social-post-actions').hidden);
    const authored = source.value;
    const root = editor.closest('[data-post-components]');
    form.querySelector('[data-component-add="video"]').click();
    if (root.moveBefore) {
      const loaded = frame();
      let reloaded = false;
      loaded.addEventListener('load', () => { reloaded = true; });
      editor.querySelector('[data-component-move="down"]').click();
      await pause();
      assert(frame() === loaded && !reloaded && fallback.hidden, 'Moving a component must preserve a rendered iframe when supported.');
      editor.querySelector('[data-component-move="up"]').click();
    }
    const moveBefore = root.moveBefore;
    root.moveBefore = undefined;
    const previous = frame();
    const originalTimeout = window.setTimeout;
    window.setTimeout = (callback, delay, ...args) => originalTimeout(callback, delay === 15000 ? 500 : delay, ...args);
    editor.querySelector('[data-component-move="down"]').click();
    await wait(() => frame() && frame() !== previous);
    fallback = card().querySelector('[data-social-post-fallback]');
    assert(!fallback.hidden && !card().querySelector('.social-post-actions').hidden && source.value === authored, 'Older browsers must rebuild a moved preview with its fallback and input intact.');
    await wait(() => !frame());
    window.setTimeout = originalTimeout;
    root.moveBefore = moveBefore;
    assert(!fallback.hidden && !card().querySelector('[data-social-post-load]').hidden, 'A stalled reload after reordering must retain a working Retry button.');
    card().querySelector('[data-social-post-load]').click();
    await wait(() => frame());
    frame().dispatchEvent(new Event('error'));
    await wait(() => !frame());
    assert(!fallback.hidden && !card().querySelector('.social-post-actions').hidden, 'An embed failure must preserve its screenshot and viewing link.');
    picker.querySelector('[data-image-reset]').click();
    await wait(() => card() && !card().querySelector('[data-social-post-fallback]'));

    const originalFetch = window.fetch;
    let release, held;
    window.fetch = async (...args) => {
      const response = await originalFetch(...args);
      if (args[0] === '/post/social-preview' && !held) {
        held = true;
        await new Promise(resolve => { release = resolve; });
      }
      return response;
    };
    url.focus(); set(url, data.Samples[0].URL); description.focus();
    await wait(() => release);
    await load(reddit);
    release();
    await pause();
    assert(card().dataset.socialProvider === reddit.Kind, 'An old response must not replace the current URL preview.');
    window.fetch = async (...args) => args[0] === '/post/social-preview' ? new Response('Unavailable', {status: 503}) : originalFetch(...args);
    url.focus(); set(url, data.Samples[0].URL); description.focus();
    await wait(() => !preview.querySelector('[data-social-post-preview-retry]').hidden);
    assert(!card() && !frame() && JSON.parse(source.value).url === data.Samples[0].URL, 'A preview failure must preserve the author input without stale content.');
    window.fetch = originalFetch;
    const retry = preview.querySelector('[data-social-post-preview-retry]');
    retry.focus();
    retry.click();
    assert(document.activeElement === preview && !preview.hidden, 'Retry must move keyboard focus to the visible preview region.');
    await wait(() => card());
    assert(document.activeElement === preview, 'Loading a retried preview must preserve keyboard focus.');
    const copy = source.value;
    editor.querySelector('[data-component-remove]').click();
    assert(preview.hidden && !frame(), 'Removing the component must release its preview.');
    form.querySelector('[data-component-undo="social-post"]').click();
    await wait(() => card());
    assert(source.value === copy, 'Undo must restore the unsaved URL and its preview.');
    url.focus(); set(url, 'not a URL'); description.focus();
    await pause();
    assert(preview.hidden && !frame(), 'An invalid or empty URL must not contact a provider.');
    const navigation = htmx.ajax('GET', data.EditURL, {target: '#article-content', swap: 'innerHTML'});
    await wait(() => document.querySelector('[data-confirm-accept]'));
    document.querySelector('[data-confirm-accept]').click();
    await navigation;
    await wait(() => document.querySelector('[data-social-post-live-preview] .x-post-component'));
    const savedEditor = document.querySelector('[data-post-component="social-post"]');
    savedEditor.querySelector('[data-social-post-live-preview]').scrollIntoView({block: 'center'});
    await wait(() => savedEditor.querySelector('iframe'));
    assert(!savedEditor.querySelector('[data-component-unsaved]:not([hidden])'), 'Opening a saved preview must not mark the component as changed.');
    window.dispatchEvent(new PageTransitionEvent('pagehide'));
    assert(!savedEditor.querySelector('iframe'), 'Cached navigation must release preview frames.');
    window.dispatchEvent(new PageTransitionEvent('pageshow'));
    await wait(() => savedEditor.querySelector('iframe'));
    await report('PASS');
  } catch (error) { await report(String(error) + '\n' + (error.stack || '')); }
})();
