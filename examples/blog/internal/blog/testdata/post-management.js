// Long titles must wrap in their own column without moving the action controls.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
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
    await document.fonts.ready;
    assert(document.documentElement.scrollWidth <= Math.max(320, document.documentElement.clientWidth), 'Post management overflows the viewport.');
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
        assert(!!actions.querySelector('form[action^="/post/unpublish/"]') === (section.id === 'published-posts'), 'Only published posts must offer Move to drafts.');
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
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
