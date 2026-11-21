# Images and photo albums

## Upload and reuse

Admins use **Media** to upload and browse images. **Browse library** beside a preview/profile image and **Choose existing image** in the Markdown toolbar reuse uploads. Posters can upload in their post editor and reuse public images or images in their own saved posts. For other file types, see [File downloads](components.md#file-downloads).

Uploads accept PNG, JPEG, or GIF, up to 8 MiB and 20 million pixels per file. The original is retained. Display and thumbnail copies have maximum edges of 2,048 and 480 pixels, without enlarging smaller images. All three count toward the site's [shared upload limit](components.md#file-downloads).

The limit applies to images added through Media, Markdown, post and page previews, Site Profile, albums, comparisons, events, social media fallbacks, and link previews. It also applies when the server imports a thumbnail for a link preview. You can still use existing uploads from the library when storage is full.

JPEG uses quality 85. JPEG/PNG processing applies EXIF orientation and removes metadata. PNG preserves transparency. GIF retains its original animation and metadata. Animated PNG is rejected.

The server processes one image upload at a time. Editors queue uploads and block Save until their batch finishes. Failures preserve successful uploads. Other editors can receive a retry response. The optional [imaging package](../../pkg/imaging/image.go) handles processing. The blog owns limits, storage, and permissions.

## Add an album

Choose **Add component → Photo album**, then **Upload** or **Library**. Removing a photo or album offers Undo and keeps the upload. Save the post or page to apply changes. Albums hold up to 50 photos, with captions up to 500 characters. See [shared component behavior](components.md).

Select a photo to preview its unsaved caption and position. Press Enter or click outside a caption to finish editing. Pasted line breaks become spaces. Undo retains up to 50 removed photos during the editor session. Adding a removed photo again clears that undo entry. At capacity, Upload, Library, and Undo stay disabled until space is available.

Failed uploads identify the file and reason. Invalid images do not block later files. Authentication or service failures pause the batch. Retry attempts only failed or unprocessed files.

### Select library photos

Select cards, then choose **Add photos**. Selections survive searches and page changes. **Added to album** marks unavailable duplicates. At capacity, deselect a pending photo to choose another.

**Show selected** displays selections across pages. Clearing the last selection returns to browsing. Cancel or navigation away discards pending selections. Retry preserves them after a browsing error.

### Reorder photos

Choose **Reorder**, then drag or use arrow buttons. On touch screens, drag from the upper-left grip and swipe elsewhere to scroll. With a photo focused, Left/Right move one position, Up/Down move one row, and Home/End move to the start/end. A marker shows the drop position.

Caption previews show two lines. Select truncated text to expand it without moving neighboring cards. **Show less**, Escape, or a tap outside collapses it. **Done** applies the order to the unsaved post. **Cancel** or Escape discards the provisional order. Full captions are preserved.

## View an album

Mobile galleries use two columns when each card can be at least 140 pixels wide. Narrower galleries and single-photo albums use one column. Select a photo to open the viewer. Previous/Next, arrow keys, Home/End, and swipes change photos. The toolbar controls captions and thumbnails. Closing restores focus and scroll position.

**Read full caption** expands text below the photo, overlapping its lower edge when space is limited. Long captions scroll. **Show less** or Escape collapses them. Failed image loads offer Retry.

Zoom with its button, double-click, double-tap, mouse wheel, or pinch. Drag or use arrow keys to pan. Plus/minus adjust zoom, and `0` resets it. Panning and pinching do not change photos. Escape resets zoom before closing the viewer. Without JavaScript, cards link to individual images.

## Find images

Media and image choosers show 24 images per page. Search is literal and case-insensitive. Filenames, labels, and IDs rank first, tags second, and current or historical usage titles third. Newer uploads break ties. Usage filters distinguish current content, history-only references, and unused files. Current content includes drafts, schedules, pages, and Site Profile, but excludes unsaved editors and browser recovery copies.

Labels and comma-separated tags affect search, not URLs, alt text, access, or cleanup. Tags allow 20 values of 50 characters each. Missing files do not appear even when their metadata remains.

## Privacy and storage

New uploads are private to Admins and their uploader. Posters can also access images in saved versions of their own posts. Current published content and saved profile images make references public. Shared images stay public while any public reference remains. Unpublishing a page also hides references from its descendants.

Access checks run before bytes or conditional responses. Originals stay private to Admins and their uploader. Duplicate uploads never transfer ownership. Previously downloaded copies cannot be recalled.

Each image is a ZIP bundle containing the original, display copy, and thumbnail. The original's hash determines its ID and URL. Do not edit bundles in place. Keep `uploadsDirectory` on persistent storage outside public roots. Startup rejects overlap with `assetsDirectory`. Back up databases and uploads together.

See [component formats](component-development.md#saved-formats).

For unmanaged public files, set `assetsDirectory: assets` and create it. `portrait.jpg` becomes `/local-assets/portrait.jpg`. Only top-level, non-hidden files are served, without listings. These files are neither optimized nor cleaned up.

## Clean up uploads

**Media → Clean up uploads** lists unused and history-only images and files. Current references protect uploads, including drafts and schedules. Bulk deletion excludes history-only uploads. Individual deletion of a historical image shows affected versions before confirmation.

Bulk confirmation covers up to 5,000 files and their total size. Deletion rechecks references and excludes files added after confirmation. Newly referenced, re-uploaded, or missing files are skipped. Partial failures report deleted, skipped, and unprocessed counts. Deletion cannot overlap an upload or save.

Save editors before cleanup. Unsaved references and browser recovery copies are invisible to the server. If cleanup removes an unsaved upload, upload it again or remove the reference. Old-domain references are conservatively retained. Bundled assets, local assets, and backups are excluded.

## Reprocess existing uploads

Build `go build -o /tmp/optimize-images ./cmd/optimize-images` from the repository root. Back up uploads and stop the blog. Run `/tmp/optimize-images` from the application's configuration directory.

The optimizer preserves originals, URLs, and dates, replacing each bundle atomically. Failed files remain unchanged. Rerunning is safe. Reprocessing can change how much space the bundles use. The blog counts their new sizes when it next checks the upload limit. Legacy uploads are not served publicly until processed. See [backup and recovery](backup.md).
