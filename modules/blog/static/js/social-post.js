// Each provider owns its iframe. Only the blog's bundled code runs in the page.
const providers = {
  'bluesky-post': {name: 'Bluesky', origin: 'https://embed.bsky.app', minWidth: 300},
  'threads-post': {name: 'Threads', origin: 'https://www.threads.com', minWidth: 270},
  'facebook-post': {name: 'Facebook', origin: 'https://www.facebook.com', minWidth: 350},
  'reddit-post': {name: 'Reddit', origin: 'https://embed.reddit.com', minWidth: 300},
  'instagram-post': {name: 'Instagram', origin: 'https://www.instagram.com', minWidth: 326},
  'linkedin-post': {name: 'LinkedIn', origin: 'https://www.linkedin.com', minWidth: 300, fixedHeight: 600},
  'tiktok-post': {name: 'TikTok', origin: 'https://www.tiktok.com', minWidth: 240, portrait: true},
  'mastodon-post': {name: 'Mastodon', origin: location.origin, minWidth: 300}
};
const active = new Map();
const initialized = new WeakSet();
const sizes = new ResizeObserver(entries => {
  for (const entry of entries) {
    const section = entry.target.closest('.social-post-component');
    const state = active.get(section);
    if (state?.frame) fit(section, state);
  }
});
const observer = typeof IntersectionObserver === 'function' ? new IntersectionObserver(entries => {
  for (const entry of entries) {
    if (!entry.isIntersecting) continue;
    observer.unobserve(entry.target);
    load(entry.target);
  }
}) : null;

function fit(section, state) {
  const host = section.querySelector('[data-social-post]');
  const width = host.getBoundingClientRect().width;
  if (!width) return;
  // Provider cards have minimum widths. Fit the whole card on narrow screens
  // instead of clipping its text or adding a horizontal scrollbar.
  const frameWidth = Math.max(state.provider.minWidth, width);
  const scale = width / frameWidth;
  state.frame.style.width = frameWidth + 'px';
  state.frame.style.transform = scale < 1 ? 'scale(' + scale + ')' : '';
  if (state.provider.portrait) {
    state.height = Math.ceil(frameWidth * 16 / 9);
    state.frame.style.height = state.height + 'px';
  }
  host.style.height = Math.ceil((state.height || 100) * scale) + 'px';
}

async function embedResponse(endpoint, url, signal) {
  const response = await fetch(endpoint + '?' + new URLSearchParams({url}), {
    signal, credentials: 'omit', referrerPolicy: 'no-referrer', redirect: 'error'
  });
  if (!response.ok) throw new Error('Post unavailable');
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let text = '', bytes = 0;
  try {
    for (;;) {
      const {done, value} = await reader.read();
      if (done) break;
      bytes += value.length;
      if (bytes > 65536) throw new Error('Embed response too large');
      text += decoder.decode(value, {stream: true});
    }
    text += decoder.decode();
  } finally { await reader.cancel(); }
  const value = JSON.parse(text);
  if (value.type !== 'rich' || typeof value.html !== 'string' || !value.html) throw new Error('Invalid embed response');
  return value.html;
}

async function blueskyURI(url, signal) {
  // Never insert the provider's HTML or execute its script. Extract only the
  // validated post identity from the official oEmbed response.
  const html = await embedResponse('https://embed.bsky.app/oembed', url, signal);
  const uri = html.match(/\bdata-bluesky-uri="(at:\/\/did:(?:plc:[a-z2-7]{24}|web:[a-zA-Z0-9.-]+)\/app\.bsky\.feed\.post\/[a-zA-Z0-9._~:-]{1,512})"/)?.[1];
  if (!uri || uri.split('/').pop() !== new URL(url).pathname.split('/').pop()) throw new Error('Invalid Bluesky post identity');
  return uri.slice('at://'.length);
}

