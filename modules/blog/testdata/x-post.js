(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const wait = async predicate => {
    const end = Date.now() + 12000;
    while (!predicate()) { if (Date.now() > end) throw new Error('Timed out: ' + predicate); await new Promise(resolve => setTimeout(resolve, 25)); }
  };
  const set = (field, value) => { field.value = value; field.dispatchEvent(new Event('input', {bubbles: true})); };
  const description = 'Context with <markup> & a long description that wraps onto more than one line on narrow phones.\nMore context.';
  try {
    const stage = sessionStorage.getItem('x-post-stage');
    if (!stage) {
      await wait(() => document.querySelector('[name="xPost"]').hidden && document.querySelector('#post-form .CodeMirror'));
      const form = document.querySelector('#post-form');
      const section = form.querySelector('[data-x-post-editor]');
      assert(section.hidden && section.querySelector('fieldset').disabled, 'An unused component must not block Save.');
      form.elements.title.value = 'X post component review'; form.elements.slug.value = 'x-post-component-review';
      form.querySelector('[data-component-add="x-post"]').click();
      assert(!section.hidden && document.activeElement.id === 'x-post-url', 'Adding an X post must focus its URL.');
      set(form.querySelector('#x-post-url'), 'https://evil.test/user/status/123');
      set(form.querySelector('#x-post-description'), description);
      form.requestSubmit();
      await wait(() => section.querySelector('[data-component-error]:not([hidden])'));
      assert(form.querySelector('#x-post-description').value === description, 'Invalid URL errors must retain authored context.');
      set(form.querySelector('#x-post-url'), 'https://twitter.com/user/status/560070183650213889?s=20');
      const original = form.elements.xPost.value;
      section.querySelector('[data-component-remove]').click();
      assert(section.hidden && !form.elements.xPost.value && section.querySelector('fieldset').disabled, 'Removal must clear and disable fields.');
      form.querySelector('[data-component-undo="x-post"]').click();
      assert(form.elements.xPost.value === original && form.querySelector('#x-post-description').value === description, 'Undo must restore the whole component.');
      form.querySelector('[data-component-add="video"]').click();
      section.querySelector('[data-component-move="down"]').click();
      assert(form.elements.componentOrder.value === '["video","x-post"]', 'X posts must use shared ordering.');
      form.querySelector('[data-video-editor] [data-component-remove]').click();
      form.querySelector('[name="published"][value="on"]').checked = true;
      form.dispatchEvent(new Event('input', {bubbles: true}));
      await wait(() => Object.keys(localStorage).some(key => key.startsWith('hyperserver.blog.draft.v1:') && localStorage.getItem(key).includes('560070183650213889')));
      sessionStorage.setItem('x-post-stage', 'recover'); location.reload(); return;
    }
    if (stage === 'recover') {
      await wait(() => document.querySelector('[data-draft-recovery]') && !document.querySelector('[data-draft-recovery]').hidden);
      document.querySelector('[data-draft-restore]').click();
      const form = document.querySelector('#post-form');
      assert(form.querySelector('#x-post-description').value === description && form.elements.componentOrder.value === '["x-post"]', 'Recovery must preserve X post content and order.');
      sessionStorage.setItem('x-post-stage', 'reader'); form.requestSubmit(); return;
    }
    await wait(() => document.querySelector('.post-body .x-post-component'));
    const component = document.querySelector('.post-body .x-post-component');
    const host = component.querySelector('[data-x-post]');
    const button = component.querySelector('[data-x-post-load]');
    const actions = component.querySelector('.x-post-actions');
    assert(component.querySelector('.x-post-description').textContent === description && !component.querySelector('markup'), 'Authored context must render safely.');
    const focused = document.activeElement;
    component.scrollIntoView({block: 'center'});
    assert(component.querySelector('a').href.endsWith('/i/web/status/560070183650213889') && component.querySelector('a').getAttribute('hx-boost') === 'false', 'A canonical ordinary fallback link must remain available.');
    await wait(() => host.querySelector('iframe')?.style.height === '221px');
    let frame = host.querySelector('iframe');
    assert(!actions.hidden, 'Open on X must remain available while the embed loads.');
    assert(button.disabled && button.hidden && component.querySelector('[data-x-post-status]').textContent.includes('Loading'), 'A resize must not report successful loading before X confirms rendering.');
    const rendered = {'twttr.embed': {jsonrpc: '2.0', method: 'twttr.private.rendered', params: [{}]}};
    const untrusted = new MessageEvent('message', {origin: 'https://untrusted.example', data: rendered});
    // Firefox cannot construct a MessageEvent with a cross-origin source.
    Object.defineProperty(untrusted, 'source', {value: frame.contentWindow});
    window.dispatchEvent(untrusted);
    window.dispatchEvent(new MessageEvent('message', {origin: new URL(frame.src).origin, source: window, data: rendered}));
    assert(button.disabled && button.hidden, 'Another origin or window must not finish loading the post.');
    const geometry = new Promise(resolve => {
      const receive = event => {
        if (event.source !== frame.contentWindow || event.data?.['twttr.embed']?.method !== 'twttr.private.rendered') return;
        window.removeEventListener('message', receive);
        resolve(event.data['twttr.embed'].params[0]);
      };
      window.addEventListener('message', receive);
    });
    frame.contentWindow.postMessage('test:render', new URL(frame.src).origin);
    const dimensions = await geometry;
    assert(dimensions.viewport === dimensions.content, 'The provider must not force a scrollbar when its content fits.');
    await wait(() => button.hidden && !button.disabled);
    assert(actions.hidden && getComputedStyle(actions).display === 'none', 'A rendered embed must hide the redundant Open on X link.');
    assert(document.activeElement === focused, 'Automatic loading must not move keyboard focus.');
    assert(frame.sandbox.contains('allow-same-origin') && frame.referrerPolicy === 'no-referrer' && button.hidden, 'The working embed must stay isolated and hide its load button.');
    window.dispatchEvent(new MessageEvent('message', {origin: new URL(frame.src).origin, source: window, data: {'twttr.embed': {jsonrpc:'2.0', method:'twttr.private.resize', params:[{height:3000}]}}}));
    assert(frame.style.height === '221px', 'Resize messages from another window must be ignored.');
    frame.contentWindow.postMessage({resize: 5000}, new URL(frame.src).origin);
    await wait(() => frame.style.height === '4096px');
    assert(frame.getAttribute('scrolling') === 'auto', 'Posts beyond the height limit must remain scrollable.');
    frame.contentWindow.postMessage({resize: 220}, new URL(frame.src).origin);
    await wait(() => frame.style.height === '221px');
    assert(frame.getAttribute('scrolling') === 'no', 'Posts that fit must hide the provider scrollbar.');
    assert(document.documentElement.scrollWidth <= innerWidth + 1, 'X post content must fit the viewport.');
    const surface = getComputedStyle(component);
    assert(surface.backgroundColor === 'rgba(0, 0, 0, 0)' && parseFloat(surface.paddingTop) === 0 && component.getBoundingClientRect().width <= 550, 'The X card must stand alone without a padded gray surface.');
    // A component below the viewport waits until the reader reaches it.
    const later = component.cloneNode(true);
    later.querySelector('iframe').remove();
    later.querySelector('.x-post-actions').hidden = false;
    later.style.marginTop = '200vh';
    component.parentElement.append(later);
    later.dispatchEvent(new Event('htmx:after:process', {bubbles: true}));
    await new Promise(resolve => setTimeout(resolve, 150));
    assert(!later.querySelector('iframe'), 'An offscreen post must wait until it is displayed.');
    later.scrollIntoView({block: 'center'});
    await wait(() => later.querySelector('iframe')?.style.height === '221px');
    const laterFrame = later.querySelector('iframe');
    laterFrame.contentWindow.postMessage('test:render', new URL(laterFrame.src).origin);
    await wait(() => !later.querySelector('[data-x-post-load]').disabled);
    later.dispatchEvent(new Event('htmx:before:cleanup', {bubbles: true}));
    later.remove();
    component.scrollIntoView({block: 'center'});
    assert(host.querySelector('iframe') === frame, 'Scrolling back must not reload a working post.');
    // Browser-cache restoration automatically reloads a visible post.
    window.dispatchEvent(new Event('pagehide'));
    assert(!host.querySelector('iframe') && button.hidden && !button.disabled && !actions.hidden, 'History must discard live frames and restore the fallback link.');
    window.dispatchEvent(new Event('pageshow'));
    await wait(() => host.querySelector('iframe')?.style.height === '221px');
    frame = host.querySelector('iframe');
    actions.querySelector('a').focus();
    frame.contentWindow.postMessage('test:render', new URL(frame.src).origin);
    await wait(() => button.hidden && !button.disabled);
    assert(actions.hidden && document.activeElement === frame, 'Hiding a focused fallback link must move focus to the rendered embed.');
    // An automatic failure exposes Retry without creating a retry loop.
    window.dispatchEvent(new Event('pagehide'));
    host.dataset.xPost = '2';
    window.dispatchEvent(new Event('pageshow'));
    await wait(() => !button.hidden && button.textContent === 'Retry X post');
    assert(!actions.hidden, 'A failed embed must show the Open on X fallback.');
    component.dispatchEvent(new Event('htmx:after:process', {bubbles: true}));
    await new Promise(resolve => setTimeout(resolve, 150));
    assert(!host.querySelector('iframe'), 'Failed automatic loading must wait for a manual retry.');
    for (const id of ['3', '5']) {
      host.dataset.xPost = id;
      const originalTimeout = window.setTimeout;
      if (id === '3' || id === '5') window.setTimeout = (callback, delay, ...args) => originalTimeout(callback, delay === 15000 ? 100 : delay, ...args);
      button.focus();
      button.click();
      await wait(() => !button.disabled && button.textContent === 'Retry X post');
      window.setTimeout = originalTimeout;
      assert(!host.querySelector('iframe') && component.querySelector('[data-x-post-status]').textContent.includes('could not load') && component.querySelector('a') && document.activeElement === button, 'Missing and stalled embeds must offer retry, return keyboard focus, and keep the fallback.');
    }
    host.dataset.xPost = '560070183650213889'; button.click();
    await wait(() => host.querySelector('iframe')?.style.height === '221px');
    frame = host.querySelector('iframe');
    const otherLink = document.querySelector('a[href^="/post/update/"]');
    otherLink.focus();
    frame.contentWindow.postMessage('test:render', new URL(frame.src).origin);
    await wait(() => button.hidden && !button.disabled);
    assert(document.activeElement === otherLink, 'Finishing loading must not steal focus after the reader moves away.');
    await htmx.ajax('GET', document.querySelector('a[href^="/post/update/"]').getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
    await wait(() => document.querySelector('#x-post-description'));
    assert(document.querySelector('#x-post-description').value === description && !document.querySelector('[data-x-post-editor] [data-component-unsaved]:not([hidden])'), 'Reopening must preserve content with a clean saved indicator.');
    sessionStorage.removeItem('x-post-stage'); await report('PASS');
  } catch (error) { await report(String(error) + '\n' + (error.stack || '')); }
})();
