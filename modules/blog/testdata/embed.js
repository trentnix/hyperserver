(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', resolve, {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const wait = async predicate => {
    const deadline = Date.now() + 12000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error('Timed out: ' + predicate);
      await new Promise(resolve => setTimeout(resolve, 25));
    }
  };
  const url = 'https://widgets.example.test/demo?theme=light&scale=2';
  const title = 'Interactive chart';
  const description = 'Explore the **yearly changes**.\n\nTry hovering over the chart.';
  const checkFrame = async frame => {
    await wait(() => frame.contentDocument?.querySelector('iframe'));
    const inner = frame.contentDocument.querySelector('iframe');
    assert(frame.title === title && inner.title === title && inner.src === url, 'The embed must preserve the destination and accessible title.');
    assert(inner.sandbox.value === 'allow-scripts' && inner.referrerPolicy === 'no-referrer', 'The provider must run in an isolated frame.');
    assert(frame.getBoundingClientRect().width <= innerWidth && frame.getBoundingClientRect().height >= 280, 'The frame must fit the viewport and allow room to interact.');
  };
  try {
    if (sessionStorage.getItem('embed-saved')) {
      const section = document.querySelector('.web-embed');
      const frame = section.querySelector('iframe');
      frame.scrollIntoView();
      await checkFrame(frame);
      const introduction = section.querySelector('.web-embed-description');
      assert(introduction.querySelector('strong').textContent === 'yearly changes' && introduction.getBoundingClientRect().bottom < frame.getBoundingClientRect().top, 'The Markdown description must appear above the embed.');
      const open = section.querySelector(':scope > p > a');
      assert(open.href === url && open.target === '_blank' && open.textContent === 'Open ' + title, 'Readers must have a direct viewing link.');
      const edit = document.querySelector('a[href^="/post/update/"]');
      await htmx.ajax('GET', edit.getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
      await wait(() => document.querySelector('[data-embed-editor] textarea').hidden);
      assert(document.querySelector('#embed-url').value === url && document.querySelector('#embed-title').value === title && document.querySelector('#embed-description').value === description, 'Editing must restore the saved embed.');
      assert(document.querySelector('#embed-editor-preview').hidden, 'The editor must only load the provider on request.');
      await report('PASS');
      return;
    }

    await wait(() => document.querySelector('#post-form .CodeMirror') && document.querySelector('[name=embed]').hidden);
    const form = document.querySelector('#post-form');
    const section = form.querySelector('[data-embed-editor]');
    const urlField = section.querySelector('#embed-url');
    const titleField = section.querySelector('#embed-title');
    const descriptionField = section.querySelector('#embed-description');
    const preview = section.querySelector('#embed-editor-preview');
    const button = section.querySelector('[data-embed-preview-button]');
    const set = (field, value) => {
      field.value = value;
      field.dispatchEvent(new Event('input', {bubbles: true}));
    };
    form.elements.title.value = 'Embed review';
    form.elements.slug.value = 'embed-review';
    form.querySelector('[data-component-picker] summary').click();
    await addTestComponent(form.querySelector('[data-component-add=embed]'));
    assert(!section.hidden && document.activeElement === urlField, 'Adding an embed must focus its URL.');
    const urlRect = urlField.getBoundingClientRect();
    const titleRect = titleField.getBoundingClientRect();
    const descriptionEditor = section.querySelector('.compact-markdown-editor');
    const descriptionRect = descriptionEditor.getBoundingClientRect();
    const buttonRect = button.getBoundingClientRect();
    assert(titleRect.top > urlRect.bottom && descriptionRect.top > titleRect.bottom && buttonRect.top > descriptionRect.bottom, 'Fields and Preview must stack in reading order at every viewport width.');
    assert(buttonRect.height >= 44, 'Preview must retain a touch-sized button.');
    set(urlField, url);
    set(titleField, '  ');
    form.requestSubmit();
    await wait(() => document.activeElement === titleField && titleField.getAttribute('aria-invalid') === 'true');
    form.noValidate = true;
    form.requestSubmit();
    await wait(() => section.querySelector('[data-component-error]:not([hidden])')?.textContent.includes('embed title'));
    assert(document.activeElement === titleField && titleField.getAttribute('aria-invalid') === 'true' && !urlField.hasAttribute('aria-invalid'), 'A server-rejected title must be marked and focused.');
    form.noValidate = false;
    set(titleField, title);
    assert(!titleField.hasAttribute('aria-invalid'), 'Editing the title must clear its error.');
    set(urlField, 'https://widgets.example.test/' + 'é'.repeat(500));
    button.click();
    await wait(() => document.activeElement === urlField && urlField.validationMessage.includes('2,048'));
    assert(preview.hidden && !preview.querySelector('iframe').hasAttribute('src'), 'An oversized normalized URL must get a field error before opening the preview.');

    set(urlField, 'https://other.example.test/demo');
    assert(preview.hidden, 'Typing a URL must leave the preview closed.');
    button.click();
    const frame = preview.querySelector('iframe');
    const status = preview.querySelector('[role=status]');
    const link = preview.querySelector('[data-embed-preview-link]');
    await wait(() => status.textContent.includes('Readers will see a link'));
    assert(frame.parentElement.hidden && !frame.hasAttribute('src') && !link.hidden && link.href === urlField.value && link.target === '_blank', 'An unapproved URL must show an inline status and link without opening a frame.');
    set(urlField, url);
    assert(preview.hidden && !frame.hasAttribute('src'), 'Changing the URL must clear the stale preview.');
    button.click();
    await checkFrame(frame);
    assert(button.getAttribute('aria-expanded') === 'true', 'The preview button must expose the open state.');
    const frameDocument = frame.contentDocument;
    const inner = frameDocument.querySelector('iframe');
    const provider = inner.contentWindow;
    const frameSource = frame.src;
    set(titleField, 'Revised preview title');
    assert(!preview.hidden && frame.src === frameSource && frame.contentDocument === frameDocument && inner.contentWindow === provider && frame.title === titleField.value && inner.title === titleField.value, 'A title edit must update the labels and preserve the running frame.');
    set(titleField, title);
    section.querySelector('.compact-markdown-editor .CodeMirror').CodeMirror.setValue(description);
    assert(!preview.hidden && frame.contentDocument === frameDocument && inner.contentWindow === provider, 'Editing the description must preserve the running preview.');
    button.click();
    assert(preview.hidden && !frame.hasAttribute('src'), 'Hide preview must close and unload the frame.');
    await removeTestComponent(section.querySelector('[data-component-remove]'));
    await undoTestComponent(form.querySelector('[data-component-undo=embed]'));
    assert(!section.hidden && urlField.value === url && titleField.value === title && descriptionField.value === description && preview.hidden, 'Undo must restore the fields with the preview closed.');
    assert(document.documentElement.scrollWidth <= innerWidth + 1, 'The editor must fit at narrow widths.');
    form.querySelector('[name=published][value=on]').checked = true;
    sessionStorage.setItem('embed-saved', 'true');
    form.requestSubmit();
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
