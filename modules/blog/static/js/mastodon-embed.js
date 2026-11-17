// Relay only this instance's native sizing messages to the containing blog.
const frame = document.querySelector('iframe');
const origin = document.body.dataset.origin;
const callback = document.body.dataset.callback;
const id = crypto.getRandomValues(new Uint32Array(1))[0] || 1;
const measure = () => frame.contentWindow.postMessage({type: 'setHeight', id}, origin);
frame.addEventListener('load', measure);
measure();
let width = 0;
new ResizeObserver(([entry]) => {
  if (!entry.contentRect.width || entry.contentRect.width === width) return;
  width = entry.contentRect.width;
  // The native document can use its viewport as a minimum height. Collapse it
  // before measuring so widening a post can also reduce its height.
  frame.style.height = '100px';
  requestAnimationFrame(measure);
}).observe(frame);
window.addEventListener('message', event => {
  const message = event.data;
  if (event.source !== frame.contentWindow || event.origin !== origin || message?.type !== 'setHeight' || message.id !== id || !Number.isFinite(message.height) || message.height < 1) return;
  const height = Math.min(4096, Math.max(100, Math.ceil(message.height)));
  frame.style.height = height + 'px';
  frame.setAttribute('scrolling', message.height >= 4096 ? 'auto' : 'no');
  parent.postMessage({type: 'mastodon:resize', id: callback, height}, location.origin);
});
