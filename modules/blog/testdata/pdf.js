(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const wait = async predicate => {
    const end = Date.now() + 12000;
    while (!predicate()) { if (Date.now() > end) throw new Error('Timed out: ' + predicate); await new Promise(resolve => setTimeout(resolve, 25)); }
  };
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    const stage = sessionStorage.getItem('pdf-stage');
    if (!stage) {
      await wait(() => document.querySelector('#post-form .CodeMirror') && document.querySelector('[data-post-component="pdf-preview"] textarea[hidden]'));
      const form = document.querySelector('#post-form');
      const section = form.querySelector('[data-post-component="pdf-preview"]');
      form.elements.title.value = 'PDF preview review';
      form.elements.slug.value = 'pdf-preview-review';
      form.querySelector('[data-component-picker] summary').click();
      form.querySelector('[data-component-add="pdf-preview"]').click();
      assert(!section.hidden && document.activeElement === section.querySelector('[data-download-upload]'), 'Adding a PDF must reveal its editor and focus Upload.');
      const upload = section.querySelector('[data-download-files]');
      assert(!upload.multiple && upload.accept === '.pdf', 'The PDF component must accept one PDF.');
      section.querySelector('[data-download-browse]').click();
      await wait(() => document.querySelector('.download-library input[type="radio"]'));
      const library = document.querySelector('.download-library');
      assert(library.querySelectorAll('li').length === 1 && library.querySelector('input[type="radio"]'), 'The library must show only PDFs with a single choice.');
      library.querySelector('input[type="radio"]').click();
      library.querySelector('.form-actions button:last-child').click();
      assert(JSON.parse(form.elements.pdf.value).length === 1 && !form.elements.content.required, 'PDF-only posts must be allowed.');
      const firstOption = section.querySelector('input[type="checkbox"]');
      assert(firstOption?.checked && firstOption.closest('label').textContent === 'Show download link', 'Existing PDFs must show a clearly labeled download option, checked by default.');
      firstOption.click();
      const saved = form.elements.pdf.value;
      section.querySelector('[data-downloads-list] button:last-child').click();
      assert(!form.elements.pdf.value && !section.querySelector('[data-downloads-recovery]').hidden, 'Removing the PDF must offer Undo.');
      section.querySelector('[data-download-undo]').click();
      assert(form.elements.pdf.value === saved, 'Undo must restore the PDF.');
      assert(!section.querySelector('input[type="checkbox"]').checked, 'Undo must retain the download setting.');
      section.querySelector('[data-downloads-list] button:last-child').click();
      const data = new DataTransfer();
      data.items.add(new File([Uint8Array.from(atob(testPDFData), char => char.charCodeAt(0))], 'uploaded-preview.pdf', {type: 'application/pdf'}));
      upload.files = data.files;
      upload.dispatchEvent(new Event('change', {bubbles: true}));
      await wait(() => !form.dataset.uploading && form.elements.pdf.value);
      const name = section.querySelector('[data-downloads-list] input');
      name.value = 'A document with a title that wraps cleanly on a narrow mobile screen';
      name.dispatchEvent(new Event('input', {bubbles: true}));
      const description = section.querySelector('[data-downloads-list] textarea');
      description.value = 'A sample PDF with two pages.\nOpen or download the original file.';
      description.dispatchEvent(new Event('input', {bubbles: true}));
      const option = section.querySelector('input[type="checkbox"]');
      assert(option.checked, 'New uploads must show a download link by default.');
      option.click();
      assert(JSON.parse(form.elements.pdf.value)[0].hideDownload === true, 'Unchecking the option must save the preference.');
      option.click();
      assert(!JSON.parse(form.elements.pdf.value)[0].hideDownload, 'Checking the option must restore the default.');
      option.click();
      assert(section.querySelector('[data-download-upload]').disabled && section.querySelector('[data-download-browse]').disabled, 'Only one PDF may be embedded in this component.');
      form.querySelector('[data-component-picker] summary').click();
      form.querySelector('[data-component-add="file-downloads"]').click();
      const downloads = form.querySelector('[data-post-component="file-downloads"]');
      downloads.querySelector('[data-download-browse]').click();
      await wait(() => document.querySelector('.download-library input[type="checkbox"]'));
      const rows = [...document.querySelectorAll('.download-library li')];
      rows.find(row => row.textContent.includes('preview.pdf')).querySelector('input').click();
      document.querySelector('.download-library .form-actions button:last-child').click();
      const ids = [...form.querySelectorAll('[id]')].map(node => node.id);
      assert(new Set(ids).size === ids.length, 'The same file in two components must not duplicate field IDs.');
      section.querySelector('[data-component-move="down"]').click();
      assert(form.elements.componentOrder.value === '["file-downloads","pdf-preview"]', 'The PDF component must participate in ordering.');
      section.querySelector('[data-component-remove]').click();
      form.querySelector('[data-component-undo="pdf-preview"]').click();
      assert(form.elements.componentOrder.value === '["file-downloads","pdf-preview"]' && form.elements.pdf.value.includes('two pages'), 'Component Undo must restore PDF details and position.');
      assert(!section.querySelector('input[type="checkbox"]').checked && !downloads.querySelector('input[type="checkbox"]'), 'Component Undo must retain the setting without adding the option to file downloads.');
      form.querySelector('[name="published"][value="on"]').checked = true;
      form.dispatchEvent(new Event('input', {bubbles: true}));
      await wait(() => Object.keys(localStorage).some(key => key.startsWith('hyperserver.blog.draft.v1:') && localStorage.getItem(key).includes('two pages')));
      sessionStorage.setItem('pdf-stage', 'recover');
      location.reload();
      return;
    }
    if (stage === 'recover') {
      await wait(() => document.querySelector('[data-draft-recovery]') && !document.querySelector('[data-draft-recovery]').hidden);
      document.querySelector('[data-draft-restore]').click();
      const form = document.querySelector('#post-form');
      await wait(() => !form.querySelector('[data-post-component="pdf-preview"]').hidden);
      assert(form.elements.pdf.value.includes('two pages') && form.elements.componentOrder.value === '["file-downloads","pdf-preview"]', 'Draft recovery must retain the PDF and component order.');
      assert(!form.querySelector('[data-post-component="pdf-preview"] input[type="checkbox"]').checked, 'Draft recovery must retain the download setting.');
      sessionStorage.setItem('pdf-stage', 'view');
      form.requestSubmit();
      return;
    }
    if (stage === 'view') {
      await wait(() => document.querySelector('.pdf-preview-frame'));
      const section = document.querySelector('.pdf-preview');
      const frame = section.querySelector('iframe');
      assert(frame.title.includes('document with a title') && frame.getBoundingClientRect().height >= 300, 'The viewer needs a title and useful bounded height.');
      assert(section.textContent.includes('two pages'), 'The saved description must render.');
      assert(document.documentElement.scrollWidth <= innerWidth + 1, 'PDF content must fit the viewport.');
      assert(document.querySelector('.file-downloads').compareDocumentPosition(section) & Node.DOCUMENT_POSITION_FOLLOWING, 'PDF order must survive saving.');
      const open = section.querySelector('a[target="_blank"]');
      assert(!section.querySelector('a[download]'), 'Clearing Show download link must hide the preview download link.');
      const download = document.querySelector('.file-downloads a[download]');
      const inlineResponse = await fetch(open.href);
      const attachment = await fetch(download.href);
      assert(inlineResponse.headers.get('Content-Disposition').startsWith('inline') && attachment.headers.get('Content-Disposition').startsWith('attachment'), 'Open PDF and Download need distinct browser behaviors.');
      frame.scrollIntoView({block: 'center'});
      // Firefox owns the native viewer in a privileged document. Page scripts cannot inspect its controls.
      await wait(() => frame.contentDocument === null);
      await htmx.ajax('GET', document.querySelector('a[href^="/post/update/"]').getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
      await wait(() => document.querySelector('[data-post-component="pdf-preview"] input'));
      assert(document.querySelector('#post-form').elements.pdf.value.includes('two pages'), 'Reopening the editor must retain the PDF fields.');
      assert(!document.querySelector('[data-post-component="pdf-preview"] input[type="checkbox"]').checked, 'Reopening the editor must retain the download setting.');
      await report('PASS');
    }
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
