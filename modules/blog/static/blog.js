// Keep unsaved form input when an HTMX request fails. Never insert error HTML.
function showRequestFailure(message) {
  let notice = document.getElementById('request-status');
  if (!notice) {
    notice = document.createElement('p');
    notice.id = 'request-status';
    notice.className = 'notice field-error';
    notice.setAttribute('role', 'alert');
    document.getElementById('main').prepend(notice);
  }
  notice.textContent = message;
  notice.hidden = false;
  notice.scrollIntoView({block: 'nearest'});
}

document.addEventListener('htmx:beforeRequest', () => {
  const notice = document.getElementById('request-status');
  if (notice) notice.hidden = true;
});
document.addEventListener('htmx:responseError', event => {
  showRequestFailure(`The request failed (HTTP ${event.detail.xhr.status}). Your unsaved changes are still here.`);
});
document.addEventListener('htmx:sendError', () => {
  showRequestFailure('The server could not be reached. Your unsaved changes are still here.');
});
document.addEventListener('htmx:timeout', () => {
  showRequestFailure('The request timed out. Your unsaved changes are still here.');
});

// The original layout repeats recent posts below the article on narrow screens.
document.addEventListener('htmx:afterSwap', event => {
  if (event.detail.target.id !== 'posts-list') return;
  const mobileMenu = document.getElementById('content-menu');
  if (mobileMenu) {
    mobileMenu.replaceChildren(...Array.from(event.detail.target.childNodes, node => node.cloneNode(true)));
  }
});
