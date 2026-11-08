import './editor.js';
import './album-editor.js';
import './editor-drafts.js';

// Browser Back can restore a whole document without contacting the server.
// HTMX history already fetches fresh content. Refresh native restored lists too.
window.addEventListener('pageshow', event => {
  if (event.persisted && ['/post/manage', '/pages'].includes(location.pathname)) location.reload();
});

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
document.addEventListener('htmx:before:swap', event => {
  const {ctx} = event.detail;
  const target = typeof ctx.target === 'string' ? document.querySelector(ctx.target) : ctx.target;
  if (ctx.response?.status !== 200 || !['management-results', 'revision-preview'].includes(target?.id)) return;
  let href = document.activeElement?.closest('a')?.getAttribute('href');
  if (target.id === 'management-results') {
    const form = ctx.sourceElement;
    const id = form.action.split('/').pop();
    const rows = [...target.querySelectorAll('[data-content-id]')];
    const index = rows.findIndex(row => row.dataset.contentId === id);
    const next = rows[index + 1] || rows[index - 1];
    href = next?.querySelector('.page-manager-actions a')?.getAttribute('href');
  }
  positions.set(ctx, {href, target: target.id, x: scrollX, y: scrollY});
});
document.addEventListener('htmx:after:swap', event => {
  const position = positions.get(event.detail.ctx);
  if (!position) return;
  positions.delete(event.detail.ctx);
  const links = document.querySelectorAll('#management-results a, .revision-list a');
  const focus = [...links].find(link => link.getAttribute('href') === position.href) ||
    (position.target === 'management-results' ? document.querySelector('.management-search input') || document.querySelector('.page-create') :
      document.querySelector('.revision-list [aria-current]'));
  focus?.focus({preventScroll: true});
  window.scrollTo({left: position.x, top: position.y, behavior: 'instant'});
});
function initialize(root) {
  if (root.matches?.('dialog:not([open])')) root.showModal();
  root.querySelectorAll('dialog:not([open])').forEach(dialog => dialog.showModal());
}
document.addEventListener('htmx:after:process', event => initialize(event.target));
document.addEventListener('click', event => {
  const close = event.target.closest('[data-close-dialog]');
  if (close) close.closest('dialog').close();
});
document.addEventListener('htmx:after:swap', event => {
  const {ctx} = event.detail;
  const target = typeof ctx.target === 'string' ? document.querySelector(ctx.target) : ctx.target;
  if (target?.id !== 'revision-preview') return;
  const selected = target.querySelector('[data-revision]')?.dataset.revision;
  if (!selected) return;
  // Keep long lists in place during selection. Deletions return an updated list.
  for (const link of document.querySelectorAll('.revision-list a')) {
    if (link.dataset.revision === selected) link.setAttribute('aria-current', 'true');
    else link.removeAttribute('aria-current');
  }
});
initialize(document);
