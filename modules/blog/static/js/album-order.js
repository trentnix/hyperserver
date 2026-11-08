import {enableAlbumReordering} from './album-reorder.js';

// Reordering stays provisional until Done. The post editor owns persistence.
export function openAlbumOrder(photos, apply, opener = document.activeElement) {
  if (document.querySelector('.album-order-dialog')) return;
  const ordered = [...photos];
  const grid = matchMedia('(min-width: 700px)');
  const dialog = document.querySelector('#album-order-template').content.firstElementChild.cloneNode(true);
  dialog.style.setProperty('--album-order-width', Math.min(960, photos.length * 320 + 24) + 'px');
  const help = dialog.querySelector('#album-order-instructions');
  const list = dialog.querySelector('.album-order-list');
  const status = dialog.querySelector('.album-order-status');
  let highlighted;
  let highlightTimer;
  let captionOwner;
  const captionDetail = document.querySelector('#album-order-caption-template').content.firstElementChild.cloneNode(true);
  const captionText = captionDetail.querySelector('p');
  captionDetail.querySelector('button').addEventListener('click', () => hideCaption());
  captionDetail.addEventListener('keydown', event => {
    if (event.key !== 'Escape') return;
    event.preventDefault();
    event.stopPropagation();
    hideCaption();
  });
  function hideCaption() {
    if (!captionOwner) return;
    const owner = captionOwner;
    const restoreFocus = captionDetail.contains(document.activeElement);
    for (const child of owner.closest('li').children) child.inert = false;
    owner.setAttribute('aria-expanded', 'false');
    captionDetail.remove();
    captionOwner = null;
    if (restoreFocus) owner.focus({preventScroll: true});
  }
  function showCaption(owner, text) {
    hideCaption();
    captionOwner = owner;
    captionText.textContent = text;
    const row = owner.closest('li');
    row.append(captionDetail);
    for (const child of row.children) {
      if (child !== captionDetail && (!grid.matches || child.matches('.album-order-handle, .album-order-caption'))) child.inert = true;
    }
    owner.setAttribute('aria-expanded', 'true');
    captionText.scrollTop = 0;
    captionText.focus({preventScroll: true});
  }
  dialog.querySelector('[data-album-order-cancel]').addEventListener('click', () => dialog.close());
  const done = dialog.querySelector('[data-album-order-done]');
  done.addEventListener('click', () => {
    ordering.cancel();
    if (ordered.some((photo, index) => photo !== photos[index])) apply(ordered);
    dialog.close();
  });
  function announce(message) { status.textContent = message; }
  function positions() {
    [...list.children].forEach((row, index) => {
      row.dataset.albumIndex = index;
      row.querySelector('[data-album-drag]').setAttribute('aria-label', `Reorder photo ${index + 1}`);
      for (const direction of ['up', 'down']) {
        const button = row.querySelector(`[data-album-move=${direction}]`);
        const movement = grid.matches ? direction === 'up' ? 'back' : 'forward' : direction;
        button.disabled = direction === 'up' ? index === 0 : index === ordered.length - 1;
        button.setAttribute('aria-label', `Move photo ${index + 1} ${movement}`);
        button.title = `Move ${movement}`;
      }
    });
  }
  function move(from, to) {
    if (from === to || from < 0 || to < 0 || from >= ordered.length || to >= ordered.length) return false;
    const row = list.children[from];
    hideCaption();
    const focus = row.contains(document.activeElement) ? document.activeElement : null;
    const target = to > from ? list.children[to].nextSibling : list.children[to];
    ordered.splice(to, 0, ordered.splice(from, 1)[0]);
    list.insertBefore(row, target);
    positions();
    (focus?.disabled ? row.querySelector('[data-album-move]:not(:disabled)') : focus)?.focus({preventScroll: true});
    highlightPhoto(row);
    return true;
  }
  function highlightPhoto(row) {
    clearTimeout(highlightTimer);
    highlighted?.classList.remove('album-photo-moved');
    highlighted = row;
    row.classList.add('album-photo-moved');
    highlightTimer = setTimeout(() => {
      row.classList.remove('album-photo-moved');
      highlighted = null;
    }, 900);
  }
  function updateCaptionControls() {
    if (!dialog.open) return;
    for (const caption of list.querySelectorAll('.album-order-caption')) {
      const preview = caption.querySelector('span');
      const truncated = Boolean(preview && preview.scrollHeight > preview.clientHeight + 1);
      if ((caption.tagName === 'BUTTON') === truncated) continue;
      if (caption === captionOwner) hideCaption();
      const focused = caption === document.activeElement;
      const replacement = document.createElement(truncated ? 'button' : 'p');
      replacement.className = caption.className;
      replacement.id = caption.id;
      replacement.title = caption.title;
      replacement.append(...caption.childNodes);
      if (truncated) {
        replacement.type = 'button';
        replacement.setAttribute('aria-expanded', 'false');
        replacement.setAttribute('aria-controls', captionDetail.id);
        replacement.addEventListener('click', () => showCaption(replacement, replacement.textContent));
      }
      caption.replaceWith(replacement);
      if (focused) (truncated ? replacement : replacement.closest('li').querySelector('[data-album-drag]')).focus({preventScroll: true});
    }
  }
  const ordering = enableAlbumReordering(list, {move, announce, dropped: highlightPhoto, disabled: () => !dialog.open, scrollRoot: list});
  ordered.forEach((photo, index) => {
    const row = document.querySelector('#album-order-photo-template').content.firstElementChild.cloneNode(true);
    const handle = row.querySelector('[data-album-drag]');
    const image = handle.querySelector('img');
    image.src = photo.path + '?variant=thumbnail';
    image.srcset = `${photo.path}?variant=thumbnail 1x, ${photo.path} 2x`;
    const caption = row.querySelector('.album-order-caption');
    caption.id = `album-order-caption-${index}`;
    caption.title = photo.caption;
    if (photo.caption) {
      const preview = document.createElement('span');
      preview.textContent = photo.caption;
      caption.append(preview);
      handle.setAttribute('aria-describedby', `${help.id} ${caption.id}`);
      for (const button of row.querySelectorAll('[data-album-move]')) button.setAttribute('aria-describedby', caption.id);
    }
    list.append(row);
  });
  function moved(row) {
    row.scrollIntoView({block: 'nearest'});
    announce(`Photo moved to position ${Number(row.dataset.albumIndex) + 1} of ${ordered.length}.`);
  }
  list.addEventListener('click', event => {
    const button = event.target.closest('[data-album-move]');
    if (!button || button.disabled || ordering.dragging) return;
    const row = button.closest('[data-album-index]');
    const from = Number(row.dataset.albumIndex);
    if (move(from, from + (button.dataset.albumMove === 'up' ? -1 : 1))) moved(row);
  });
  const events = new AbortController();
  const captionLayout = new ResizeObserver(updateCaptionControls);
  captionLayout.observe(list);
  document.fonts.ready.then(updateCaptionControls);
  dialog.addEventListener('pointerdown', event => {
    if (captionOwner && !captionDetail.contains(event.target)) hideCaption();
  });
  grid.addEventListener('change', positions, {signal: events.signal});
  window.addEventListener('resize', hideCaption, {signal: events.signal});
  document.addEventListener('htmx:before:swap', event => {
    const candidate = event.detail.ctx.target;
    const target = typeof candidate === 'string' ? document.querySelector(candidate) : candidate;
    if (target?.contains(opener)) dialog.close();
  }, {signal: events.signal});
  dialog.addEventListener('close', () => {
    hideCaption();
    ordering.cancel();
    clearTimeout(highlightTimer);
    captionLayout.disconnect();
    events.abort();
    dialog.remove();
    if (opener?.isConnected) opener.focus({preventScroll: true});
  }, {once: true});
  document.body.append(dialog);
  positions();
  dialog.showModal();
  updateCaptionControls();
}
