// Cleanup uses ordinary navigation and explicit confirmation, including on mobile.
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
  window.addEventListener('error', event => report(event.message));
  try {
    const stage = sessionStorage.getItem('media-stage');
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('media-stage', 'logged-in');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'logged-in') {
      sessionStorage.setItem('media-stage', 'list');
      if (innerWidth < 768) document.querySelector('.account-menu summary').click();
      document.querySelector('a[href="/media/unused"]').click();
      return;
    }
    assert(document.documentElement.scrollWidth <= innerWidth, 'Cleanup overflows the viewport.');
    if (stage === 'list' || stage === 'cancelled') {
      const rows = document.querySelectorAll('[aria-label="Unused uploads"] .page-manager-row');
      assert(rows.length === 1, 'Cleanup must list only the unused image.');
      const image = rows[0].querySelector('img');
      image.scrollIntoView();
      await wait(() => image.complete && image.naturalWidth > 0);
      const box = image.getBoundingClientRect();
      assert(box.width === 80 && box.height === 80, 'Cleanup preview must stay bounded.');
      sessionStorage.setItem('media-image', image.getAttribute('src'));
      sessionStorage.setItem('media-stage', stage === 'list' ? 'confirmation' : 'delete');
      rows[0].querySelector('a[href^="/media/delete/"]').click();
      return;
    }
    if (stage === 'confirmation') {
      assert(document.querySelector('h1').textContent === 'Delete image?', 'Missing delete confirmation.');
      assert(document.body.textContent.includes('browser recovery copies'), 'Missing unsaved-image warning.');
      sessionStorage.setItem('media-stage', 'cancelled');
      document.querySelector('.confirmation-panel a[href="/media/unused"]').click();
      return;
    }
    if (stage === 'delete') {
      sessionStorage.setItem('media-stage', 'deleted');
      document.querySelector('.confirmation-panel form').requestSubmit();
      return;
    }
    if (stage === 'deleted') {
      assert(location.pathname === '/media/unused', 'Deletion did not return to cleanup.');
      assert(document.body.textContent.includes('No unused uploads.'), 'Empty state is missing.');
      assert(!document.querySelector('a[href="/media/delete-unused"]'), 'Empty list offers bulk deletion.');
      assert((await fetch(sessionStorage.getItem('media-image'))).status === 404, 'Deleted upload is still served.');
      const uploads = [];
      for (const color of ['#d09030', '#306090']) {
        const canvas = document.createElement('canvas');
        canvas.width = canvas.height = 8;
        const context = canvas.getContext('2d');
        context.fillStyle = color;
        context.fillRect(0, 0, 8, 8);
        const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
        const body = new FormData();
        body.append('image', blob, 'unused.png');
        const response = await fetch('/post/images/upload', {method: 'POST', body});
        assert(response.ok, 'Fixture upload failed.');
        uploads.push((await response.json()).data.filePath);
      }
      sessionStorage.setItem('bulk-images', JSON.stringify(uploads));
      sessionStorage.setItem('media-stage', 'bulk-list');
      location.assign('/media/unused');
      return;
    }
    if (stage === 'bulk-list' || stage === 'bulk-cancelled') {
      assert(document.querySelectorAll('[aria-label="Unused uploads"] .page-manager-row').length === 2, 'Bulk list lost uploads before confirmation.');
      sessionStorage.setItem('media-stage', stage === 'bulk-list' ? 'bulk-confirm' : 'bulk-delete');
      document.querySelector('a[href="/media/delete-unused"]').click();
      return;
    }
    if (stage === 'bulk-confirm' || stage === 'bulk-delete') {
      const panel = document.querySelector('.confirmation-panel');
      assert(panel.textContent.includes('2 images') && panel.textContent.includes('bytes'), 'Bulk confirmation lacks count or size.');
      assert(panel.querySelectorAll('input[name="file"]').length === 2, 'Confirmation did not retain its reviewed uploads.');
      if (stage === 'bulk-confirm') {
        sessionStorage.setItem('media-stage', 'bulk-cancelled');
        panel.querySelector('a[href="/media/unused"]').click();
      } else {
        sessionStorage.setItem('media-stage', 'bulk-result');
        panel.querySelector('form').requestSubmit();
      }
      return;
    }
    if (stage === 'bulk-result') {
      assert(document.body.textContent.includes('Deleted: 2. Skipped: 0. Not processed: 0.'), 'Bulk result is inaccurate.');
      for (const image of JSON.parse(sessionStorage.getItem('bulk-images'))) {
        assert((await fetch(image)).status === 404, 'Bulk-deleted image is still served.');
      }
      sessionStorage.setItem('media-stage', 'history-list');
      location.assign('/media/unused');
      return;
    }
    if (stage === 'history-list') {
      const rows = document.querySelectorAll('[aria-label="Historical uploads"] .page-manager-row');
      assert(rows.length === 1, 'Bulk cleanup removed a historical upload.');
      sessionStorage.setItem('media-stage', 'history-confirm');
      rows[0].querySelector('a[href^="/media/delete/"]').click();
      return;
    }
    if (stage === 'history-confirm') {
      const panel = document.querySelector('.confirmation-panel');
      const links = panel.querySelectorAll('a[href^="/history/"]');
      assert(links.length === 1, 'Confirmation must identify the affected revision once.');
      sessionStorage.setItem('media-history', links[0].getAttribute('href'));
      const form = panel.querySelector('form');
      assert(!form.checkValidity(), 'Historical deletion lacks explicit acknowledgment.');
      form.querySelector('[name="deleteHistory"]').click();
      assert(form.checkValidity(), 'Acknowledged historical deletion cannot be submitted.');
      sessionStorage.setItem('media-stage', 'history-deleted');
      form.requestSubmit();
      return;
    }
    assert(!document.querySelector('[aria-label="Historical uploads"]'), 'Deleted historical upload remains listed.');
    const preview = await fetch(sessionStorage.getItem('media-history'));
    assert(preview.ok, 'Deleted media made the historical writing unavailable.');
    const doc = new DOMParser().parseFromString(await preview.text(), 'text/html');
    assert(doc.querySelectorAll('.missing-image').length === 2, 'Missing history images lack placeholders.');
    await report('PASS');
  } catch (error) {
    await report(error.stack || String(error));
  }
})();
