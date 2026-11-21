(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  try {
    const comparison = document.querySelector('.post-body .image-comparison');
    const stage = comparison.querySelector('.comparison-stage');
    const before = stage.querySelector('.comparison-before img');
    const after = stage.querySelector('.comparison-after img');
    stage.scrollIntoView();
    await waitForComponent(() => before.naturalWidth > 0 && after.naturalWidth > 0, 'comparison images');
    await waitForComponent(() => comparison.classList.contains('comparison-ready'), 'interactive comparison');
    const assertRatio = image => {
      const {width, height} = image.getBoundingClientRect();
      const expected = width * image.naturalHeight / image.naturalWidth;
      if (Math.abs(height - expected) > 1) throw new Error(`Expected image height ${expected}, got ${height}`);
    };
    if (before.naturalHeight / before.naturalWidth > 3) {
      if (stage.clientHeight > innerHeight * .85 + 1 || stage.clientHeight < innerHeight * .8) {
        throw new Error('Tall comparisons must fit within the viewport while leaving room to reach the slider.');
      }
    } else {
      assertRatio(before);
    }
    if ([before, after].some(image => image.offsetHeight !== stage.clientHeight || getComputedStyle(image).objectFit !== 'contain')) {
      throw new Error('Both images must share the first image’s frame and fit inside it.');
    }
    comparison.classList.remove('comparison-ready');
    assertRatio(before);
    assertRatio(after);
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + (error.stack || '')); }
})();
