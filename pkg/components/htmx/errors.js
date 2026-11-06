(() => {
  const notices = new WeakMap();
  const owners = new WeakMap();
  const element = target => typeof target === 'string' ? document.querySelector(target) : target;

  function showError(ctx, message) {
    const source = ctx.sourceElement;
    const target = element(ctx.hx?.retarget || ctx.target);
    const container = source?.closest('form, dialog[open]') ||
      target?.closest('form, dialog[open]') || target?.querySelector('form') ||
      document.querySelector('[data-request-errors]') || document.body;
    const owner = owners.get(ctx) || source;
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

  document.addEventListener('htmx:before:request', event => {
    const {ctx} = event.detail;
    // Retargeting changes placement, not ownership. Programmatic requests use body.
    const owner = ctx.sourceElement === document.body ? element(ctx.target) : ctx.sourceElement;
    owners.set(ctx, owner);
    const send = ctx.fetch;
    ctx.fetch = (...args) => {
      // A canceled request never calls fetch and must retain its existing alert.
      notices.get(owner)?.remove();
      notices.delete(owner);
      return send(...args);
    };
  });

  document.addEventListener('htmx:before:swap', event => {
    const {ctx} = event.detail;
    if (ctx.response?.status >= 400 && ctx.response.status <= 599) {
      const type = (ctx.response.headers.get('Content-Type') || '').split(';')[0].trim().toLowerCase();
      if (type !== 'text/html') {
        event.preventDefault();
        const message = type === 'text/plain' ? ctx.text.trim() : '';
        showError(ctx, message || `Request failed (HTTP ${ctx.response.status}). Please try again.`);
      }
    }
    // Responses contain markup, never executable scripts. Apply to main and OOB swaps.
    for (const task of event.detail.tasks) {
      task.fragment.querySelectorAll('script').forEach(script => script.remove());
    }
  });

  // A lost response does not prove that a write failed. Never retry automatically.
  document.addEventListener('htmx:error', event => {
    const {ctx} = event.detail;
    if (ctx && ctx.text === undefined) showError(ctx,
      'No response from the server. If you submitted a change, check whether it went through before trying again.');
  });
})();
