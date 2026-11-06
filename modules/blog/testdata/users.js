(async () => {
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 10000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate.toString());
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    const stage = sessionStorage.getItem('users-stage');
    if (!stage) {
      document.querySelector('#email').value = 'trentnix@gmail.com';
      document.querySelector('#password').value = 'Sample-Password-123!';
      sessionStorage.setItem('users-stage', 'signed-in');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'signed-in') {
      sessionStorage.setItem('users-stage', 'checking');
      location.assign('/users');
      return;
    }
    await document.fonts.ready;
    const shell = document.querySelector('.site-header');
    assert(document.querySelector('.account-nav a[href="/users"]'), 'Users is missing from administration.');
    assert(document.querySelector('.user-row').textContent.includes('You'), 'Current admin is not identified.');
    assert(!document.querySelector('.user-row a'), 'The sole user has an Edit access link.');
    document.querySelector('a[href="/users/add"]').click();
    await wait(() => document.querySelector('.user-role-options'));
    assert(document.querySelector('.site-header') === shell, 'Adding a user reloaded the shell.');
    const options = [...document.querySelectorAll('.user-role-options input')];
    assert(options.length === 2 && options.every(option => option.type === 'radio'), 'Roles must be exclusive radio choices.');
    assert(options[0].checked, 'New users should default to Poster.');
    for (const option of options) {
      assert(option.getBoundingClientRect().width >= 18, 'Role radio is too small.');
    }
    const form = document.querySelector('.user-editor form');
    form.requestSubmit();
    await wait(() => form.querySelector('#user-email[aria-invalid="true"]'));
    assert(form.querySelector('.field-error'), 'Validation must use an inline error.');
    const response = await fetch('/users', {method: 'POST', body: new URLSearchParams({email: 'writer@example.com', role: 'poster'})});
    assert(response.ok, 'Invitation failed.');
    await htmx.ajax('GET', '/users', {target: '#article-content'});
    await wait(() => document.querySelectorAll('.user-row').length === 2);
    await wait(() => !document.querySelector('#article-content').classList.contains('htmx-settling'));
    const admin = [...document.querySelectorAll('.user-row')].find(row => row.querySelector('.user-self'));
    admin.querySelector('a').click();
    await wait(() => document.querySelector('.user-role-options'));
    await wait(() => !document.querySelector('#article-content').classList.contains('htmx-settling'));
    assert(document.querySelector('#user-role-poster').disabled, 'The last admin can select Poster.');
    assert(document.querySelector('#user-role-admin').checked, 'The last admin is not selected as Admin.');
    assert(document.querySelector('.user-editor button[type="submit"]').disabled, 'The last admin can save an unchanged role.');
    assert(document.querySelector('#last-admin-help'), 'The protected role has no explanation.');
    assert(!document.querySelector('.user-remove-link'), 'An admin can remove themselves.');
    document.querySelector('.user-editor > a').click();
    await wait(() => document.querySelector('.users-list'));
    await wait(() => !document.querySelector('#article-content').classList.contains('htmx-settling'));
    const row = [...document.querySelectorAll('.user-row')].find(row => row.textContent.includes('writer@example.com'));
    assert(row.textContent.includes('Poster') && row.textContent.includes('Invited'), 'Invitation status or role is missing.');
    assert(document.documentElement.scrollWidth <= Math.max(320, document.documentElement.clientWidth), 'User list overflows the viewport.');
    row.querySelector('a').click();
    await wait(() => document.querySelector('.user-editor'));
    assert(document.querySelector('.user-remove-link'), 'Remove access is missing.');
    assert(document.documentElement.scrollWidth <= Math.max(320, document.documentElement.clientWidth), 'User editor overflows the viewport.');
    await report('PASS');
  } catch (error) { await report(String(error) + '\n' + error.stack); }
})();
