import {enableAlbumGestures} from './album-gestures.js';

// Real image links remain usable without JavaScript. The native dialog traps
// focus. The viewer owns presentation only, not album content or persistence.
(() => {
  let active;
  let activeAlbum;
  document.addEventListener('click', event => {
    const opener = event.target.closest('[data-album-photo]');
    if (!opener || event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
    const album = opener.closest('.photo-album, [data-album-editor]');
    if (!album || typeof HTMLDialogElement === 'undefined') return;
    event.preventDefault();
    active?.close();
    const scroll = {x: scrollX, y: scrollY};
    const photos = [...album.querySelectorAll('[data-album-photo]')];
    let index = photos.indexOf(opener);
    let captionsVisible = true;
    let captionExpanded = false;
    let thumbnailsVisible = !matchMedia('(max-width: 600px)').matches;
    let request = 0;
    let pending;
    let neighbors = [];
    let loading = true;
    let loadingTimer;
    const dialog = document.querySelector('#album-viewer-template').content.firstElementChild.cloneNode(true);
    const count = dialog.querySelector('header p');
    const zoom = dialog.querySelector('[data-album-zoom]');
    const captionToggle = dialog.querySelector('[data-album-captions]');
    const thumbnailsToggle = dialog.querySelector('[data-album-thumbnails]');
    thumbnailsToggle.hidden = photos.length < 2;
    dialog.querySelector('[data-album-close]').addEventListener('click', () => dialog.close());
    function label(button, text) { button.setAttribute('aria-label', text); button.title = text; }
    const figure = dialog.querySelector('figure');
    figure.classList.toggle('album-viewer-with-captions', photos.some(photo => photo.dataset.caption));
    const frame = dialog.querySelector('.album-viewer-image-frame');
    const image = dialog.querySelector('[data-album-image]');
    const caption = dialog.querySelector('figcaption');
    const captionText = caption.querySelector('.album-viewer-caption-text');
    const moreCaption = caption.querySelector('[data-album-caption-more]');
    moreCaption.remove();
    function setCaptionExpanded(expanded, focus = false) {
      captionExpanded = expanded;
      caption.classList.toggle('album-viewer-caption-expanded', expanded);
      captionText.tabIndex = expanded ? 0 : -1;
      moreCaption.setAttribute('aria-expanded', String(expanded));
      moreCaption.textContent = expanded ? 'Show less' : 'Read full caption';
      positionCaption();
      if (focus) (expanded ? captionText : moreCaption.isConnected ? moreCaption : caption).focus({preventScroll: true});
    }
    moreCaption.addEventListener('click', () => setCaptionExpanded(!captionExpanded, true));
    const status = dialog.querySelector('.album-viewer-status');
    function positionCaption() {
      if (!image.naturalWidth || !image.naturalHeight) return;
      const box = image.getBoundingClientRect();
      if (!box.width || !box.height) return;
      const viewport = frame.getBoundingClientRect();
      const container = figure.getBoundingClientRect();
      const ratio = image.naturalWidth / image.naturalHeight;
      const width = Math.min(box.width, box.height * ratio);
      const height = width / ratio;
      const left = Math.max(viewport.left, box.left + (box.width - width) / 2);
      const right = Math.min(viewport.right, box.left + (box.width + width) / 2);
      const bottom = Math.min(viewport.bottom, box.top + (box.height + height) / 2);
      caption.style.left = left - container.left + 'px';
      caption.style.top = bottom - container.top + 8 + 'px';
      caption.style.width = Math.max(0, right - left) + 'px';
      caption.classList.remove('album-viewer-caption-truncated');
      const available = parseFloat(getComputedStyle(caption).maxHeight);
      const truncated = !caption.hidden && (captionExpanded || captionText.scrollHeight > available + 1);
      caption.classList.toggle('album-viewer-caption-truncated', truncated);
      if (truncated && !moreCaption.isConnected) caption.append(moreCaption);
      if (!truncated && moreCaption.isConnected) {
        if (document.activeElement === moreCaption) caption.focus({preventScroll: true});
        moreCaption.remove();
      }
      if (captionExpanded) {
        const top = Math.min(bottom + 8, container.bottom - 8 - caption.getBoundingClientRect().height);
        caption.style.top = Math.max(8, top - container.top) + 'px';
      }
    }
    const captionLayout = new ResizeObserver(positionCaption);
    captionLayout.observe(frame);
    image.addEventListener('load', positionCaption);
    const navigation = dialog.querySelector('.album-viewer-thumbnails');
    const thumbnails = photos.map((photo, position) => {
      const button = document.createElement('button');
      button.type = 'button';
      button.dataset.albumThumbnail = '';
      button.setAttribute('aria-label', `Photo ${position + 1}${photo.dataset.caption ? ': ' + photo.dataset.caption : ''}`);
      const preview = document.createElement('img');
      preview.src = photo.querySelector('img').src;
      preview.alt = '';
      preview.loading = 'lazy';
      const number = document.createElement('span');
      number.textContent = position + 1;
      button.append(preview, number);
      button.addEventListener('click', () => show(position));
      navigation.append(button);
      return button;
    });
    function scrollThumbnail() {
      if (dialog.open && !navigation.hidden) thumbnails[index].scrollIntoView({block: 'nearest', inline: 'nearest'});
    }
    function presentation() {
      caption.hidden = image.hidden || !captionsVisible || !captionText.textContent;
      captionToggle.disabled = image.hidden || !captionText.textContent;
      captionToggle.setAttribute('aria-pressed', String(captionsVisible && Boolean(captionText.textContent)));
      label(captionToggle, !captionText.textContent ? 'No caption for this photo' : captionsVisible ? 'Hide captions' : 'Show captions');
      navigation.hidden = !thumbnailsVisible || photos.length < 2;
      thumbnailsToggle.setAttribute('aria-pressed', String(thumbnailsVisible));
      label(thumbnailsToggle, thumbnailsVisible ? 'Hide thumbnails' : 'Show thumbnails');
      scrollThumbnail();
      positionCaption();
    }
    const gestures = enableAlbumGestures(frame, image, {
      enabled: () => !loading,
      navigate: direction => show(index + direction),
      changed: scale => {
        zoom.setAttribute('aria-pressed', String(scale > 1));
        label(zoom, scale > 1 ? 'Reset zoom' : 'Zoom in');
        positionCaption();
      }
    });
    image.addEventListener('load', gestures.refresh);
    zoom.addEventListener('click', () => gestures.zoom(gestures.scale > 1 ? 1 : 2));
    captionToggle.addEventListener('click', () => { captionsVisible = !captionsVisible; if (!captionsVisible) setCaptionExpanded(false); presentation(); });
    thumbnailsToggle.addEventListener('click', () => { thumbnailsVisible = !thumbnailsVisible; presentation(); });
    function clearLoadingMessage() {
      clearTimeout(loadingTimer);
      status.textContent = '';
    }
    function failed() {
      clearLoadingMessage();
      gestures.reset();
      loading = false;
      image.hidden = true;
      caption.hidden = true;
      captionToggle.disabled = true;
      count.textContent = `Photo ${index + 1} of ${photos.length}`;
      thumbnails.forEach((button, position) => {
        button.tabIndex = position === index ? 0 : -1;
        if (position === index) button.setAttribute('aria-current', 'true');
        else button.removeAttribute('aria-current');
      });
      scrollThumbnail();
      zoom.disabled = true;
      frame.removeAttribute('aria-busy');
      status.textContent = 'Unable to load this photo.';
      const retry = document.createElement('button');
      retry.type = 'button';
      retry.className = 'album-viewer-retry';
      retry.dataset.albumRetry = '';
      retry.textContent = 'Retry';
      retry.addEventListener('click', () => { frame.focus({preventScroll: true}); show(index); });
      status.append(retry);
    }
    image.addEventListener('error', failed);
    function show(next) {
      index = (next + photos.length) % photos.length;
      const selected = photos[index];
      if (image.hidden) {
        count.textContent = `Photo ${index + 1} of ${photos.length}`;
        caption.hidden = true;
        captionToggle.disabled = true;
      }
      clearLoadingMessage();
      frame.setAttribute('aria-busy', 'true');
      loading = true;
      zoom.disabled = true;
      const currentRequest = ++request;
      // Keep quick image changes quiet. Slow loads still need visible feedback.
      loadingTimer = setTimeout(() => {
        if (currentRequest === request && dialog.open && loading) status.textContent = 'Loading photo…';
      }, 300);
      if (pending) pending.onload = pending.onerror = null;
      pending = new Image();
      pending.onload = () => {
        if (currentRequest !== request || !dialog.open) return;
        gestures.reset();
        image.alt = selected.dataset.caption || `Photo ${index + 1}`;
        image.src = selected.href;
        image.hidden = false;
        setCaptionExpanded(false);
        captionText.textContent = selected.dataset.caption;
        captionText.scrollTop = 0;
        count.textContent = `Photo ${index + 1} of ${photos.length}`;
        thumbnails.forEach((button, position) => {
          button.tabIndex = position === index ? 0 : -1;
          if (position === index) button.setAttribute('aria-current', 'true');
          else button.removeAttribute('aria-current');
        });
        presentation();
        loading = false;
        zoom.disabled = false;
        clearLoadingMessage();
        frame.removeAttribute('aria-busy');
        // Only adjacent photos are prefetched. The browser owns its image cache.
        neighbors = photos.length > 1 ? [...new Set([(index + 1) % photos.length, (index - 1 + photos.length) % photos.length])].map(position => {
          const neighbor = new Image();
          neighbor.src = photos[position].href;
          return neighbor;
        }) : [];
      };
      pending.onerror = () => { if (currentRequest === request && dialog.open) failed(); };
      pending.src = selected.href;
    }
    for (const [direction, selector] of [[-1, '[data-album-prev]'], [1, '[data-album-next]']]) {
      const button = dialog.querySelector(selector);
      button.hidden = photos.length < 2;
      button.addEventListener('click', () => show(index + direction));
    }
    dialog.addEventListener('keydown', event => {
      if (event.ctrlKey || event.metaKey || event.altKey || event.target.matches('input, textarea, select')) return;
      if (event.key === 'Escape') {
        event.preventDefault();
        if (captionExpanded) setCaptionExpanded(false, true);
        else if (gestures.scale > 1) gestures.reset();
        else dialog.close();
        return;
      }
      if (['+', '=', '-', '0'].includes(event.key) && !loading && !image.hidden) {
        event.preventDefault();
        gestures.zoom(event.key === '0' ? 1 : gestures.scale * (event.key === '-' ? 1 / 1.5 : 1.5));
        return;
      }
      if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(event.key)) return;
      if (event.target.closest('[data-album-thumbnail]') && ['ArrowUp', 'ArrowDown'].includes(event.key)) return;
      if (gestures.scale === 1 && ['ArrowUp', 'ArrowDown'].includes(event.key)) return;
      if (event.target.closest('figcaption') && ['ArrowUp', 'ArrowDown', 'Home', 'End'].includes(event.key)) return;
      event.preventDefault();
      if (gestures.scale > 1 && event.key.startsWith('Arrow') && !event.target.closest('[data-album-thumbnail]')) {
        gestures.pan(event.key === 'ArrowLeft' ? 60 : event.key === 'ArrowRight' ? -60 : 0, event.key === 'ArrowUp' ? 60 : event.key === 'ArrowDown' ? -60 : 0);
      } else {
        show(event.key === 'Home' ? 0 : event.key === 'End' ? photos.length - 1 : index + (event.key === 'ArrowRight' ? 1 : -1));
        if (event.target.closest('[data-album-thumbnail]')) thumbnails[index].focus({preventScroll: true});
      }
    });
    dialog.addEventListener('cancel', event => { event.preventDefault(); dialog.close(); });
    dialog.addEventListener('close', () => {
      ++request;
      clearLoadingMessage();
      if (pending) pending.onload = pending.onerror = null;
      neighbors = [];
      gestures.destroy();
      captionLayout.disconnect();
      dialog.remove();
      if (active === dialog) {
        active = null;
        activeAlbum = null;
        if (opener.isConnected) {
          opener.focus({preventScroll: true});
          window.scrollTo(scroll.x, scroll.y);
        }
      }
    }, {once: true});
    document.body.append(dialog);
    active = dialog;
    activeAlbum = album;
    dialog.showModal();
    presentation();
    show(index);
  });
  function navigationTarget(event) {
    const candidate = event.detail.ctx.target;
    return typeof candidate === 'string' ? document.querySelector(candidate) : candidate;
  }
  document.addEventListener('htmx:before:cleanup', event => {
    if (active && (event.target === activeAlbum || event.target.contains?.(activeAlbum))) active.close();
  });
  document.addEventListener('htmx:before:swap', event => {
    if (active && navigationTarget(event)?.contains(activeAlbum)) active.close();
  });
  document.addEventListener('htmx:after:swap', () => {
    if (active && !activeAlbum.isConnected) active.close();
  });
})();
