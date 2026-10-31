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

// Remove only confirmed deletions. Keep the list, its query, and nearby focus.
document.addEventListener('blog:content-deleted', event => {
  const list = document.querySelector('[data-content-list]');
  const {kind, id} = event.detail;
  if (!list || list.dataset.contentList !== kind) return;
  const rows = [...list.querySelectorAll('[data-content-id]')];
  const index = rows.findIndex(row => row.dataset.contentId === id);
  if (index < 0) return;
  const row = rows[index];
  const next = rows[index + 1] || rows[index - 1];
  const focus = next?.querySelector('.page-manager-actions a') || list.querySelector('input[type=search]');
  const url = row.querySelector('.page-manager-title a').getAttribute('href');
  const section = kind === 'post' ? row.closest('section') : null;
  const searching = list.dataset.searching === 'true';
  const {scrollX, scrollY} = window;
  row.remove();
  for (const group of list.querySelectorAll('.page-manager-list')) {
    if (!group.children.length) group.remove();
  }
  const empty = (container, message) => {
    const p = document.createElement('p');
    p.className = 'page-manager-empty';
    p.textContent = message;
    container.append(p);
  };
  if (section && !section.querySelector('[data-content-id]')) {
    if (section.id === 'published-posts' && !searching) empty(section, 'No published posts yet.');
    else section.remove();
  }
  const remaining = list.querySelectorAll('[data-content-id]').length;
  const count = list.querySelector('[data-result-count]');
  if (count) {
    count.dataset.resultCount = String(remaining);
    count.textContent = `${remaining} ${kind}${remaining === 1 ? '' : 's'} found.`;
  }
  if (!remaining && (searching || kind === 'page')) {
    empty(list, searching ? `No ${kind}s match your search.` : 'No pages yet. Create a page to get started.');
  }
  // Public navigation can contain the same item outside the management list.
  for (const link of document.querySelectorAll('#site-menu a, .profile-links a, .archive-nav a')) {
    if (link.getAttribute('href') !== url) continue;
    const item = link.closest('li');
    if (!item) continue;
    const siblings = item.parentElement;
    item.remove();
    if (!siblings.children.length && siblings.classList.contains('child-links')) {
      siblings.parentElement.querySelector('.child-toggle')?.remove();
      siblings.remove();
    }
  }
  const dialog = event.target.closest('dialog');
  dialog?.close();
  dialog?.remove();
  focus?.focus({preventScroll: true});
  window.scrollTo({left: scrollX, top: scrollY, behavior: 'instant'});
  const notice = document.createElement('div');
  notice.className = 'notification notification--success';
  notice.innerHTML = '<p role="status"></p><button class="text-button notification-dismiss" type="button" aria-label="Dismiss notification" data-dismiss-notification>×</button>';
  notice.querySelector('p').textContent = kind === 'post' ? 'Post deleted.' : 'Page deleted.';
  document.querySelector('#notifications').append(notice);
});

document.addEventListener('blog:revision-deleted', event => {
  const dialog = event.target.closest('dialog');
  if (dialog) {
    dialog.close();
    dialog.remove();
  }
  for (const link of document.querySelectorAll('.revision-list a')) {
    if (link.dataset.revision === event.detail.revision) link.closest('li').remove();
  }
  htmx.ajax('GET', event.detail.next, {target: '#revision-preview', swap: 'innerHTML show:none'});
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
  // Keep the list in place. Only mark a version after its preview arrives.
  for (const link of document.querySelectorAll('.revision-list a')) {
    if (link.dataset.revision === selected) link.setAttribute('aria-current', 'true');
    else link.removeAttribute('aria-current');
  }
});
initialize(document);
