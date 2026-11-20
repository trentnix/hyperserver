(async () => {
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const wait = async predicate => {
    const end = Date.now() + 12000;
    while (!predicate()) { if (Date.now() > end) throw new Error('Timed out: ' + predicate); await new Promise(resolve => setTimeout(resolve, 25)); }
  };
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  document.addEventListener('securitypolicyviolation', event => report('CSP violation: ' + event.violatedDirective + ' ' + event.blockedURI));
  try {
    const presentationMode = typeof testPresentationMode !== 'undefined' && testPresentationMode;
    const stage = sessionStorage.getItem('pdf-stage');
    if (!stage) {
      await wait(() => document.querySelector('#post-form .CodeMirror') && document.querySelector('[data-post-component="pdf-preview"] textarea[hidden]'));
      const form = document.querySelector('#post-form');
      const section = form.querySelector('[data-post-component="pdf-preview"]');
      form.elements.title.value = 'PDF preview review';
      form.elements.slug.value = 'pdf-preview-review';
      form.querySelector('[data-component-picker] summary').click();
      await addTestComponent(form.querySelector('[data-component-add="pdf-preview"]'));
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
      assert(!section.querySelector('ol, li, .download-type, [data-downloads-count], .download-actions'), 'The single PDF editor must not show a file list, type badge, count, or a second remove action.');
      assert(section.querySelector('[data-pdf-document] a').href.endsWith('?preview=1') && section.querySelector('[data-download-upload-label]').textContent === 'Replace PDF', 'The selected PDF needs an Open link and a replacement action.');
      const firstOption = section.querySelector('input[type="checkbox"]');
      assert(firstOption?.checked && firstOption.closest('label').textContent === 'Show download link', 'Existing PDFs must show a clearly labeled download option, checked by default.');
      const presentation = section.querySelector('.pdf-editor-presentation-option input');
      assert(presentation && !presentation.checked, 'Existing PDFs must default to the native viewer.');
      const presentationTitle = document.getElementById(presentation.getAttribute('aria-labelledby'));
      const presentationHint = document.getElementById(presentation.getAttribute('aria-describedby'));
      const titleBounds = presentationTitle.getBoundingClientRect();
      const hintBounds = presentationHint.getBoundingClientRect();
      const presentationBounds = presentation.closest('label').getBoundingClientRect();
      assert(presentationTitle.textContent === 'Presentation mode' && Math.abs(hintBounds.left - titleBounds.left) < 1 &&
        hintBounds.top >= titleBounds.bottom && hintBounds.top - titleBounds.bottom < 8 &&
        hintBounds.bottom <= presentationBounds.bottom && presentationBounds.height >= 44,
        'The presentation hint must sit directly below its title within the same touch target.');
      if (presentationMode) {
        presentationHint.click();
        assert(presentation.checked, 'The presentation hint must toggle its own checkbox.');
      }
      const authorDescription = section.querySelector('[data-downloads-list] textarea');
      authorDescription.value = 'An author-written description to retain when replacing the PDF.';
      authorDescription.dispatchEvent(new Event('input', {bubbles: true}));
      firstOption.click();
      const saved = form.elements.pdf.value;
      const invalid = new DataTransfer();
      invalid.items.add(new File(['Not a PDF'], 'invalid.pdf', {type: 'application/pdf'}));
      upload.files = invalid.files;
      upload.dispatchEvent(new Event('change', {bubbles: true}));
      await wait(() => !form.dataset.uploading && section.querySelector('[data-downloads-status]').textContent.includes('valid PDF'));
      assert(form.elements.pdf.value === saved, 'A failed replacement must keep the original PDF and its settings.');
      const data = new DataTransfer();
      data.items.add(new File([Uint8Array.from(atob(testPDFData), char => char.charCodeAt(0)), '\n% Replacement copy\n'], 'uploaded-preview.pdf', {type: 'application/pdf'}));
      upload.files = data.files;
      upload.dispatchEvent(new Event('change', {bubbles: true}));
      await wait(() => !form.dataset.uploading && form.elements.pdf.value !== saved);
      const replacement = JSON.parse(form.elements.pdf.value)[0];
      const original = JSON.parse(saved)[0];
      assert(replacement.id !== original.id && replacement.label === original.label && replacement.description === original.description && replacement.hideDownload === true && Boolean(replacement.presentation) === presentationMode && !section.querySelector('[data-downloads-recovery]').hidden, 'Replacement must keep author details and offer Undo.');
      section.querySelector('[data-download-undo]').click();
      assert(form.elements.pdf.value === saved, 'Undo must restore the PDF.');
      assert(!section.querySelector('input[type="checkbox"]').checked, 'Undo must retain the download setting.');
      assert(document.activeElement === section.querySelector('[data-downloads-list] input'), 'Undo must return focus to the restored PDF fields.');
      section.querySelector('[data-download-browse]').click();
      await wait(() => document.querySelector('.download-library input[type="radio"]'));
      document.querySelector('.download-library .form-actions button:first-child').click();
      await wait(() => !document.querySelector('.download-library'));
      assert(form.elements.pdf.value === saved, 'Canceling the library must preserve the current PDF.');
      section.querySelector('[data-download-browse]').click();
      await wait(() => document.querySelector('.download-library input[type="radio"]'));
      const replacementRow = [...document.querySelectorAll('.download-library li')].find(row => row.textContent.includes('uploaded-preview.pdf'));
      replacementRow.querySelector('input').click();
      document.querySelector('.download-library .form-actions button:last-child').click();
      await wait(() => !document.querySelector('.download-library'));
      assert(JSON.parse(form.elements.pdf.value).length === 1 && JSON.parse(form.elements.pdf.value)[0].id === replacement.id, 'Library selection must replace the PDF without adding a second document.');
      const name = section.querySelector('[data-downloads-list] input');
      name.value = 'A document with a title that wraps cleanly on a narrow mobile screen';
      name.dispatchEvent(new Event('input', {bubbles: true}));
      const description = section.querySelector('[data-downloads-list] textarea');
      description.value = 'A sample PDF with two pages.\nOpen or download the original file.';
      description.dispatchEvent(new Event('input', {bubbles: true}));
      const option = section.querySelector('input[type="checkbox"]');
      assert(!option.checked, 'Replacement must preserve the existing download setting.');
      option.click();
      assert(!JSON.parse(form.elements.pdf.value)[0].hideDownload, 'Checking the option must restore the default.');
      option.click();
      assert(JSON.parse(form.elements.pdf.value)[0].hideDownload === true, 'Unchecking the option must save the preference.');
      assert(!section.querySelector('[data-download-upload]').disabled && !section.querySelector('[data-download-browse]').disabled && JSON.parse(form.elements.pdf.value).length === 1, 'A single PDF must remain replaceable.');
      form.querySelector('[data-component-picker] summary').click();
      await addTestComponent(form.querySelector('[data-component-add="file-downloads"]'));
      const downloads = form.querySelector('[data-post-component="file-downloads"]');
      downloads.querySelector('[data-download-browse]').click();
      await wait(() => document.querySelector('.download-library input[type="checkbox"]'));
      const rows = [...document.querySelectorAll('.download-library li')];
      rows.find(row => row.textContent.includes('preview.pdf')).querySelector('input').click();
      document.querySelector('.download-library .form-actions button:last-child').click();
      const ids = [...form.querySelectorAll('[id]')].map(node => node.id);
      assert(new Set(ids).size === ids.length, 'The same file in two components must not duplicate field IDs.');
      await moveTestComponent(section, 'down');
      assert(form.elements.componentOrder.value === '["file-downloads","pdf-preview"]', 'The PDF component must participate in ordering.');
      await removeTestComponent(section.querySelector('[data-component-remove]'));
      await undoTestComponent(form.querySelector('[data-component-undo="pdf-preview"]'));
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
      await wait(() => document.querySelector('.pdf-preview'));
      const section = document.querySelector('.pdf-preview');
      assert(section.textContent.includes('two pages'), 'The saved description must render.');
      assert(document.documentElement.scrollWidth <= innerWidth + 1, 'PDF content must fit the viewport.');
      assert(document.querySelector('.file-downloads').compareDocumentPosition(section) & Node.DOCUMENT_POSITION_FOLLOWING, 'PDF order must survive saving.');
      const open = section.querySelector('a[target="_blank"]');
      assert(!section.querySelector('a[download]'), 'Clearing Show download link must hide the preview download link.');
      const download = document.querySelector('.file-downloads a[download]');
      const inlineResponse = await fetch(open.href);
      const attachment = await fetch(download.href);
      assert(inlineResponse.headers.get('Content-Disposition').startsWith('inline') && attachment.headers.get('Content-Disposition').startsWith('attachment'), 'Open PDF and Download need distinct browser behaviors.');
      if (presentationMode) {
        const viewer = section.querySelector('[data-pdf-presentation]');
        assert(viewer, 'The saved presentation option must select the presentation renderer.');
        viewer.scrollIntoView({block: 'center'});
        const page = viewer.querySelector('[data-pdf-page]');
        await wait(() => page.textContent === 'Page 1 of 2');
        const previous = viewer.querySelector('[data-pdf-previous]');
        const next = viewer.querySelector('[data-pdf-next]');
        assert(previous.disabled && !next.disabled, 'Page navigation must respect the first page.');
        assert(viewer.querySelector('canvas').width > 0 && !viewer.querySelector(':scope > iframe'), 'Presentation must render a canvas without loading a second native viewer.');
        const text = viewer.querySelector('[data-pdf-text]');
        await wait(() => text.textContent.trim());
        const firstText = text.textContent;
        next.click();
        await wait(() => page.textContent === 'Page 2 of 2');
        await wait(() => text.textContent.trim() && text.textContent !== firstText);
        assert(!previous.disabled && next.disabled, 'Page navigation must respect the last page.');
        viewer.focus();
        const key = value => viewer.dispatchEvent(new KeyboardEvent('keydown', {key: value, bubbles: true, cancelable: true}));
        key('Home');
        await wait(() => page.textContent === 'Page 1 of 2');
        key('End');
        await wait(() => page.textContent === 'Page 2 of 2');
        key('ArrowLeft'); key('ArrowRight'); key('ArrowLeft');
        await wait(() => page.textContent === 'Page 1 of 2' && !viewer.hasAttribute('aria-busy'));
        const canvas = viewer.querySelector('canvas').getBoundingClientRect();
        const bounds = viewer.querySelector('[data-pdf-slide]').getBoundingClientRect();
        assert(canvas.width <= bounds.width + 1 && canvas.height <= bounds.height + 1, 'Each complete page must fit its presentation area.');
        const fullscreen = viewer.querySelector('[data-pdf-fullscreen]');
        assert(fullscreen.hidden === !document.fullscreenEnabled, 'Fullscreen should only be offered when the browser supports it.');
        assert(getComputedStyle(fullscreen.querySelector('.pdf-fullscreen-enter')).display !== 'none' && getComputedStyle(fullscreen.querySelector('.pdf-fullscreen-exit')).display === 'none', 'The fullscreen button should show only the enter icon in the inline viewer.');
        assert(document.documentElement.scrollWidth <= innerWidth + 1, 'Presentation controls must fit narrow viewports.');
      } else {
        const frame = section.querySelector('iframe');
        assert(frame.title.includes('document with a title') && frame.getBoundingClientRect().height >= 300, 'The viewer needs a title and useful bounded height.');
        frame.scrollIntoView({block: 'center'});
        // Firefox owns the native viewer in a privileged document.
        await wait(() => frame.contentDocument === null);
        assert(!performance.getEntriesByType('resource').some(resource => resource.name.includes('/lib/pdfjs/')), 'Ordinary PDF previews must not load presentation libraries.');
      }
      const oldCanvas = section.querySelector('[data-pdf-slide] canvas');
      await htmx.ajax('GET', document.querySelector('a[href^="/post/update/"]').getAttribute('href'), {target: '#article-content', swap: 'innerHTML'});
      await wait(() => document.querySelector('[data-post-component="pdf-preview"] input'));
      assert(document.querySelector('#post-form').elements.pdf.value.includes('two pages'), 'Reopening the editor must retain the PDF fields.');
      assert(!document.querySelector('[data-post-component="pdf-preview"] input[type="checkbox"]').checked, 'Reopening the editor must retain the download setting.');
      const restoredPresentation = document.querySelector('.pdf-editor-presentation-option input');
      assert(restoredPresentation.checked === presentationMode, 'Reopening the editor must retain the presentation setting.');
      if (oldCanvas) assert(oldCanvas.width === 0, 'HTMX navigation must release the previous presentation canvas.');
      await report('PASS');
    }
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
