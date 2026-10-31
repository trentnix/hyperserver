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
    let stage = sessionStorage.getItem('page-stage');
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
      await wait(() => location.pathname === '/pages' && !document.querySelector('#article-content').classList.contains('htmx-settling'));
      stage = 'create';
    }
    if (stage === 'create') {
      assert(location.pathname === '/pages', 'Missing page management screen.');
      const manager = document.querySelector('.page-manager');
      assert(manager.querySelector('.page-manager-list .page-manager-list .page-manager-list'), 'Management lost deeper subpages.');
      assert(manager.querySelector('.page-manager-draft')?.textContent === 'Draft', 'Missing draft badge.');
      assert(document.documentElement.scrollWidth <= Math.max(320, document.documentElement.clientWidth), 'Page management exceeds the viewport or minimum width.');
      const bounds = manager.getBoundingClientRect();
      for (const row of manager.querySelectorAll('.page-manager-row')) {
        const pageLink = row.querySelector('.page-manager-title a');
        assert(pageLink.getAttribute('href') === row.querySelector('.page-manager-title small').textContent, 'Page title must link to its displayed URL.');
        const links = [...row.querySelectorAll('.page-manager-actions a')];
        assert(links.map(link => link.textContent).join(',') === 'Edit,Delete', 'Page actions must not duplicate the title link.');
        for (const link of links) {
          assert(link.getAttribute('aria-label') === link.textContent + ' ' + pageLink.textContent, 'Page action must identify its page.');
        }
        const title = row.querySelector('.page-manager-title').getBoundingClientRect();
        const actions = row.querySelector('.page-manager-actions').getBoundingClientRect();
        assert(title.right <= bounds.right + 1 && actions.right <= bounds.right + 1, 'Page title or actions exceed the content width.');
        assert(title.bottom <= actions.top + 1 || title.right <= actions.left, 'Page title overlaps its actions.');
      }
      const parentRow = manager.querySelector('.page-manager-title a[href="/projects"]').closest('.page-manager-row');
      parentRow.querySelector('a[href^="/pages/delete/"]').click();
      await wait(() => document.querySelector('dialog[open] form[action^="/pages/delete/"]'));
      await wait(() => !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
      const confirmation = document.querySelector('dialog[open]');
      const deleteForm = confirmation.querySelector('form');
      deleteForm.requestSubmit();
      await wait(() => confirmation.querySelector('[role="alert"]')?.textContent.includes('This page has subpages.'));
      assert(confirmation.isConnected && confirmation.open, 'Blocked deletion replaced or dismissed its dialog.');
      assert(!confirmation.querySelector('form'), 'Blocked deletion still offers a delete form.');
      const controls = confirmation.querySelectorAll('button, a');
      assert(controls.length === 1 && controls[0].textContent === 'OK', 'Blocked deletion must offer only OK.');
      assert(location.pathname === '/pages' && document.querySelector('.page-manager') === manager, 'Blocked deletion replaced the underlying page.');
      confirmation.querySelector('[data-close-dialog]').click();
      await wait(() => !confirmation.open);

      document.querySelector('a[href="/pages/create"]').click();
      const form = await editorReady();
      const panelStyle = getComputedStyle(form.closest('.editor-panel'));
      assert(panelStyle.fontSize === '16px' && panelStyle.lineHeight === '24px', 'Page editor typography differs from other forms.');
      assert(form.querySelector('.button-secondary[data-close-dialog]'), 'Page editor Cancel is not secondary.');
      assert(form.textContent.includes('Navigation shows top-level pages and their immediate children.'), 'Navigation help does not explain the depth limit.');
      assert(!form.elements.published.checked, 'New pages must start as drafts.');
      form.elements.title.value = 'Contact';
      form.elements.title.dispatchEvent(new Event('input', {bubbles: true}));
      assert(form.elements.slug.placeholder === 'contact' && form.elements.slug.value === '', 'Page slug suggestion must follow its title without entering a value.');
      form.elements.slug.value = 'about';
      const cm = document.querySelector('.CodeMirror').CodeMirror;
      cm.setValue('A **contact** page.');
      form.requestSubmit();
      await wait(() => document.querySelector('#editor-errors').textContent.includes('This URL slug is already in use.'));
      const conflict = form.querySelector('#editor-errors');
      assert(conflict.textContent.includes('leave it blank to generate one from the title'), 'Slug conflict must explain how to recover.');
      assert(getComputedStyle(conflict).borderLeftWidth === '3px' && getComputedStyle(conflict.querySelector('.page-message')).paddingTop === '0px', 'Editor error must use a compact, distinct callout.');
      assert(conflict.getBoundingClientRect().top > form.querySelector('.publish-option').getBoundingClientRect().bottom, 'Editor error must appear near the save controls.');
      assert(document.querySelector('.CodeMirror').CodeMirror === cm && cm.getValue() === 'A **contact** page.', 'Conflict lost the mounted editor or draft.');
      form.elements.slug.value = '';
      form.noValidate = true;
      form.elements.displayOrder.value = 'invalid-order';
      form.requestSubmit();
      await wait(() => document.querySelector('#editor-errors').textContent.includes('display order'));
      assert(form.elements.displayOrder.value === 'invalid-order', 'HTMX validation lost the display order input.');
      form.elements.displayOrder.value = '';
      form.noValidate = false;
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
      assert(form.elements.slug.value === 'contact' && !form.elements.slug.required, 'Page editor must load its generated slug and allow clearing it.');
      form.elements.slug.value = '';
      form.elements.published.checked = true;
      sessionStorage.setItem('page-stage', 'published');
      form.requestSubmit();
      return;
    }
    if (stage === 'published') {
      const response = await fetch('/page/contact', {credentials: 'omit'});
      assert(response.ok && (await response.text()).includes('<strong>contact</strong>'), 'Published page was not readable.');
      const actions = [...document.querySelectorAll('.content-actions a')];
      assert(actions.map(action => action.textContent).join(',') === 'Edit,New subpage,Delete', 'Page action order or labels are incorrect.');
      assert(actions[1].getAttribute('href').startsWith('/pages/create?parent='), 'New subpage lost its parent selection.');
      const menu = document.querySelector('.content-actions .action-menu');
      assert(!menu.open, 'Page actions must start collapsed.');
      const more = menu.querySelector('summary');
      assert(Math.abs(more.getBoundingClientRect().top - actions[0].getBoundingClientRect().top) < 1, 'Page Edit and More must share a row.');
      more.click();
      for (const action of actions) {
        const style = getComputedStyle(action);
        assert(style.textDecorationLine === 'none', 'Page actions must not look like content links.');
        assert(action.getBoundingClientRect().height >= (innerWidth < 768 ? 44 : 36), 'Page action is too small to interact with.');
      }
      assert(document.documentElement.scrollWidth <= Math.max(320, innerWidth), 'Page actions overflow the viewport.');
      document.querySelector('#article-content a[href^="/pages/delete/"]').click();
      assert(!menu.open, 'Selecting a page action must close More.');
      await wait(() => document.querySelector('dialog[open] form[action^="/pages/delete/"]'));
      await wait(() => !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
      assert(document.querySelector('dialog[open] .button-danger[type=submit]') && document.querySelector('dialog[open] .button-secondary[data-close-dialog]'), 'Page deletion needs destructive and secondary controls.');
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
    for (const menu of ['#site-menu', '.profile-links']) {
      assert(document.querySelector(menu + ' a[href="/about"]').textContent === 'About this site', 'Navigation title did not update.');
    }
    assert(document.querySelector('.post-body strong')?.textContent === 'biography', 'About content did not update.');
    const editURL = document.querySelector('#article-content a[href^="/pages/update/"]').getAttribute('href');
    const saveURL = editURL.replace('/pages/update/', '/pages/');
    const currentEditor = new DOMParser().parseFromString(await (await fetch(editURL)).text(), 'text/html');
    for (const value of ['-1', 'invalid-order', '1000001']) {
      const response = await fetch(saveURL, {method: 'POST', body: new URLSearchParams({
        title: 'About this site', slug: 'about', content: 'A **biography**.', published: 'on', displayOrder: value, version: currentEditor.querySelector('[name="version"]').value
      })});
      assert(response.status === 422, 'Invalid order was accepted.');
      const html = new DOMParser().parseFromString(await response.text(), 'text/html');
      assert(html.querySelector('#page-order').value === value, 'Ordinary validation lost the display order input.');
    }
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
