# File downloads

Use **Add component → File downloads** below a post's Markdown editor. **Upload** accepts multiple files from your device. **Library** reuses uploaded files. Edit each download's display name and optional description. The downloaded file keeps its original filename. Use the arrows to change the order. Removing a file or the component offers **Undo** and keeps the uploaded bytes. Save the post to apply changes.

Components appear in the order you add them, below the Markdown body. Use the component heading’s up/down buttons to move the whole component. Undo restores a removed component at its original position. Adding the component again starts an empty component at the end.

A post can contain downloads, a photo album, or both without body text. File downloads are available on posts. Page editing does not expose the component yet.

## Limits

- Up to 20 downloads per post.
- PDF, ZIP, or UTF-8 text files with `.pdf`, `.zip`, or `.txt` extensions.
- Up to 32 MiB per file and 1 GiB of stored download files per site. Images have separate limits.

The server checks PDF signatures and trailers, ZIP directory structure, and text encoding. It ignores the submitted MIME type and never extracts archives. These checks do not scan files for malware. Accepted files are served as attachments with `nosniff`.

Each editor queues uploads and blocks Save until the batch finishes. Failed uploads identify files that were not added. Successful uploads and unsaved edits remain available. Retry attempts only files that were not added. Browser recovery retains selected files, names, descriptions, and order.

If a reverse proxy limits request bodies, allow at least 34 MiB for `/post/files/upload`. For Nginx, `client_max_body_size 34m;` meets the application's request limit. The application still enforces its 32 MiB file limit.

## Access and retention

New uploads are private. Admins can download all files. Posters can download files they uploaded, files referenced by any saved version of their posts, and files used by public content. Library choices follow the same rules. Re-uploading identical bytes preserves the first uploader and original filename.

Anonymous downloads require a reference from current public content. Drafts, scheduled posts before publication, and history do not grant public access. Removing the last public reference revokes access, including conditional and range requests. Public responses must revalidate. Downloads support HEAD, ranges, and resume.

**Media → Clean up uploads** includes download files. Current references protect files from deletion. History-only files require individual confirmation. Unsaved editors and browser recovery copies cannot protect uploads from cleanup.

## Storage

Files live under `uploadsDirectory/files`, using SHA-256 IDs and validated extensions. Download names never become storage paths. `blog_media` holds metadata. Post and page revision references use the existing shared media tables. No additional database migration is required.

Album-only posts keep their existing `photo-album` block. Posts with downloads use a final `post-components` block containing version 2 JSON with an ordered `components` list. Version 1 posts remain readable and editable in their original album-then-downloads order. Each download stores its immutable ID, name, description, and byte count. Saving checks the byte count against the stored file. Body search uses captions, names, and descriptions rather than JSON fields and IDs. Unsupported component versions remain visible as source and must be corrected before saving.

Existing [backups](backup.md) include the files directory, metadata, references, and saved versions. Restore the complete uploads directory with the matching databases.
