function thumbnailURL(src) {
  if (!src.startsWith('/') && !/^https?:\/\//i.test(src)) return '';
  let url;
  try { url = new URL(src, location.href); } catch { return ''; }
  if (url.origin !== location.origin || !/\.(?:avif|gif|jpe?g|png|svg|webp)$/i.test(url.pathname)) return '';
  if (url.pathname.startsWith('/images/')) url.searchParams.set('variant', 'thumbnail');
  return url.href;
}

function markdownImages(html, source) {
  const markup = document.createElement('template');
  markup.innerHTML = html;
  const bytes = new TextEncoder().encode(source);
  const decoder = new TextDecoder('utf-8', {ignoreBOM: true});
  const images = [];
  let previousByte = 0;
  let offset = 0;
  for (const image of markup.content.querySelectorAll('img[data-markdown-image]:not([data-markdown-image=""])')) {
    const byte = Number(image.dataset.markdownImage);
    const src = image.getAttribute('src');
    if (!src || byte < previousByte || byte >= bytes.length) continue;
    offset += decoder.decode(bytes.subarray(previousByte, byte)).length;
    previousByte = byte;
    if (!source.startsWith('![', offset)) continue;
    const preset = image.dataset.markdownImageSize || '';
    const start = preset ? source.lastIndexOf('{size=', offset) : offset;
    const limit = axis => image.style.getPropertyValue('--markdown-image-max-' + axis).replace(/px$/, '');
    images.push({name: image.alt, thumbnail: thumbnailURL(src), start, offset, preset, width: limit('width'), height: limit('height')});
  }
  return images;
}

function markImage(cm, button, start, end) {
  const from = cm.posFromIndex(start);
  return start === end ? cm.setBookmark(from, {widget: button}) : cm.markText(from, cm.posFromIndex(end), {replacedWith: button});
}

function openImageSize(cm, template, widget, source) {
  const {button, image} = widget;
  const dialog = template.content.firstElementChild.cloneNode(true);
  const form = dialog.querySelector('form');
  const status = dialog.querySelector('[data-image-size-status]');
  const name = dialog.querySelector('[data-image-size-name]');
  const apply = dialog.querySelector('[type="submit"]');
  const dimensions = dialog.querySelector('[data-image-dimensions]');
  const {preset, width, height} = form.elements;
  const updateDimensions = () => {
    dimensions.hidden = preset.value !== 'custom';
    width.disabled = height.disabled = dimensions.hidden;
    width.setCustomValidity('');
  };
  name.textContent = name.title = image.name;
  name.hidden = !image.name;
  preset.value = image.preset;
  width.value = image.width;
  height.value = image.height;
  updateDimensions();
  if (cm.getValue() !== source) {
    status.hidden = false;
    status.textContent = 'Could not refresh image settings. Close and try again.';
    apply.disabled = true;
  }

  for (const radio of preset) radio.addEventListener('change', updateDimensions);
  dimensions.addEventListener('input', () => width.setCustomValidity(''));
  dialog.querySelector('[data-image-size-cancel]').addEventListener('click', () => dialog.close());
  dialog.addEventListener('close', () => {
    dialog.remove();
    if (button.isConnected) button.focus({preventScroll: true});
    else if (cm.getWrapperElement().isConnected) cm.focus();
  });
  form.addEventListener('submit', event => {
    event.preventDefault();
    if (cm.getValue() !== source) {
      status.hidden = false;
      status.textContent = 'Content changed while this dialog was open. Reopen Image size to continue.';
      apply.disabled = true;
      return;
    }
    const {start, offset} = image;
    let marker = preset.value ? '{size=' + preset.value + '}' : '';
    if (preset.value === 'custom') {
      if (!width.value && !height.value) {
        width.setCustomValidity('Enter a maximum width or height.');
        width.reportValidity();
        return;
      }
      const maxWidth = width.value ? ' max-width=' + width.valueAsNumber : '';
      const maxHeight = height.value ? ' max-height=' + height.valueAsNumber : '';
      marker = '{size=custom' + maxWidth + maxHeight + '}';
    }
    if (source.slice(start, offset) !== marker) {
      cm.operation(() => {
        widget.mark.clear();
        cm.replaceRange(marker, cm.posFromIndex(start), cm.posFromIndex(offset), 'image-size');
        widget.mark = markImage(cm, button, start, start + marker.length);
      });
    }
    dialog.close();
  });
  document.body.append(dialog);
  dialog.showModal();
  form.querySelector('input:checked').focus();
  return dialog;
}

export function addMarkdownImageSettings(cm, template) {
  const labels = new Map([...template.content.querySelectorAll('input[name="preset"]')].map(input =>
    [input.value, input.nextElementSibling.textContent]));
  let source;
  let widgets = [];
  let timer;
  let request;
  let dialog;

  // Reuse the thumbnail at each tracked range so nearby edits preserve focus.
  function update(next, images) {
    source = next;
    const existing = new Map();
    cm.operation(() => {
      for (const widget of widgets) {
        const range = widget.mark.find();
        const position = range?.from ?? range;
        const offset = position ? cm.indexFromPos(position) : -1;
        if (existing.has(offset)) widget.mark.clear();
        else existing.set(offset, widget);
      }
      widgets = images.map(image => {
        let widget = existing.get(image.start);
        if (widget) {
          existing.delete(image.start);
          const range = widget.mark.find();
          if (cm.indexFromPos(range.to ?? range) !== image.offset) {
            widget.mark.clear();
            widget.mark = markImage(cm, widget.button, image.start, image.offset);
          }
        } else {
          const button = document.createElement('button');
          button.type = 'button';
          button.className = 'markdown-image-size-button';
          button.setAttribute('aria-haspopup', 'dialog');
          const thumbnail = document.createElement('img');
          thumbnail.alt = '';
          thumbnail.hidden = true;
          thumbnail.loading = 'lazy';
          thumbnail.addEventListener('error', () => { thumbnail.hidden = true; });
          button.append(thumbnail);
          widget = {button, thumbnail, mark: markImage(cm, button, image.start, image.offset)};
          button.addEventListener('click', async () => {
            if (dialog?.open) return;
            const next = cm.getValue();
            if (source !== next) {
              clearTimeout(timer);
              await read(next);
            }
            if (!dialog?.open && !cm.getOption('readOnly') && cm.getValue() === next && widgets.includes(widget)) {
              dialog = openImageSize(cm, template, widget, source);
            }
          });
        }
        widget.image = image;
        const {thumbnail} = widget;
        if ((thumbnail.getAttribute('src') || '') !== image.thumbnail) {
          thumbnail.hidden = !image.thumbnail;
          if (image.thumbnail) thumbnail.src = image.thumbnail;
          else thumbnail.removeAttribute('src');
        }
        widget.button.toggleAttribute('data-image-sized', Boolean(image.preset));
        const limits = [image.width && 'Max width ' + image.width + ' px', image.height && 'Max height ' + image.height + ' px'].filter(Boolean);
        widget.button.title = 'Image size · ' + (limits.join(', ') || labels.get(image.preset));
        widget.button.setAttribute('aria-label', image.name ? 'Image size: ' + image.name : 'Image size');
        return widget;
      });
      for (const {mark} of existing.values()) mark.clear();
    });
  }

  function read(next) {
    if (request?.source === next) return request.result;
    const controller = new AbortController();
    const result = fetch('/post/preview', {
      method: 'POST', body: new URLSearchParams({content: next, imageSettings: ''}), signal: controller.signal
    }).then(async response => {
      if (!response.ok) return;
      const html = await response.text();
      if (!controller.signal.aborted) update(next, markdownImages(html, next));
    }).catch(() => {
      // An edit or another click can retry. Stale offsets cannot be used.
    }).finally(() => {
      if (request?.controller === controller) request = undefined;
    });
    request = {source: next, controller, result};
    return result;
  }

  function refresh() {
    clearTimeout(timer);
    request?.controller.abort();
    request = undefined;
    const next = cm.getValue();
    if (next.includes('![')) timer = setTimeout(() => read(next), 160);
    else update(next, []);
  }

  // Treat the folded marker as part of the image at the source boundary.
  function keepImageSizeTogether(_, change) {
    const {from, to, text} = change;
    if (!change.update || !cm.getLine(from.line).startsWith('![', from.ch)) return;
    for (const {mark} of widgets) {
      const range = mark.find();
      if (range?.to?.line !== from.line || range.to.ch !== from.ch) continue;
      if (to.line === from.line && to.ch === from.ch) {
        change.update(range.from, range.from);
      } else {
        const following = text.length === 1 ? cm.getLine(to.line).slice(to.ch, to.ch + 2) : '\n';
        if (!(text[0] + following).startsWith('![')) change.update(range.from, to);
      }
      break;
    }
  }

  cm.on('beforeChange', keepImageSizeTogether);
  cm.on('change', refresh);
  refresh();
  return () => {
    cm.off('beforeChange', keepImageSizeTogether);
    cm.off('change', refresh);
    clearTimeout(timer);
    request?.controller.abort();
    dialog?.close();
    update(undefined, []);
  };
}
