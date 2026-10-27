// Management keeps actions aligned and returns to the list after a confirmed deletion.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate.toString());
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  const deleteFirst = async (section, nextStage) => {
    const row = document.querySelector(section + ' .page-manager-row');
    sessionStorage.setItem('deleted-post-url', row.querySelector('.page-manager-title a').getAttribute('href'));
    row.querySelector('a[href^="/post/delete/"]').click();
    await wait(() => document.querySelector('dialog[open] form[action^="/post/delete/"]'));
    await wait(() => !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
    sessionStorage.setItem('management-stage', nextStage);
    document.querySelector('dialog[open] form').requestSubmit();
  };
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    const stage = sessionStorage.getItem('management-stage');
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('management-stage', 'logged-in');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'logged-in') {
      sessionStorage.setItem('management-stage', 'checking');
      location.assign('/post/manage');
      return;
    }
    if (stage === 'draft-deleted' || stage === 'published-deleted') {
      assert(location.pathname === '/post/manage' && document.querySelector('.page-manager'), 'Deleting a post must return to Posts.');
      assert(!document.querySelector('dialog[open]') && !document.querySelector('.post-body'), 'Deletion left a dialog open or displayed another post.');
      assert((await fetch(sessionStorage.getItem('deleted-post-url'))).status === 404, 'Deleted post remains readable.');
      assert(document.querySelectorAll('#draft-posts .page-manager-row').length === 2, 'Draft deletion removed the wrong number of posts.');
      if (stage === 'draft-deleted') {
        assert(document.querySelectorAll('#published-posts .page-manager-row').length === 3, 'Draft deletion affected published posts.');
        await deleteFirst('#published-posts', 'published-deleted');
      } else {
        assert(document.querySelectorAll('#published-posts .page-manager-row').length === 2, 'Published deletion removed the wrong number of posts.');
        await report('PASS');
      }
      return;
    }
    await document.fonts.ready;
    assert(document.documentElement.scrollWidth <= Math.max(320, document.documentElement.clientWidth), 'Post management overflows the viewport.');
    const accountNav = document.querySelector('nav.account-nav[aria-label="Account"]');
    assert(accountNav, 'Account links must have a named navigation landmark.');
    const navBounds = accountNav.getBoundingClientRect();
    const controls = [...accountNav.querySelectorAll('a, button')];
    assert(controls.map(control => control.textContent).join(',') === 'Posts,Pages,Site Profile,Log out', 'Account controls changed.');
    const accountMenu = accountNav.querySelector('.account-menu');
    const summary = accountMenu.querySelector('summary');
    const label = accountNav.parentElement.querySelector('.account-label');
    assert(label && label.getBoundingClientRect().height > 0 && label.textContent === 'Administration', 'Admin toolbar must have a visible label at every viewport width.');
    const labelBounds = label.getBoundingClientRect();
    assert(labelBounds.bottom < navBounds.top && Math.abs(labelBounds.left - navBounds.left) < 1, 'Administration caption must sit above the toolbar, aligned with its left edge.');
    const navStyle = getComputedStyle(accountNav);
    assert(navStyle.backgroundColor === 'rgb(48, 57, 70)' && navStyle.color === 'rgb(232, 237, 243)', 'Admin toolbar must use light controls on its dark background.');
    assert(navBounds.height <= 60, 'Admin toolbar must stay compact at every viewport width.');
    if (innerWidth < 768) {
      assert(!accountMenu.open && !summary.hidden, 'Mobile admin links must start collapsed.');
      assert(Math.abs(summary.getBoundingClientRect().top - controls[3].getBoundingClientRect().top) < 1, 'Manage and Log out must share one row.');
      summary.click();
      assert(accountMenu.open, 'Manage did not open.');
      const dropdownBounds = accountMenu.querySelector('.action-menu-items').getBoundingClientRect();
      assert(Math.abs(dropdownBounds.left - navBounds.left) < 1 && dropdownBounds.top >= navBounds.bottom, 'Mobile admin dropdown must align with the toolbar left edge and open below it.');
    } else {
      assert(accountMenu.open && summary.hidden, 'Desktop admin links must stay expanded.');
      for (const link of accountMenu.querySelectorAll('a')) {
        assert(parseFloat(getComputedStyle(link).paddingLeft) >= 8 && parseFloat(getComputedStyle(link).paddingRight) >= 8, 'Expanded admin links must have horizontal breathing room.');
      }
    }
    for (const control of controls) {
      const bounds = control.getBoundingClientRect();
      assert(bounds.height >= 44 && bounds.left >= navBounds.left - 1 && bounds.right <= navBounds.right + 1, 'Account controls must be touch-friendly and fit the toolbar: ' + JSON.stringify({bounds, navBounds}));
      if (innerWidth >= 768) assert(Math.abs(bounds.top - controls[0].getBoundingClientRect().top) < 1, 'Desktop account controls should fit on one row.');
    }
    const logoutStyle = getComputedStyle(controls[3]);
    const linkStyle = getComputedStyle(controls[0]);
    assert(logoutStyle.paddingLeft === linkStyle.paddingLeft && logoutStyle.paddingRight === linkStyle.paddingRight, 'Log out must use the same horizontal padding as the admin links.');
    assert(Math.abs(controls[3].getBoundingClientRect().right - (navBounds.right - parseFloat(navStyle.paddingRight))) < 1, 'Log out must stay aligned with the toolbar right inset.');
    controls[0].focus();
    const focusStyle = getComputedStyle(controls[0]);
    assert(focusStyle.outlineStyle !== 'none' && parseFloat(focusStyle.outlineWidth) >= 2, 'Keyboard focus must remain visible.');
    assert(focusStyle.outlineColor === 'rgb(185, 216, 255)', 'Keyboard focus must contrast with the dark toolbar.');
    controls[0].blur();
    if (innerWidth < 768) {
      controls[0].focus();
      controls[0].dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
      assert(!accountMenu.open && document.activeElement === summary, 'Escape must close Manage and restore focus.');
      summary.click();
      document.querySelector('.page-manager-heading').click();
      assert(!accountMenu.open, 'Outside click must close Manage.');
    } else {
      document.querySelector('.page-manager-heading').click();
      assert(accountMenu.open, 'Outside click must not hide desktop admin links.');
    }
    const frame = window.frameElement;
    const originalWidth = frame.style.width;
    const startedMobile = innerWidth < 768;
    (startedMobile ? summary : controls[0]).focus();
    frame.style.width = startedMobile ? '1100px' : '390px';
    await wait(() => summary.hidden === startedMobile && accountMenu.open === startedMobile);
    assert(document.activeElement === (startedMobile ? controls[0] : summary), 'Changing layout must move focus to a visible admin control.');
    frame.style.width = originalWidth;
    await wait(() => summary.hidden === !startedMobile && accountMenu.open === !startedMobile);
    assert(document.activeElement === (startedMobile ? summary : controls[0]), 'Returning to the original layout must preserve usable keyboard focus.');
    for (const section of document.querySelectorAll('#draft-posts, #published-posts')) {
      const rows = [...section.querySelectorAll('.page-manager-row')];
      assert(rows.length === 3, 'Expected short, long, and unbroken titles.');
      let positions;
      for (const row of rows) {
        const title = row.querySelector('.page-manager-title');
        const actions = row.querySelector('.page-manager-actions');
        const titleLink = title.querySelector('a');
        assert(titleLink.getAttribute('href').startsWith('/post/s/'), 'Post title must open the post or draft preview.');
        const links = [...actions.querySelectorAll('a')];
        assert(links.map(link => link.textContent).join(',') === 'Edit,Delete', 'Post actions must not duplicate the title link.');
        for (const link of links) {
          assert(link.getAttribute('aria-label') === link.textContent + ' ' + titleLink.textContent, 'Post action must identify its post.');
        }
        assert(!actions.querySelector('form'), 'Publication changes belong in the editor, not the Posts list.');
        const titleBounds = title.getBoundingClientRect();
        const actionBounds = actions.getBoundingClientRect();
        assert(title.scrollWidth <= title.clientWidth + 1 && actions.scrollWidth <= actions.clientWidth + 1, 'Title or actions overflow their column.');
        if (innerWidth < 768) {
          assert(actionBounds.top >= titleBounds.bottom, 'Mobile actions must stack below the title.');
        } else {
          assert(titleBounds.right + 23 <= actionBounds.left, 'Title must stay in a separate column.');
          const controls = [...actions.querySelectorAll('a, button')].map(control => control.getBoundingClientRect());
          if (positions) controls.forEach((bounds, index) => assert(Math.abs(bounds.left - positions[index]) < 1, 'Actions do not align between rows.'));
          positions = controls.map(bounds => bounds.left);
          assert(controls.every(bounds => Math.abs(bounds.top - controls[0].top) < 1), 'Actions wrap onto different lines.');
          if (titleLink.textContent.length > 200) {
            assert(titleLink.getClientRects().length > 1, 'Long title did not wrap within its column.');
          }
        }
      }
    }
    await deleteFirst('#draft-posts', 'draft-deleted');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
