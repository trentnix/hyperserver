// Tags share suggestions without replacing earlier labels or losing recovery copies.
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
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    if (!sessionStorage.getItem('tags-ready')) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('tags-ready', 'true');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    window.blogConfirm = (_, proceed) => proceed();
    for (const path of ['/post', '/pages']) {
      const open = async () => {
        const previous = document.querySelector('form[data-draft-owner]');
        await htmx.ajax('GET', path + '/create', {target: '#article-content', swap: 'innerHTML'});
        await wait(() => (!previous || !previous.isConnected) && document.querySelector('.CodeMirror') && document.querySelector('[data-draft-status]:not([hidden])') && !document.querySelector('#article-content').classList.contains('htmx-settling'));
        return document.querySelector('form[data-draft-owner]');
      };
      let form = await open();
      const tags = form.elements.tags;
      assert([...tags.list.options].map(option => option.value).join(',') === 'go,self-hosting,sqlite', 'Suggestions must combine current post and page tags.');
      tags.value = 'GO, sq';
      tags.dispatchEvent(new Event('input', {bubbles: true}));
      const choice = [...tags.list.options].find(option => option.dataset.tag === 'sqlite');
      assert(choice?.value === 'GO, sqlite' && ![...tags.list.options].some(option => option.dataset.tag === 'go'), 'Suggestions must preserve earlier tags and omit duplicates.');
      tags.value = choice.value;
      tags.dispatchEvent(new Event('input', {bubbles: true}));
      assert(document.documentElement.scrollWidth <= innerWidth, 'Tag editing overflows the viewport.');
      await wait(() => form.querySelector('[data-draft-status]').textContent.includes('Saved locally'));
      form = await open();
      assert(!form.querySelector('[data-draft-recovery]').hidden, 'Tag changes must create a recovery copy.');
      form.querySelector('[data-draft-restore]').click();
      await wait(() => form.elements.tags.value === 'GO, sqlite');
      assert(form.querySelector('[data-draft-recovery]').hidden, 'Restoring tags must dismiss the recovery prompt.');

      // HTML history snapshots contain enhanced option values. Preserve their source labels.
      const clone = form.cloneNode(true);
      form.replaceWith(clone);
      htmx.trigger(clone.parentElement, 'htmx:afterSwap');
      clone.elements.tags.value = 'go, ';
      clone.elements.tags.dispatchEvent(new Event('input', {bubbles: true}));
      assert([...clone.elements.tags.list.options].some(option => option.value === 'go, sqlite'), 'History-restored suggestions accumulated tag prefixes.');

      // Copies written before tags existed must remain recoverable.
      const prefix = 'hyperserver.blog.draft.v1:' + form.dataset.draftOwner + ':' + path + ':';
      const key = Object.keys(localStorage).find(key => key.startsWith(prefix));
      const copy = JSON.parse(localStorage.getItem(key));
      delete copy.fields.tags;
      copy.fields.title = 'Older browser copy';
      for (const key of Object.keys(localStorage)) if (key.startsWith(prefix)) localStorage.removeItem(key);
      localStorage.setItem(prefix + 'legacy', JSON.stringify(copy));
      form = await open();
      assert(!form.querySelector('[data-draft-recovery]').hidden, 'Older recovery copies must remain available.');
      form.querySelector('[data-draft-restore]').click();
      await wait(() => form.elements.title.value === 'Older browser copy');
      assert(form.elements.tags.value === '', 'An older copy must not invent tags.');
    }
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