async function frameURL(section, state) {
  const url = section.querySelector('[data-social-post]').dataset.socialPost;
  switch (section.dataset.socialProvider) {
    case 'bluesky-post':
      return providers['bluesky-post'].origin + '/embed/' + await blueskyURI(url, state.controller.signal) + '?' + new URLSearchParams({id: state.id, colorMode: 'light'});
    case 'threads-post':
      await embedResponse('https://graph.threads.com/oembed', url, state.controller.signal);
      return url + '/embed/';
    case 'facebook-post': {
      await embedResponse('https://graph.facebook.com/v27.0/oembed_post', url, state.controller.signal);
      // Match the channel used by Facebook's official post plugin. The callback
      // ID belongs to this frame and every message also checks its origin/window.
      const channel = 'https://staticxx.facebook.com/x/connect/xd_arbiter/#' + new URLSearchParams({
        cb: state.id, origin: location.origin + '/' + state.id, domain: location.hostname, relation: 'parent.parent', is_canvas: 'false'
      });
      return providers['facebook-post'].origin + '/plugins/post.php?' + new URLSearchParams({
        href: url, show_text: 'true', width: state.width, container_width: state.width, sdk: 'joey', locale: 'en_US', channel
      });
    }
    case 'reddit-post':
      return providers['reddit-post'].origin + new URL(url).pathname + '?' + new URLSearchParams({embed: 'true', theme: 'light'});
    case 'instagram-post':
      await embedResponse('https://graph.facebook.com/v27.0/instagram_oembed', url, state.controller.signal);
      return url + 'embed/captioned/';
    case 'linkedin-post':
      return providers['linkedin-post'].origin + '/embed' + new URL(url).pathname;
    case 'tiktok-post':
      return providers['tiktok-post'].origin + '/player/v1/' + new URL(url).pathname.split('/').pop() + '?' + new URLSearchParams({autoplay: '0', description: '1', music_info: '1'});
    case 'mastodon-post':
      return '/social/mastodon?' + new URLSearchParams({url, id: state.id});
    default: throw new Error('Unknown social provider');
  }
}

function initialize(root) {
  for (const section of root.querySelectorAll('.social-post-component')) {
    if (initialized.has(section) || !section.querySelector('[data-social-post]') || !providers[section.dataset.socialProvider]) continue;
    reset(section);
    initialized.add(section);
    if (observer) observer.observe(section);
    else section.querySelector('[data-social-post-load]').hidden = false;
  }
}

function finish(section, failed = false) {
  const state = active.get(section);
  if (!state) return;
  clearTimeout(state.timer);
  state.controller.abort();
  state.ready = !failed;
  const button = section.querySelector('[data-social-post-load]');
  const actions = section.querySelector('.social-post-actions');
  const fallback = section.querySelector('[data-social-post-fallback]');
  const focused = actions.contains(document.activeElement) || fallback?.contains(document.activeElement) || document.activeElement === state.frame;
  if (fallback) fallback.hidden = !failed;
  section.querySelector('[data-social-post]').classList.remove('social-post-pending');
  button.disabled = false;
  button.hidden = !failed;
  actions.hidden = !failed;
  button.textContent = (failed ? 'Retry ' : 'Load ') + state.provider.name + ' post';
  section.querySelector('[data-social-post-status]').textContent = failed ? 'The ' + state.provider.name + ' post could not load. Try again or open it on ' + state.provider.name + '.' : '';
  if (failed) {
    state.frame?.remove();
    const host = section.querySelector('[data-social-post]');
    sizes.unobserve(host);
    host.style.height = '';
    active.delete(section);
  }
  if (focused) (failed ? button : state.frame).focus({preventScroll: true});
}

async function load(section, moveFocus = false) {
  if (!section.isConnected) return;
  const previous = active.get(section);
  if (previous) {
    if (!previous.preview || previous.ready) return;
    reset(section);
    initialized.add(section);
  }
  const button = section.querySelector('[data-social-post-load]');
  const host = section.querySelector('[data-social-post]');
  const fallback = section.querySelector('[data-social-post-fallback]');
  const provider = providers[section.dataset.socialProvider];
  const id = [...crypto.getRandomValues(new Uint8Array(16))].map(byte => byte.toString(16).padStart(2, '0')).join('');
  const state = {provider, controller: new AbortController(), id, width: Math.max(provider.minWidth, Math.floor(host.getBoundingClientRect().width)), ready: false};
  active.set(section, state);
  button.disabled = true;
  section.querySelector('[data-social-post-status]').textContent = 'Loading ' + state.provider.name + ' post…';
  host.classList.toggle('social-post-pending', Boolean(fallback));
  state.timer = setTimeout(() => finish(section, true), 15000);
  try {
    const src = await frameURL(section, state);
    if (active.get(section) !== state || !section.isConnected) return;
    const frame = document.createElement('iframe');
    state.frame = frame;
    frame.className = 'social-post-frame';
    frame.title = 'Embedded ' + state.provider.name + ' post';
    frame.setAttribute('scrolling', 'no');
    frame.setAttribute('sandbox', 'allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox');
    frame.setAttribute('allow', "autoplay 'none'; fullscreen; picture-in-picture");
    frame.referrerPolicy = 'no-referrer';
    frame.src = src;
    if (provider.fixedHeight) {
      state.height = provider.fixedHeight;
      frame.style.height = state.height + 'px';
      frame.setAttribute('scrolling', 'auto');
    }
    if (provider.fixedHeight || provider.portrait) {
      frame.addEventListener('load', () => {
        if (active.get(section) !== state || state.ready) return;
        // LinkedIn cannot confirm rendering to another site. TikTok confirms
        // readiness after Play. Keep recovery controls on a loaded preview.
        state.preview = true;
        clearTimeout(state.timer);
        host.classList.remove('social-post-pending');
        const focused = fallback?.contains(document.activeElement);
        if (fallback) fallback.hidden = true;
        button.disabled = button.hidden = false;
        button.textContent = 'Retry ' + provider.name + ' post';
        section.querySelector('[data-social-post-status]').textContent = '';
        if (focused) frame.focus({preventScroll: true});
      });
    }
    frame.addEventListener('error', () => { if (active.get(section) === state) finish(section, true); });
    host.replaceChildren(frame);
    fit(section, state);
    sizes.observe(host);
    if (moveFocus) (fallback || frame).focus({preventScroll: true});
  } catch {
    if (active.get(section) === state) finish(section, true);
  }
}

