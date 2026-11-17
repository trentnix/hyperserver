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
  try {
    const section = document.querySelector('.social-post-component');
    const kind = section.dataset.socialProvider;
    section.scrollIntoView();
    await wait(() => section.querySelector('iframe'));
    let frame = section.querySelector('iframe');
    const fallback = section.querySelector('[data-social-post-fallback]');
    const actions = section.querySelector('.social-post-actions');
    const host = section.querySelector('[data-social-post]');
    const originalURL = host.dataset.socialPost;
    const child = () => kind === 'mastodon-post' ? frame.contentDocument?.querySelector('iframe') : frame;
    await wait(() => child()?.contentWindow);
    await frameReady(child());
    if (kind === 'linkedin-post') {
      const button = section.querySelector('[data-social-post-load]');
      await wait(() => fallback.hidden && !button.disabled);
      assert(!actions.hidden && frame.style.height === '600px' && frame.getAttribute('scrolling') === 'auto', 'LinkedIn must remain usable without a cross-site resize protocol.');
      assert(!button.hidden && button.textContent === 'Retry LinkedIn post', 'A LinkedIn frame load must not remove recovery controls.');
      host.dataset.socialPost = originalURL.replace(/\d+$/, '999998');
      const previous = frame;
      button.focus();
      button.click();
      await wait(() => section.querySelector('iframe') && section.querySelector('iframe') !== previous && fallback.hidden && !button.disabled);
      frame = section.querySelector('iframe');
      assert(!button.hidden && !actions.hidden && document.activeElement === frame, 'A LinkedIn error document must retain Open and Retry and transfer focus to its frame.');
      host.dataset.socialPost = originalURL;
    } else {
      if (kind === 'tiktok-post') {
        await wait(() => fallback.hidden && !section.querySelector('[data-social-post-load]').disabled);
        assert(!actions.hidden && getComputedStyle(frame).visibility === 'visible', 'A TikTok preview must stay usable with Open and Retry before Play.');
        child().contentWindow.postMessage('test:dimensions', '*'); await pause();
        assert(!actions.hidden && !section.querySelector('[data-social-post-load]').hidden, 'Dimensions alone must not complete a TikTok post.');
        const previous = frame;
        section.querySelector('[data-social-post-load]').focus();
        section.querySelector('[data-social-post-load]').click();
        await wait(() => section.querySelector('iframe') && section.querySelector('iframe') !== previous);
        frame = section.querySelector('iframe');
        await frameReady(frame);
        await wait(() => fallback.hidden && !section.querySelector('[data-social-post-load]').disabled);
        assert(document.activeElement === frame, 'Retry must transfer focus from the fallback to the loaded preview.');
      } else {
        assert(!fallback.hidden && !actions.hidden, 'Pending embeds must preserve the fallback and viewing link.');
      }
      const payload = kind === 'reddit-post' ? JSON.stringify({type:'resize.embed',data:777}) : kind === 'instagram-post' ? JSON.stringify({type:'MEASURE',details:{height:777}}) : {'x-tiktok-player':true,type:'onPlayerReady'};
      const spoof = new MessageEvent('message', {origin: 'https://untrusted.example', data: payload});
      Object.defineProperty(spoof, 'source', {value: frame.contentWindow});
      window.dispatchEvent(spoof);
      window.dispatchEvent(new MessageEvent('message', {origin: new URL(frame.src, location.href).origin, source: window, data: payload}));
      await pause();
      assert(kind === 'tiktok-post' ? !actions.hidden : !fallback.hidden, 'An untrusted origin or window must not complete an embed.');
      if (kind === 'mastodon-post') {
        child().contentWindow.postMessage('test:bad-id', '*'); await pause();
        assert(!fallback.hidden, 'Mastodon must check its native callback ID.');
      }
      (kind === 'tiktok-post' ? actions.querySelector('a') : fallback).focus();
      child().contentWindow.postMessage('test:render', '*');
      await wait(() => fallback.hidden && actions.hidden);
      assert(document.activeElement === frame && getComputedStyle(fallback).display === 'none', 'Completion must transfer fallback focus without leaving a screenshot behind.');
      if (kind !== 'tiktok-post') {
        const expectedHeight = kind === 'mastodon-post' && frame.getBoundingClientRect().width < 400 ? '501px' : '222px';
        await wait(() => frame.style.height === expectedHeight);
        child().contentWindow.postMessage({resize:9000}, '*');
        await wait(() => frame.style.height === '4096px');
        assert(frame.getAttribute('scrolling') === 'auto', 'Very tall posts must stay scrollable.');
        if (kind === 'mastodon-post') {
          const originalStyle = section.style.cssText;
          section.style.maxWidth = 'none';
          section.style.width = '450px';
          await wait(() => frame.style.width === '450px' && frame.style.height === '222px');
          section.style.width = '300px';
          await wait(() => frame.style.width === '300px' && frame.style.height === '501px');
          section.style.width = '600px';
          await wait(() => frame.style.width === '600px' && frame.style.height === '222px');
          section.style.cssText = originalStyle;
          await wait(() => frame.getBoundingClientRect().right <= section.getBoundingClientRect().right + 1);
        }
      } else {
        assert(Math.abs(frame.getBoundingClientRect().height / frame.getBoundingClientRect().width - 16 / 9) < 0.01, 'TikTok must keep its portrait player ratio.');
        child().contentWindow.postMessage('test:error', '*');
        await wait(() => !section.querySelector('iframe'));
        assert(!fallback.hidden && !actions.hidden, 'A TikTok player error must restore the local fallback.');
      }
    }
    assert(document.documentElement.scrollWidth <= innerWidth + 1, 'The provider must fit narrow content widths.');
    window.dispatchEvent(new PageTransitionEvent('pagehide'));
    assert(!section.querySelector('iframe') && !fallback.hidden && !actions.hidden, 'Navigation cleanup must restore the static fallback.');
    const originalTimeout = window.setTimeout;
    window.setTimeout = (callback, delay, ...args) => originalTimeout(callback, delay === 15000 ? 1000 : delay, ...args);
    if (kind === 'tiktok-post' || kind === 'linkedin-post') host.dataset.socialPost = originalURL.replace(/\d+$/, '999999');
    window.dispatchEvent(new PageTransitionEvent('pageshow'));
    if (kind === 'tiktok-post' || kind === 'linkedin-post') {
      await wait(() => section.querySelector('iframe'));
      frame = section.querySelector('iframe');
      await frameReady(frame);
      frame.contentWindow.postMessage('test:dimensions', '*');
    }
    await wait(() => section.querySelector('[data-social-post-load]').textContent.includes('Retry'));
    window.setTimeout = originalTimeout;
    assert(!section.querySelector('iframe') && !fallback.hidden, 'Stalled embeds must offer Retry and preserve the fallback.');
    host.dataset.socialPost = originalURL;
    section.querySelector('[data-social-post-load]').click();
    await wait(() => section.querySelector('iframe'));
    frame = section.querySelector('iframe');
    await wait(() => child()?.contentWindow);
    await frameReady(child());
    child().contentWindow.postMessage('test:render', '*');
    await wait(() => fallback.hidden);
    await report('PASS');
  } catch (error) { await report(String(error) + '\n' + (error.stack || '')); }
})();
