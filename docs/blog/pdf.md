# PDF previews

Use **Add component → PDF preview** below a post's Markdown editor. Upload one PDF or choose one from the library. Edit its display name and optional description. Save the post to apply changes.

Readers see the document in their browser's PDF viewer. **Open PDF** opens a separate tab when inline viewing is unavailable. **Download** saves the original file. Clear **Show download link** beneath the description to hide Download above the preview. The checkbox is checked by default, including for existing previews. The browser's viewer may still offer its own download control.

PDF previews use the same rounded container as photo albums and downloads. The viewer has a bounded height, with a shorter height on mobile. Authors can move, remove, and undo the component. Draft recovery and revision history retain the file, display name, description, download link setting, and component order. Posts can contain a PDF without body text. Page editing does not expose the component yet.

## Files and access

A preview accepts one PDF, up to 32 MiB. It shares [file downloads](downloads.md) storage, the 1 GiB site limit, upload validation, ownership, cleanup, and backups. The same uploaded PDF can appear in a preview and a downloads component without storing another copy.

`/files/{id}` serves attachments. `/files/{id}?preview=1` serves PDFs inline after the same access checks. Both support HEAD, conditional requests, and byte ranges. Private files stay private, including draft-only and future scheduled references. Unpublishing the last public reference revokes anonymous access.

The blog permits same-origin frames. Only PDF preview responses allow same-origin embedding. Ordinary pages and attachment responses retain their framing restrictions. See [frame-src](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy/frame-src), [frame-ancestors](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy/frame-ancestors), and [Content-Disposition](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Disposition).

## Saved format

PDF previews use the existing version 2 `post-components` block. A `pdf-preview` record contains one entry in `files`, using the same ID, label, description, and byte count as a download. The optional `hideDownload` boolean on that entry controls the preview's Download link. An omitted or false value keeps the link visible. File downloads components always show their download links. The component follows the author's chosen order. No database migration is required.

Search includes the display name and description. It does not extract text from the PDF. Feeds and historical previews show document links and descriptions without an embedded viewer. Earlier binaries preserve the unsupported component source but cannot render or save it as a PDF component.
