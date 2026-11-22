(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const savedTitle = 'Saved with the native editor';
  try {
    const form = document.querySelector('#page-form, #post-form');
    if (form) {
      const panels = [...form.querySelectorAll('[data-post-component]')];
      if (!panels.length || panels.some(panel => panel.getClientRects().length)) {
        throw new Error('Raw component editors must stay hidden without the editor script.');
      }
      const content = form.elements.content;
      if (form.id === 'page-form') {
        content.value = content.value.replace('Original writing.', 'Updated page writing.');
      } else if (content.value !== '') {
        throw new Error('The component-only post must have an empty Content field.');
      }
      if (!window.EasyMDE) {
        const row = componentRowIn(form, 'callout');
        if (!row || !form.querySelector('[data-component-fallback]')?.getClientRects().length ||
            form.querySelector('[data-component-picker]').getClientRects().length ||
            row.querySelector('[data-component-place]').getClientRects().length || row.querySelector('[data-component-remove-row]').getClientRects().length) {
          throw new Error('Show the placement fallback and hide actions that need the enhanced editor.');
        }
        row.querySelector('[data-component-edit]').click();
        const panel = componentEditor(form, 'callout');
        if (!panel.closest('dialog').open || panel.querySelector('[data-component-remove]').getClientRects().length) {
          throw new Error('Existing settings must stay editable without offering removal.');
        }
        await closeComponentSettings(form);
      }
      form.elements.title.value = savedTitle;
      if (!form.checkValidity()) throw new Error('The native form must allow saving when a component supplies the content.');
      sessionStorage.setItem('saved-native-content', 'yes');
      form.requestSubmit();
      return;
    }
    if (!sessionStorage.getItem('saved-native-content')) {
      location.assign(document.querySelector('.action-edit').href);
      return;
    }
    const body = document.querySelector('.post-body');
    if (document.querySelector('#article-content h1')?.textContent !== savedTitle ||
        !body?.querySelector('.post-callout')?.textContent.includes('Keep this callout.') ||
        (location.pathname.startsWith('/page/') && !body.textContent.includes('Updated page writing.'))) {
      throw new Error('The native form must save edits and preserve hidden component values.');
    }
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + (error.stack || '')); }
})();
