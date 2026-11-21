(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  try {
    const page = document.querySelector('.info-page');
    const outline = page.querySelector('.post-toc');
    const body = page.querySelector('.post-body').getBoundingClientRect();
    const bounds = outline.getBoundingClientRect();
    const viewport = document.documentElement.clientWidth;
    const sticky = getComputedStyle(outline).position === 'sticky';
    if (bounds.left < 0 || bounds.right > viewport) throw new Error('The outline must fit the viewport.');
    if (page.classList.contains('page-blocks') ? sticky || bounds.left < body.left || bounds.right > body.right : !sticky || bounds.left < body.right) {
      throw new Error('The outline must stay inline with Preview blocks and use the right rail with Preview list.');
    }
    if (page.classList.contains('page-blocks')) {
      const inset = Math.max(0, (innerWidth - Math.max(1680, innerHeight * 21 / 9)) / 2);
      const main = document.querySelector('#main-content').getBoundingClientRect();
      if (Math.abs(main.right - (viewport - inset)) > 1) throw new Error('Preview blocks must fit inside the 21:9 layout.');
    }
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + (error.stack || '')); }
})();
