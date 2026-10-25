// Exercise page creation, conflict feedback, publication, deletion, and About editing.
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
  const editorReady = async () => {
    await wait(() => document.querySelector('dialog[open] .CodeMirror'));
    await wait(() => !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
    return document.querySelector('#page-form');
  };
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    const stage = sessionStorage.getItem('page-stage');
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('page-stage', 'logged-in');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'logged-in') {
      document.querySelector('.account-nav a[href="/pages"]').click();
      sessionStorage.setItem('page-stage', 'create');
      return;
    }
    if (stage === 'create') {
      assert(location.pathname === '/pages', 'Missing page management screen.');
      document.querySelector('a[href="/pages/create"]').click();
      const form = await editorReady();
      assert(!form.elements.published.checked, 'New pages must start as drafts.');
      form.elements.title.value = 'Contact';
      form.elements.slug.value = 'about';
      const cm = document.querySelector('.CodeMirror').CodeMirror;
      cm.setValue('A **contact** page.');
      form.requestSubmit();
      await wait(() => document.querySelector('#editor-errors').textContent.includes('already used'));
      assert(document.querySelector('.CodeMirror').CodeMirror === cm && cm.getValue() === 'A **contact** page.', 'Conflict lost the mounted editor or draft.');
      form.elements.slug.value = 'contact';
      document.querySelector('.tool-preview').click();
      await wait(() => document.querySelector('.editor-preview strong')?.textContent === 'contact');
      document.querySelector('.tool-preview').click();
      sessionStorage.setItem('page-stage', 'draft');
      form.requestSubmit();
      return;
    }
    if (stage === 'draft') {
      assert(location.pathname === '/page/contact', 'Draft save did not reach its page.');
      assert(document.querySelector('#article-content').textContent.includes('Draft'), 'Missing draft label.');
      assert((await fetch('/page/contact', {credentials: 'omit'})).status === 404, 'Anonymous visitor can read a draft.');
      document.querySelector('#article-content a[href^="/pages/update/"]').click();
      const form = await editorReady();
      assert(document.querySelector('.CodeMirror').CodeMirror.getValue() === 'A **contact** page.', 'Stored page was not loaded into the editor.');
      form.elements.published.checked = true;
      sessionStorage.setItem('page-stage', 'published');
      form.requestSubmit();
      return;
    }
    if (stage === 'published') {
      const response = await fetch('/page/contact', {credentials: 'omit'});
      assert(response.ok && (await response.text()).includes('<strong>contact</strong>'), 'Published page was not readable.');
      document.querySelector('#article-content a[href^="/pages/delete/"]').click();
      await wait(() => document.querySelector('dialog[open] form[action^="/pages/delete/"]'));
      sessionStorage.setItem('page-stage', 'deleted');
      document.querySelector('dialog[open] form').requestSubmit();
      return;
    }
    if (stage === 'deleted') {
      assert(location.pathname === '/pages', 'Deletion did not return to management.');
      assert(!document.querySelector('#article-content a[href="/page/contact"]'), 'Deleted page remains listed.');
      assert((await fetch('/page/contact')).status === 404, 'Deleted page remains readable.');
      sessionStorage.setItem('page-stage', 'about');
      location.assign('/about');
      return;
    }
    if (stage === 'about') {
      document.querySelector('#article-content a[href^="/pages/update/"]').click();
      const form = await editorReady();
      assert(form.elements.slug.value === 'about', 'About did not use the page editor.');
      form.elements.title.value = 'About this site';
      document.querySelector('.CodeMirror').CodeMirror.setValue('A **biography**.');
      sessionStorage.setItem('page-stage', 'about-saved');
      form.requestSubmit();
      return;
    }
    assert(stage === 'about-saved' && location.pathname === '/about', 'About lost its short URL.');
    assert(document.title === 'Trent Nix | About this site', 'Page title did not update.');
    assert(document.querySelector('.post-body strong')?.textContent === 'biography', 'About content did not update.');
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
