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
    const origin = new URL(frame.src, location.href).origin;
    const receive = event => { if (event.source === frame.contentWindow && event.origin === origin && event.data === 'test:ready') ready = true; };
    window.addEventListener('message', receive);
    try {
      await wait(() => {
        frame.contentWindow.postMessage('test:ready', '*');
        return ready;
      });
    } finally { window.removeEventListener('message', receive); }
  };
  const set = (field, value) => { field.value = value; field.dispatchEvent(new Event('input', {bubbles: true})); };
  const platforms = [
    ['bluesky-post', 'blueskyPost', 'https://bsky.app/profile/example.bsky.social/post/3kabc12345678'],
    ['threads-post', 'threadsPost', 'https://www.threads.com/@example/post/C1234567890'],
    ['facebook-post', 'facebookPost', 'https://www.facebook.com/example/posts/123456789']
  ];
  const platform = sessionStorage.getItem('social-post-platform') || new URLSearchParams(location.search).get('platform');
  const platformURL = platforms.find(([kind]) => kind === platform)[2];
  const cases = [['social-post', 'socialPost', platformURL]];
  const description = 'Context with <markup> & a long description that wraps on narrow phones.\nMore context.';
  try {
    const stage = sessionStorage.getItem('social-post-stage');
    if (!stage) {
      sessionStorage.setItem('social-post-platform', platform);
      await wait(() => document.querySelector('[name="socialPost"]').hidden && document.querySelector('#post-form .CodeMirror'));
      const form = document.querySelector('#post-form');
      form.elements.title.value = 'Social post review'; form.elements.slug.value = 'social-post-review';
      assert(form.querySelectorAll('[data-component-add="social-post"]').length === 1 && !form.querySelector('[data-component-add="x-post"], [data-component-add="bluesky-post"], [data-component-add="threads-post"], [data-component-add="facebook-post"]'), 'The picker must offer one Social post option.');
      // Old recovery data still uses provider-specific field names. Those
      // sections share the single picker choice and accept every platform.
      for (const [legacyKind, legacyField, legacyURL] of [...platforms, ['x-post', 'xPost', 'https://x.com/user/status/123']]) {
        const legacy = form.querySelector(`[data-post-component="${legacyKind}"]`);
        form.elements[legacyField].value = JSON.stringify({url: legacyURL, description});
        form.elements[legacyField].dispatchEvent(new Event('change', {bubbles: true}));
        assert(!legacy.hidden && form.querySelector('[data-component-add="social-post"]').hidden, 'Recovered social posts must use the single picker choice.');
        set(legacy.querySelector('[data-social-post-field="url"]'), platformURL);
        assert(JSON.parse(form.elements[legacyField].value).url === platformURL, 'Legacy editors must allow platform changes.');
        legacy.querySelector('[data-component-remove]').click();
        assert(!form.querySelector('[data-component-add="social-post"]').hidden, 'Removing a legacy component must restore the unified picker choice.');
        form.querySelector('[data-component-add="social-post"]').click();
        const replacement = form.querySelector('[data-post-component="social-post"]');
        set(replacement.querySelector('[data-social-post-field="url"]'), platformURL);
        set(replacement.querySelector('[data-social-post-field="description"]'), 'Replacement context for ' + legacyKind);
        const replacementSource = form.elements.socialPost.value;
        form.querySelector(`[data-component-undo="${legacyKind}"]`).click();
        assert(!legacy.hidden && replacement.hidden && !form.elements.socialPost.value && JSON.parse(form.elements[legacyField].value).description === description, 'Undo must restore the legacy component without a second Social media editor.');
        form.querySelector('[data-component-undo="social-post"]').click();
        assert(legacy.hidden && !replacement.hidden && !form.elements[legacyField].value && form.elements.socialPost.value === replacementSource, 'The replacement must remain available through Undo without losing its input.');
        form.querySelector('[data-component-add="social-post"]').click();
        assert(form.elements.componentOrder.value === '["social-post"]', 'A hidden Add button must not add a second choice.');
        replacement.querySelector('[data-component-remove]').click();
        form.querySelector(`[data-component-undo="${legacyKind}"]`).click();
        assert(!legacy.hidden && form.querySelector('[data-component-add="social-post"]').hidden && JSON.parse(form.elements[legacyField].value).url === platformURL, 'Both versions must remain recoverable after replacing and removing a social component.');
        legacy.querySelector('[data-component-remove]').click();
      }
      for (const [kind, field, url] of cases) {
        const section = form.querySelector(`[data-post-component="${kind}"]`);
        assert(section.hidden && section.querySelector('fieldset').disabled, 'Unused editors must stay disabled.');
        form.querySelector(`[data-component-add="${kind}"]`).click();
        assert(document.activeElement.id === kind + '-url', 'Adding a social post must focus its URL.');
        set(section.querySelector('[data-social-post-field="url"]'), 'https://evil.test/post/123');
        set(section.querySelector('[data-social-post-field="description"]'), description);
        form.requestSubmit();
        await wait(() => section.querySelector('[data-component-error]:not([hidden])'));
        assert(section.querySelector('[data-social-post-field="description"]').value === description, 'Validation must preserve context.');
        for (const [, , alternative] of platforms) {
          set(section.querySelector('[data-social-post-field="url"]'), alternative);
          assert(JSON.parse(form.elements[field].value).url === alternative && section.querySelector('[data-social-post-field="description"]').value === description, 'Switching platforms must reuse the same fields and preserve context.');
        }
        set(section.querySelector('[data-social-post-field="url"]'), url);
        const picker = section.querySelector('[data-image-picker]');
        picker.querySelector('[data-media-browse]').click();
        await wait(() => document.querySelector('.media-browser [data-media-select]'));
        document.querySelector('.media-browser [data-media-select]').click();
        await wait(() => !document.querySelector('.media-browser'));
        assert(JSON.parse(form.elements[field].value).imageID && !picker.querySelector('[data-image-preview]').hidden, 'Library selection must update the saved screenshot.');
        picker.querySelector('[data-image-reset]').click();
        assert(!JSON.parse(form.elements[field].value).imageID && picker.querySelector('[data-image-preview]').hidden, 'Clear must remove the optional screenshot.');
        picker.querySelector('[data-media-browse]').click();
        await wait(() => document.querySelector('.media-browser [data-media-select]'));
        document.querySelector('.media-browser [data-media-select]').click();
        await wait(() => !document.querySelector('.media-browser'));
        const copy = form.elements[field].value;
        section.querySelector('[data-component-remove]').click();
        assert(section.hidden && !form.elements[field].value, 'Removal must clear the source.');
        form.querySelector(`[data-component-undo="${kind}"]`).click();
        assert(form.elements[field].value === copy, 'Undo must restore the full component.');
        assert(document.documentElement.scrollWidth <= innerWidth + 1, 'Editor must fit the viewport.');
      }
      const last = form.querySelector('[data-post-component="social-post"]');
      form.querySelector('[data-component-add="video"]').click();
      set(form.querySelector('#video-url'), 'https://youtu.be/M7lc1UVf-VE');
      last.querySelector('[data-component-move="down"]').click();
      assert(form.elements.componentOrder.value === '["video","social-post"]', 'Social posts must use shared ordering.');
      form.querySelector('[name="published"][value="on"]').checked = true;
      form.dispatchEvent(new Event('input', {bubbles: true}));
      await wait(() => Object.keys(localStorage).some(key => key.startsWith('hyperserver.blog.draft.v1:') && localStorage.getItem(key).includes(platformURL)));
      sessionStorage.setItem('social-post-stage', 'recover'); location.reload(); return;
    }
    if (stage === 'recover') {
      await wait(() => document.querySelector('[data-draft-recovery]') && !document.querySelector('[data-draft-recovery]').hidden);
      document.querySelector('[data-draft-restore]').click();
      const form = document.querySelector('#post-form');
      for (const [kind, field] of cases) {
        assert(JSON.parse(form.elements[field].value).imageID && !form.querySelector(`[data-post-component="${kind}"]`).hidden, 'Recovery must restore each post and screenshot.');
      }
      sessionStorage.setItem('social-post-stage', 'reader'); form.requestSubmit(); return;
    }
    if (stage === 'reader') {
      await wait(() => document.querySelectorAll('.post-body .social-post-component').length === 1);
      const sections = [...document.querySelectorAll('.post-body .social-post-component')];
      assert(sections.map(s => s.dataset.socialProvider).join() === platform, 'Reader must detect the saved URL platform.');
      for (const section of sections) {
        section.scrollIntoView({block: 'center'});
        await wait(() => section.querySelector('iframe'));
        const frame = section.querySelector('iframe');
        await frameReady(frame);
        const fallback = section.querySelector('[data-social-post-fallback]');
        const actions = section.querySelector('.social-post-actions');
        assert(!fallback.hidden && !actions.hidden && getComputedStyle(frame).visibility === 'hidden', 'Loading must preserve the screenshot and viewing link.');
        assert(section.querySelector('.social-post-description').textContent === description && !section.querySelector('markup'), 'Context must render safely.');
        const spoof = new MessageEvent('message', {origin: 'https://untrusted.example', data: 1000});
        Object.defineProperty(spoof, 'source', {value: frame.contentWindow});
        window.dispatchEvent(spoof);
        window.dispatchEvent(new MessageEvent('message', {origin: new URL(frame.src).origin, source: window, data: 1000}));
        await pause();
        assert(frame.getBoundingClientRect().height < 1000 && !fallback.hidden, 'Other origins must not complete or resize a post.');
        if (section.dataset.socialProvider !== 'threads-post') {
          frame.contentWindow.postMessage('test:bad-id', '*'); await pause();
          assert(!fallback.hidden && frame.getBoundingClientRect().height < 777, 'Wrong callback IDs must be ignored.');
        }
        fallback.focus();
        frame.contentWindow.postMessage('test:render', '*');
        await wait(() => fallback.hidden && actions.hidden);
        assert(getComputedStyle(fallback).display === 'none', 'A completed embed must hide the screenshot visually.');
        assert(document.activeElement === frame && frame.style.height === '222px', 'Completion must transfer hidden fallback focus and round up height.');
        assert(frame.getBoundingClientRect().right <= section.getBoundingClientRect().right + 1, 'The whole provider card must fit the containing box.');
        frame.contentWindow.postMessage({resize: 9000}, '*');
        await wait(() => frame.style.height === '4096px');
        assert(frame.getAttribute('scrolling') === 'auto', 'Very tall embeds must remain scrollable.');
        frame.contentWindow.postMessage({resize: 220.4}, '*');
        await wait(() => frame.style.height === '222px');
        assert(document.documentElement.scrollWidth <= innerWidth + 1, 'Reader must fit the viewport.');
      }
      const first = sections[0];
      const originalURLs = sections.map(section => section.querySelector('[data-social-post]').dataset.socialPost);
      window.dispatchEvent(new PageTransitionEvent('pagehide'));
      for (const section of sections) section.querySelector('[data-social-post]').dataset.socialPost += '/missing';
      window.dispatchEvent(new PageTransitionEvent('pageshow'));
      for (const section of sections) {
        section.scrollIntoView({block: 'center'});
        await wait(() => section.querySelector('[data-social-post-load]').textContent.includes('Retry'));
        assert(!section.querySelector('iframe') && !section.querySelector('[data-social-post-fallback]').hidden && !section.querySelector('.social-post-actions').hidden, 'An unavailable post must keep its screenshot and viewing link.');
        assert(!section.querySelector('[data-social-post]').style.height, 'A failed post must not leave empty frame space.');
      }
      // Cached markup must not preserve loaded provider frames or hidden fallbacks.
      window.dispatchEvent(new PageTransitionEvent('pagehide'));
      sections.forEach((section, index) => { section.querySelector('[data-social-post]').dataset.socialPost = originalURLs[index]; });
      assert(sections.every(s => !s.querySelector('iframe') && !s.querySelector('[data-social-post-fallback]').hidden), 'Page caching must restore static fallbacks.');
      const originalTimeout = window.setTimeout;
      window.setTimeout = (callback, delay, ...args) => originalTimeout(callback, delay === 15000 ? 250 : delay, ...args);
      window.dispatchEvent(new PageTransitionEvent('pageshow'));
      first.scrollIntoView({block: 'center'});
      await wait(() => first.querySelector('iframe'));
      await wait(() => first.querySelector('[data-social-post-load]').textContent.includes('Retry'));
      window.setTimeout = originalTimeout;
      assert(!first.querySelector('[data-social-post-fallback]').hidden && !first.querySelector('iframe'), 'Stalled loading must preserve the screenshot and offer Retry.');
      first.querySelector('[data-social-post-load]').click();
      await wait(() => first.querySelector('iframe'));
      await frameReady(first.querySelector('iframe'));
      first.querySelector('iframe').contentWindow.postMessage('test:render', '*');
      await wait(() => first.querySelector('[data-social-post-fallback]').hidden);
      sessionStorage.setItem('social-post-stage', 'edit');
      await htmx.ajax('GET', document.querySelector('a[href^="/post/update/"]').getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
    }
    await wait(() => document.querySelector('#post-form .CodeMirror'));
    const form = document.querySelector('#post-form');
    for (const [kind, field] of cases) {
      const section = form.querySelector(`[data-post-component="${kind}"]`);
      assert(JSON.parse(form.elements[field].value).description === description && !section.querySelector('[data-component-unsaved]:not([hidden])'), 'Reopening must preserve context and have a clean saved indicator.');
    }
    sessionStorage.removeItem('social-post-stage'); sessionStorage.removeItem('social-post-platform'); await report('PASS');
  } catch (error) { await report(String(error) + '\n' + (error.stack || '')); }
})();
