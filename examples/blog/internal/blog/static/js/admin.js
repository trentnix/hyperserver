import './editor.js';

const mobileLayout = matchMedia('(max-width: 767px)');

// Keep one set of admin links: expanded on desktop, a disclosure on mobile.
const accountMenu = document.querySelector('.account-menu');
function positionAccountMenu() {
  if (!accountMenu) return;
  const summary = accountMenu.querySelector('summary');
  const focused = document.activeElement;
  const hadFocus = accountMenu.contains(focused);
  accountMenu.open = !mobileLayout.matches;
  summary.hidden = !mobileLayout.matches;
  if (mobileLayout.matches && hadFocus) summary.focus({preventScroll: true});
  else if (focused === summary) accountMenu.querySelector('a').focus({preventScroll: true});
}
mobileLayout.addEventListener('change', positionAccountMenu);
positionAccountMenu();
if (accountMenu) accountMenu.dataset.ready = '';

// Native disclosure controls work without JavaScript. Add familiar dismissal
// behavior, including for action menus inserted by HTMX navigation.
document.addEventListener('click', event => {
  for (const menu of document.querySelectorAll('.action-menu[open], .post-time-menu[open]')) {
    if (menu.querySelector('summary').hidden) continue;
    if (!menu.contains(event.target) || event.target.closest('a, button')) menu.open = false;
  }
});
document.addEventListener('keydown', event => {
  if (event.key !== 'Escape') return;
  const menu = event.target.closest('.action-menu[open], .post-time-menu[open]');
  if (menu && !menu.querySelector('summary').hidden) {
    menu.open = false;
    menu.querySelector('summary').focus({preventScroll: true});
    event.preventDefault();
  }
});
document.addEventListener('focusin', event => {
  for (const menu of document.querySelectorAll('.post-time-menu[open]')) {
    if (!menu.contains(event.target)) menu.open = false;
  }
});

// Templates own the result HTML. Preserve only the user's place across swaps.
const positions = new WeakMap();
document.addEventListener('htmx:beforeSwap', event => {
  const {xhr, target} = event.detail;
  if (xhr.status !== 200 || !['management-results', 'revision-preview'].includes(target.id)) return;
  let href = document.activeElement?.closest('a')?.getAttribute('href');
  if (target.id === 'management-results') {
    const form = event.detail.requestConfig.elt;
    const id = form.action.split('/').pop();
    const rows = [...target.querySelectorAll('[data-content-id]')];
    const index = rows.findIndex(row => row.dataset.contentId === id);
    const next = rows[index + 1] || rows[index - 1];
    href = next?.querySelector('.page-manager-actions a')?.getAttribute('href');
  }
  positions.set(xhr, {href, x: scrollX, y: scrollY});
});
document.addEventListener('htmx:afterSwap', event => {
  const position = positions.get(event.detail.xhr);
  if (!position) return;
  positions.delete(event.detail.xhr);
  const links = document.querySelectorAll('#management-results a, .revision-list a');
  const focus = [...links].find(link => link.getAttribute('href') === position.href) ||
    (event.target.id === 'management-results' ? document.querySelector('.management-search input') :
      document.querySelector('.revision-list [aria-current]'));
  focus?.focus({preventScroll: true});
  window.scrollTo({left: position.x, top: position.y, behavior: 'instant'});
});
function initialize(root) {
  root.querySelectorAll('dialog:not([open])').forEach(dialog => dialog.showModal());
}
document.addEventListener('click', event => {
  const close = event.target.closest('[data-close-dialog]');
  if (close) close.closest('dialog').close();
});
document.addEventListener('htmx:afterSwap', event => {
  initialize(event.target);
  if (event.target.id !== 'revision-preview') return;
  const selected = event.target.querySelector('[data-revision]')?.dataset.revision;
  if (!selected) return;
  // Keep long lists in place during selection. Deletions return an updated list.
  for (const link of document.querySelectorAll('.revision-list a')) {
    if (link.dataset.revision === selected) link.setAttribute('aria-current', 'true');
    else link.removeAttribute('aria-current');
  }
});
initialize(document);