document.addEventListener('click', event => {
  const button = event.target.closest('[data-social-post-load]');
  if (button) load(button.closest('.social-post-component'), document.activeElement === button);
});

window.addEventListener('message', event => {
  for (const [section, state] of active) {
    if (!state.frame || event.origin !== state.provider.origin || event.source !== state.frame.contentWindow) continue;
    let height, message = event.data;
    if (typeof message === 'string' && message.startsWith('{')) {
      try { message = JSON.parse(message); } catch { continue; }
    }
    switch (section.dataset.socialProvider) {
      case 'bluesky-post':
        if (event.data?.id === state.id) height = event.data.height;
        break;
      case 'threads-post':
        // Threads' own widget receives a numeric height from the post iframe.
        if (typeof event.data === 'number' || typeof event.data === 'string' && /^\d+(\.\d+)?$/.test(event.data)) height = Number(event.data);
        break;
      case 'facebook-post': {
        if (typeof event.data !== 'string') break;
        let message;
        try { message = event.data.startsWith('{') ? JSON.parse(event.data) : Object.fromEntries(new URLSearchParams(event.data)); } catch { break; }
        if (message.cb !== state.id) break;
        if (message.type === 'error') { finish(section, true); return; }
        if (['resize', 'resize.iframe', 'resize.flow'].includes(message.type) && /^(\d+)(\.\d+)?$/.test(String(message.height))) height = Number(message.height);
        break;
      }
      case 'reddit-post':
        if (message?.type === 'resize.embed') height = message.data;
        break;
      case 'instagram-post':
        if (message?.type === 'MEASURE') height = message.details?.height;
        break;
      case 'mastodon-post':
        if (message?.type === 'mastodon:resize' && message.id === state.id) height = message.height;
        break;
      case 'tiktok-post':
        if (message?.['x-tiktok-player'] === true) {
          if (['onPlayerError', 'onError'].includes(message.type)) { finish(section, true); return; }
          if (message.type === 'onPlayerReady') height = state.height;
        }
        break;
    }
    if (!Number.isFinite(height) || height < 1) return;
    state.height = Math.min(4096, Math.max(100, Math.ceil(height) + 1));
    state.frame.style.height = state.height + 'px';
    fit(section, state);
    state.frame.setAttribute('scrolling', height >= 4096 ? 'auto' : 'no');
    // A height report can describe a post or the provider's availability notice.
    if (!state.ready) finish(section);
    return;
  }
});

function reset(section) {
  const state = active.get(section);
  if (state) { clearTimeout(state.timer); state.controller.abort(); active.delete(section); }
  initialized.delete(section);
  section.querySelector('.social-post-frame')?.remove();
  const host = section.querySelector('[data-social-post]');
  if (host) {
    sizes.unobserve(host);
    host.style.height = '';
    host.classList.remove('social-post-pending');
  }
  const fallback = section.querySelector('[data-social-post-fallback]');
  if (fallback) fallback.hidden = false;
  const button = section.querySelector('[data-social-post-load]');
  if (!button) return;
  button.hidden = true;
  button.disabled = false;
  button.textContent = 'Load ' + providers[section.dataset.socialProvider].name + ' post';
  section.querySelector('.social-post-actions').hidden = false;
  section.querySelector('[data-social-post-status]').textContent = '';
}

document.addEventListener('htmx:before:cleanup', event => {
  const sections = [...event.target.querySelectorAll?.('.social-post-component') || []];
  if (event.target.matches?.('.social-post-component')) sections.push(event.target);
  for (const section of sections) { observer?.unobserve(section); reset(section); }
});
window.addEventListener('pagehide', () => {
  observer?.disconnect();
  document.querySelectorAll('.social-post-component').forEach(reset);
});
window.addEventListener('pageshow', () => initialize(document));
document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
