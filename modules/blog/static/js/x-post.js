const pending = new Map();
const initialized = new WeakSet();
const observer = typeof IntersectionObserver === 'function' ? new IntersectionObserver(entries => {
  for (const entry of entries) {
    if (!entry.isIntersecting) continue;
    observer.unobserve(entry.target);
    load(entry.target);
  }
}) : null;

function initialize(root) {
  for (const section of root.querySelectorAll('.x-post-component')) {
    if (initialized.has(section) || !section.querySelector('[data-x-post]')) continue;
    reset(section);
    initialized.add(section);
    if (observer) observer.observe(section);
    else section.querySelector('[data-x-post-load]').hidden = false;
  }
}

function finish(frame, failed = false) {
  clearTimeout(pending.get(frame));
  pending.delete(frame);
  const section = frame.closest('.x-post-component');
  if (!section) return;
  const button = section.querySelector('[data-x-post-load]');
  const actions = section.querySelector('.x-post-actions');
  const fallback = section.querySelector('[data-x-post-fallback]');
  const focused = actions.contains(document.activeElement) || fallback?.contains(document.activeElement) || document.activeElement === frame;
  if (fallback) fallback.hidden = !failed;
  section.querySelector('[data-x-post]').classList.remove('x-post-pending');
  button.disabled = false;
  button.hidden = !failed;
  actions.hidden = !failed;
  button.textContent = failed ? 'Retry X post' : 'Load X post';
  section.querySelector('[data-x-post-status]').textContent = failed ? 'The X post could not load. Try again or open it on X.' : '';
  if (failed) frame.remove();
  if (focused) (failed ? button : frame).focus({preventScroll: true});
}

function load(section, moveFocus = false) {
  const button = section.querySelector('[data-x-post-load]');
  if (!section.isConnected || button.disabled || section.querySelector('.x-post-frame')) return;
  const host = section.querySelector('[data-x-post]');
  const fallback = section.querySelector('[data-x-post-fallback]');
  const frame = document.createElement('iframe');
  frame.className = 'x-post-frame';
  frame.title = 'Embedded X post';
  // X forces a scrollbar even when its content fits the resized frame.
  frame.setAttribute('scrolling', 'no');
  frame.setAttribute('sandbox', 'allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox');
  frame.setAttribute('allow', "autoplay 'none'; fullscreen; picture-in-picture");
  frame.referrerPolicy = 'no-referrer';
  frame.src = 'https://platform.twitter.com/embed/Tweet.html?' + new URLSearchParams({id: host.dataset.xPost, dnt: 'true', hideThread: 'true', theme: 'light', lang: 'en'});
  button.disabled = true;
  section.querySelector('[data-x-post-status]').textContent = 'Loading X post…';
  host.classList.toggle('x-post-pending', Boolean(fallback));
  host.replaceChildren(frame);
  if (moveFocus) (fallback || frame).focus({preventScroll: true});
  pending.set(frame, setTimeout(() => finish(frame, true), 15000));
}

document.addEventListener('click', event => {
  const button = event.target.closest('[data-x-post-load]');
  if (button) load(button.closest('.x-post-component'), document.activeElement === button);
});

window.addEventListener('message', event => {
  if (event.origin !== 'https://platform.twitter.com') return;
  let data = event.data;
  if (typeof data === 'string') { try { data = JSON.parse(data); } catch { return; } }
  const message = data?.['twttr.embed'];
  if (!message || message.jsonrpc !== '2.0') return;
  const frame = [...document.querySelectorAll('.x-post-frame')].find(candidate => candidate.contentWindow === event.source);
  if (!frame) return;
  if (message.method === 'twttr.private.no_results') { finish(frame, true); return; }
  // X can resize an error or loading view without rendering the post.
  if (message.method === 'twttr.private.rendered') { finish(frame); return; }
  if (message.method !== 'twttr.private.resize') return;
  const height = message.params?.[0]?.height;
  if (!Number.isFinite(height) || height < 1) return;
  // X rounds its height while fonts and borders can occupy fractional pixels.
  frame.style.height = Math.min(4096, Math.max(100, Math.ceil(height) + 1)) + 'px';
  frame.setAttribute('scrolling', height >= 4096 ? 'auto' : 'no');
});

document.addEventListener('htmx:before:cleanup', event => {
  if (event.target.matches?.('.x-post-component')) observer?.unobserve(event.target);
  for (const section of event.target.querySelectorAll?.('.x-post-component') || []) observer?.unobserve(section);
  for (const [frame, timer] of pending) {
    if (event.target.contains?.(frame)) { clearTimeout(timer); pending.delete(frame); }
  }
});
// Reset live provider frames before the browser caches this document.
function reset(section) {
  const button = section.querySelector('[data-x-post-load]');
  if (!button) return;
  initialized.delete(section);
  const frame = section.querySelector('.x-post-frame');
  clearTimeout(pending.get(frame));
  pending.delete(frame);
  frame?.remove();
  section.querySelector('[data-x-post]').classList.remove('x-post-pending');
  const fallback = section.querySelector('[data-x-post-fallback]');
  if (fallback) fallback.hidden = false;
  button.hidden = true;
  button.disabled = false;
  button.textContent = 'Load X post';
  section.querySelector('.x-post-actions').hidden = false;
  section.querySelector('[data-x-post-status]').textContent = '';
}
window.addEventListener('pagehide', () => {
  observer?.disconnect();
  document.querySelectorAll('.x-post-component').forEach(reset);
});
window.addEventListener('pageshow', () => initialize(document));
document.addEventListener('htmx:after:process', event => initialize(event.target.parentElement || event.target));
initialize(document);
