// Shared feedback on real authentication, writing, profile, and media forms.
(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate.toString());
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  const settled = () => !document.querySelector('.htmx-settling, .htmx-request');
  const navigate = async (path, selector) => {
    const navigation = htmx.ajax('GET', path, {target: '#article-content'});
    document.querySelector('dialog[open] [data-confirm-accept]')?.click();
    await navigation;
    await wait(() => document.querySelector(selector) && settled());
    return document.querySelector(selector);
  };
  const edit = (input, value) => {
    input.value = value;
    input.dispatchEvent(new Event('input', {bubbles: true}));
  };
  const assertField = input => {
    assert(input.getAttribute('aria-invalid') === 'true', input.name + ' must be marked invalid.');
    const descriptions = (input.getAttribute('aria-describedby') || '').split(' ').map(id => document.getElementById(id));
    assert(descriptions.some(node => node?.textContent.trim() && node.matches('.field-error, .form-error')), input.name + ' must describe its error.');
    input.focus();
    const style = getComputedStyle(input);
    assert(style.borderTopColor === 'rgb(161, 38, 38)' && style.outlineColor === 'rgb(161, 38, 38)' && style.outlineOffset === '0px' && style.boxShadow === 'none', input.name + ' must use the shared red border and focus outline.');
    assert(document.documentElement.scrollWidth <= innerWidth, 'Error feedback must not overflow.');
  };
  const assertPanel = panel => {
    const style = getComputedStyle(panel);
    assert(panel.getAttribute('role') === 'alert' && style.color === 'rgb(161, 38, 38)' && style.backgroundColor === 'rgb(255, 243, 242)' && style.borderLeftWidth === '4px' && style.borderRightWidth === '1px', 'Operation errors must use the shared panel treatment.');
  };
  const nativeError = async (form, field) => {
    edit(form.elements[field], '');
    let suppressed = false;
    form.elements[field].addEventListener('invalid', event => { suppressed = event.defaultPrevented; }, {once: true});
    form.requestSubmit();
    await wait(() => document.activeElement === form.elements[field]);
    assert(suppressed, 'Native validation popup must be replaced by inline feedback.');
    assertField(form.elements[field]);
  };
  try {
    const stage = sessionStorage.getItem('form-errors-stage');
    if (!stage) {
      let form = document.querySelector('#login-form');
      await nativeError(form, 'email');
      edit(form.elements.email, 'invalid');
      edit(form.elements.password, 'Wrong-Password-123!');
      form.requestSubmit();
      await wait(() => document.querySelector('#email-error') && settled());
      form = document.querySelector('#login-form');
      assertField(form.elements.email);
      edit(form.elements.email, 'trentnix@gmail.com');
      assert(!document.querySelector('#email-error'), 'Editing must clear the server field message.');
      edit(form.elements.password, 'Wrong-Password-123!');
      form.requestSubmit();
      await wait(() => document.querySelector('.form-messages[role=alert]') && settled());
      assertPanel(document.querySelector('.form-messages[role=alert]'));
      assert(!document.querySelector('#login-form [aria-invalid=true]'), 'Rejected credentials must not invent an invalid field.');

      for (const [path, selector, fields, invalid] of [
        ['/auth/reset/request/email', '#reset-request-form', {email: 'invalid'}, 'email'],
        [__RESET_URL__, '#reset-password-form', {password: 'short', passwordMatch: 'different'}, 'password']
      ]) {
        form = await navigate(path, selector);
        for (const [name, value] of Object.entries(fields)) edit(form.elements[name], value);
        form.requestSubmit();
        await wait(() => document.querySelector(selector + ' [data-field-error]') && settled());
        assertField(document.querySelector(selector).elements[invalid]);
      }
      form = await navigate('/auth/login/email', '#login-form');
      edit(form.elements.email, 'trentnix@gmail.com');
      edit(form.elements.password, 'Sample-Password-123!');
      sessionStorage.setItem('form-errors-stage', 'admin');
      form.requestSubmit();
      return;
    }
    if (stage === 'admin') {
      let form = await navigate('/pages/create', '#page-form');
      await nativeError(form, 'title');
      edit(form.elements.title, 'Error checks');
      edit(form.elements.displayOrder, '1000001');
      form.requestSubmit();
      await wait(() => form.elements.displayOrder.getAttribute('aria-invalid') === 'true' && settled());
      assertField(form.elements.displayOrder);
      assertPanel(form.querySelector('#editor-errors'));
      edit(form.elements.displayOrder, '10');
      assert(form.elements.displayOrder.getAttribute('aria-describedby') === 'order-help', 'Clearing an error must keep the field help.');

      form = await navigate('/profile', '#profile-form');
      await nativeError(form, 'name');
      edit(form.elements.name, 'Error checks');
      edit(form.elements.bio, 'b'.repeat(501));
      form.requestSubmit();
      await wait(() => form.elements.bio.getAttribute('aria-invalid') === 'true' && settled());
      assertField(form.elements.bio);
      assertPanel(form.querySelector('#profile-errors'));
      edit(form.elements.bio, 'A short bio');
      edit(form.elements.imageURL, 'javascript:invalid');
      form.requestSubmit();
      await wait(() => form.elements.imageURL.getAttribute('aria-invalid') === 'true' && settled());
      assert(form.elements.imageURL.closest('details').open, 'An invalid image URL must be exposed for correction.');
      assertField(form.elements.imageURL);
      assert(getComputedStyle(form.elements.imageURL.closest('fieldset')).borderTopColor === 'rgb(161, 38, 38)', 'An invalid image URL must mark its picker.');

      form = await navigate('/media', '[data-media-upload]');
      form.querySelector('details').open = true;
      await nativeError(form, 'image');
      const files = new DataTransfer();
      files.items.add(new File(['invalid image'], 'invalid.png', {type: 'image/png'}));
      form.elements.image.files = files.files;
      form.elements.image.dispatchEvent(new Event('change', {bubbles: true}));
      form.requestSubmit();
      await wait(() => form.elements.image.getAttribute('aria-invalid') === 'true' && settled());
      assertField(form.elements.image);
      assertPanel(form.querySelector('.form-error'));

      const detail = document.querySelector('.media-card').getAttribute('href');
      form = await navigate(detail, '.media-label-form');
      edit(form.elements.tags, 't'.repeat(51));
      sessionStorage.setItem('form-errors-stage', 'media');
      form.requestSubmit();
      return;
    }
    assert(stage === 'media', 'Unknown form error test stage.');
    let form = document.querySelector('.media-label-form');
    assertField(form.elements.tags);
    assertPanel(form.querySelector('.form-error'));
    edit(form.elements.tags, 'valid');
    assert(!form.elements.tags.hasAttribute('aria-invalid'), 'Media field errors must clear on edit.');
    form = await navigate('/auth/change/email', '#change-password-form');
    edit(form.elements.oldPassword, 'wrong');
    edit(form.elements.newPassword, 'short');
    edit(form.elements.newPasswordMatch, 'different');
    form.requestSubmit();
    await wait(() => document.querySelector('#newPassword-error') && settled());
    assertField(document.querySelector('#newPassword'));
    const requestSource = document.querySelector('#change-password-form');
    htmx.trigger(requestSource, 'htmx:sendError', {elt: requestSource, target: requestSource});
    assertPanel(document.querySelector('[data-request-error]'));
    await report('PASS');
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
