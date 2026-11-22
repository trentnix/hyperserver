(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  try {
    if (location.pathname === '/post/s/toc-browser-review') {
      const article = document.querySelector('.post-body');
      const toc = article.querySelector('.post-toc');
      const links = [...toc.querySelectorAll('a')];
      const nested = toc.querySelector('ul ul ul a');
      const heading = article.querySelector('#section-before-leaving');
      const rail = innerWidth >= 1680 && innerWidth * 9 >= innerHeight * 21;
      const bounds = toc.getBoundingClientRect();
      assert(links.map(link => link.hash).join(',') === '#section-before-leaving,#section-prepare,#section-bring-water,#section-finish' &&
        nested?.hash === '#section-bring-water', 'The outline must include H1, H2, and H4 and nest H4 under H2.');
      assert(rail ? bounds.left >= article.getBoundingClientRect().right : bounds.bottom < heading.getBoundingClientRect().top,
        'Only a sufficiently wide 21:9 viewport may show the outline to the right.');
      if (rail) {
        window.scrollTo(0, window.scrollY + heading.getBoundingClientRect().top + 80);
        await waitForComponent(() => Math.abs(toc.getBoundingClientRect().top - 24) < 2, 'sticky right rail');
      }
      nested.click();
      await waitForComponent(() => location.hash === '#section-bring-water', 'native heading navigation');
      assert(article.querySelector(location.hash).getBoundingClientRect().top >= 0, 'The jump must leave the heading visible.');
      assert(document.documentElement.scrollWidth <= innerWidth + 1, 'The reader must not scroll sideways.');
      await report('PASS');
      return;
    }
    const form = document.querySelector('#post-form');
    await waitForComponent(() => form.querySelector('.CodeMirror'), 'post editor');
    form.elements.title.value = 'Table of contents browser review';
    form.elements.slug.value = 'toc-browser-review';
    const cm = form.querySelector('.CodeMirror').CodeMirror;
    const paragraphs = 'This paragraph gives the reader enough room to scroll and compare the outline.\n\n'.repeat(32);
    cm.setValue('Opening paragraph.\n\n# Before leaving\n\n## Prepare\n\n' + paragraphs +
      '### Essentials\n\n#### Bring water\n\n' + paragraphs + '## Finish\n\n' + paragraphs);
    cm.focus();
    cm.setCursor({line: 2, ch: 0});
    const picker = form.querySelector('[data-component-add="table-of-contents"]');
    const selected = panel => [...panel.querySelectorAll('[data-toc-levels] input:checked')].map(input => input.value).join(',');
    let panel = await addTestComponent(picker);
    assert(selected(panel) === '2,3' && panel.querySelector('[data-toc-levels] input:focus'),
      'New outlines must start with H2 and H3 and focus the heading controls.');
    panel.querySelector('input[value="1"]').click();
    panel.closest('dialog').querySelector('[data-component-cancel]').click();
    assert(!form.elements.tableOfContents.value && !form.elements.content.value.includes('hyperserver:component'),
      'Cancel must leave no table of contents behind.');
    panel = await addTestComponent(picker);
    assert(selected(panel) === '2,3' && panel.querySelector('textarea[name]').hidden,
      'Cancel must discard level changes, and the heading controls must stand in for JSON.');
    for (const value of [2, 3]) panel.querySelector(`input[value="${value}"]`).click();
    await closeComponentSettings(form);
    assert(panel.closest('dialog').open && panel.querySelector('[data-component-settings-error]')?.textContent === 'Choose at least one heading level.',
      'Done must require at least one heading level.');
    for (const value of [1, 2, 4]) panel.querySelector(`input[value="${value}"]`).click();
    await closeComponentSettings(form);
    assert(form.elements.tableOfContents.value === '{"levels":[1,2,4]}' && form.elements.content.value.indexOf('hyperserver:component') <
      form.elements.content.value.indexOf('# Before leaving'), 'Done must keep the heading levels and place the outline at the selected location.');
    await waitForComponent(() => componentRowIn(form, 'table-of-contents'), 'table of contents row');
    componentRowIn(form, 'table-of-contents').querySelector('[data-component-edit]').click();
    assert(selected(panel) === '1,2,4', 'Reopening the settings must show the selected levels.');
    panel.querySelector('input[value="6"]').click();
    panel.closest('dialog').querySelector('[data-component-cancel]').click();
    assert(form.elements.tableOfContents.value === '{"levels":[1,2,4]}', 'Cancel must keep the previous levels.');
    form.querySelector('[name=published][value=on]').checked = true;
    form.requestSubmit();
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
