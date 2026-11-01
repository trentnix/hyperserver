(() => {
  const notices = new WeakMap();
  function requestOwner(detail) {
    // beforeSwap's elt may be the swap target, not the submitting element.
    const source = detail.requestConfig?.elt || detail.elt;
    // Programmatic requests use body. Keep ownership on the original target,
    // even when HX-Retarget changes where the response is displayed.
    return source === document.body ? detail.requestConfig?.target || detail.target || source : source;
  }

  function showError(detail, message) {
    const source = detail.requestConfig?.elt || detail.elt;
    const target = detail.target;
    const container = source?.closest('form, dialog[open]') ||
      target?.closest('form, dialog[open]') || target?.querySelector('form') ||
      document.querySelector('[data-request-errors]') || document.body;
    const owner = requestOwner(detail);
    let notice = notices.get(owner);
    if (!notice?.isConnected) {
      notice = document.createElement('p');
      notice.dataset.requestError = '';
      notice.setAttribute('role', 'alert');
      container.prepend(notice);
      notices.set(owner, notice);
    }
    notice.textContent = message;
  }

  // Clear only this source's alert when a new request is actually sent, not when
  // a request is canceled by validation or confirmation. Failed retries add it back.
  document.addEventListener('htmx:beforeSend', event => {
    const owner = requestOwner(event.detail);
    notices.get(owner)?.remove();
    notices.delete(owner);
  });

  document.addEventListener('htmx:beforeSwap', event => {
    const {xhr} = event.detail;
    if (xhr.status < 400 || xhr.status > 599) return;
    const type = (xhr.getResponseHeader('Content-Type') || '').split(';')[0].trim().toLowerCase();
    event.detail.shouldSwap = type === 'text/html';
    if (!event.detail.shouldSwap) {
      const message = type === 'text/plain' ? xhr.responseText.trim() : '';
      showError(event.detail, message || `Request failed (HTTP ${xhr.status}). Please try again.`);
    }
  });

  // A lost response does not prove that a write failed. Never retry automatically.
  for (const name of ['htmx:sendError', 'htmx:timeout']) {
    document.addEventListener(name, event => showError(event.detail,
      'No response from the server. If you submitted a change, check whether it went through before trying again.'));
  }
})();
