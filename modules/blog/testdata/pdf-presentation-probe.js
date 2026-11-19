import {getDocument as loadDocument} from './pdf.min.mjs?original';
export {GlobalWorkerOptions} from './pdf.min.mjs?original';

// Run the actual PDF.js renderer, recording text reads and page cleanup.
const probe = window.pdfProbe = {textReads: [], cleanedPages: [], holdText: true};

export function getDocument(options) {
  const task = loadDocument(options);
  task.promise.then(document => {
    const getPage = document.getPage.bind(document);
    const observed = new WeakSet();
    document.getPage = async number => {
      if (probe.failPage === number) throw new Error('Test page load failed.');
      const page = await getPage(number);
      if (observed.has(page)) return page;
      observed.add(page);
      const getText = page.getTextContent.bind(page);
      const cleanup = page.cleanup.bind(page);
      page.getTextContent = async (...args) => {
        probe.textReads.push(number);
        if (probe.holdText) await new Promise(resolve => { probe.releaseText = resolve; });
        return getText(...args);
      };
      page.cleanup = (...args) => {
        const cleaned = cleanup(...args);
        if (cleaned) probe.cleanedPages.push(number);
        return cleaned;
      };
      return page;
    };
  }, () => {});
  return task;
}
