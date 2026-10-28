// Editor confirmations share the site's styling without replacing the browser's
// mandatory beforeunload warning. Only an explicit action can continue the work.
(() => {
  let pending = false;

  window.blogConfirm = ({title, message, action, cancel = 'Cancel'}, proceed) => {
    if (pending) return;
    pending = true;
    const previousFocus = document.activeElement;
    const dialog = document.createElement('dialog');
    dialog.className = 'editor-confirmation';
    dialog.setAttribute('aria-labelledby', 'confirmation-title');
    dialog.setAttribute('aria-describedby', 'confirmation-message');
    dialog.innerHTML = '<h2 id="confirmation-title"></h2><p id="confirmation-message"></p><div class="form-actions"><button type="button" class="button-secondary" data-confirm-cancel autofocus></button><button type="button" data-confirm-accept></button></div>';
    dialog.querySelector('h2').textContent = title;
    dialog.querySelector('p').textContent = message;
    dialog.querySelector('[data-confirm-cancel]').textContent = cancel;
    dialog.querySelector('[data-confirm-accept]').textContent = action;

    let finished = false;
    function finish(accepted) {
      if (finished) return;
      finished = true;
      dialog.close();
      dialog.remove();
      pending = false;
      if (previousFocus?.isConnected) previousFocus.focus({preventScroll: true});
      if (accepted) proceed();
    }
    dialog.querySelector('[data-confirm-cancel]').addEventListener('click', () => finish(false));
    dialog.querySelector('[data-confirm-accept]').addEventListener('click', () => finish(true));
    dialog.addEventListener('cancel', event => {
      event.preventDefault();
      finish(false);
    });
    dialog.addEventListener('close', () => finish(false));
    document.body.append(dialog);
    dialog.showModal();
  };
})();
