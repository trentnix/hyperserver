(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const titles = ['What changed between the measurements?', 'Notes on repeating these measurements on another machine'];
  const bodies = ['First **measurement**.\n\n- Keep the same inputs.', 'Second **measurement**.\n\n```text\n' + 'long-output-'.repeat(24) + '\n```'];
  const set = (input, value) => { input.value = value; input.dispatchEvent(new Event('input', {bubbles: true})); };
  try {
    if (sessionStorage.getItem('details-published')) {
      const disclosures = [...document.querySelectorAll('.post-body .expandable-details')];
      assert(disclosures.length === 2, 'Both details must render.');
      for (const title of titles) {
        const details = disclosures.find(disclosure => disclosure.querySelector('summary').textContent === title);
        assert(details, 'The saved title must appear in the disclosure: ' + title);
        const summary = details.querySelector('summary');
        const body = details.querySelector('.expandable-details-body');
        const collapsedHeight = details.getBoundingClientRect().height;
        assert(!details.open && collapsedHeight <= summary.getBoundingClientRect().height + 2, 'Details must start collapsed.');
        assert(summary.getBoundingClientRect().height >= 44, 'The disclosure must have room to tap.');
        summary.focus();
        assert(document.activeElement === summary, 'The summary must accept keyboard focus.');
        summary.click();
        assert(details.open && details.getBoundingClientRect().height > collapsedHeight && body.querySelector('strong').textContent === 'measurement', 'The summary must reveal rendered Markdown.');
        assert(disclosures.filter(disclosure => disclosure.open).length === 1, 'Each disclosure must open independently.');
        assert(document.documentElement.scrollWidth <= innerWidth + 1, 'The expanded body must fit the viewport.');
        summary.click();
        assert(!details.open && details.getBoundingClientRect().height === collapsedHeight, 'Selecting the title again must close the details.');
      }
      const edit = document.querySelector('a[href^="/post/update/"]');
      await htmx.ajax('GET', edit.getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
      const form = document.querySelector('#post-form');
      await waitForComponent(() => form.querySelector('.CodeMirror'), 'saved post editor');
      const fields = [...form.querySelectorAll('[data-details-editor] textarea[name]')];
      assert(fields.length === 2 && fields.every(field => {
        const {title, body} = JSON.parse(field.value);
        return body === bodies[titles.indexOf(title)];
      }), 'Reopening must preserve both Markdown bodies.');
      sessionStorage.removeItem('details-published');
      await report('PASS');
      return;
    }

    const form = document.querySelector('#post-form');
    await waitForComponent(() => form.querySelector('.CodeMirror'), 'post editor');
    form.elements.title.value = 'Expandable details review';
    form.elements.slug.value = 'expandable-details-review';
    const picker = form.querySelector('[data-component-add="expandable-details"]');
    for (const [index, title] of titles.entries()) {
      const panel = await addTestComponent(picker);
      const name = panel.querySelector('input');
      const body = panel.querySelector('fieldset textarea');
      assert(document.activeElement === name && body.labels[0].textContent === 'Details', 'The dialog must focus Title and label the Markdown field.');
      assert(!panel.querySelector('fieldset').disabled, 'Adding the component must enable its required fields.');
      set(name, title);
      set(body, bodies[index]);
      closeComponentSettings(form);
    }
    form.querySelector('[name=published][value=on]').checked = true;
    sessionStorage.setItem('details-published', 'true');
    form.requestSubmit();
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
