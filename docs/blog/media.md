# Images

## Upload and reuse

Use **Media** to upload images and browse existing files. Use **Browse library** beside a preview/profile image or **Choose existing image** in the Markdown toolbar to reuse one. For these single-image controls, clicking a chooser card selects it and returns to the editor. Selection takes effect when you save.

Media management and the library chooser require Admin access. Posters can upload images directly in their post editor and reuse public images or images already in their own saved posts.

Uploads accept PNG, JPEG, or GIF, up to 8 MiB and 20 million pixels. The blog keeps the original and generates display and thumbnail copies with maximum edges of 2,048 and 480 pixels. Small images are not enlarged. JPEG copies use quality 85. PNG keeps transparency. JPEG/PNG copies apply EXIF orientation and remove metadata, including location data. GIF retains its original bytes, animation, and metadata. Animated PNG is rejected.

Only one upload is processed at a time. A concurrent upload receives a retry message. Image processing uses the framework's optional [imaging package](../../pkg/imaging/image.go). The blog owns limits, storage, access rules, and editor behavior.

## Post albums

Use **Add component → Photo album** below the Markdown editor. Posts without an album show only the Add component control. Existing albums appear automatically when you edit a post. **Remove album** removes the component from the unsaved post, and **Undo** restores it. Adding Photo album again starts an empty album. Removing a component does not delete its uploaded files.

In an album, **Upload** selects files from your device. **Library** selects existing photos. Device uploads accept several files and start automatically. Progress appears beside the controls, and uploads keep the active caption and cursor in place. File requirements appear below the photo cards.

Select a photo to preview the full image without saving or leaving the editor. The viewer includes your unsaved captions and photo order. Caption fields expand over the content below them, leaving photo rows in place. Press Enter or click outside a caption to finish editing. Enter keeps the text without submitting the post. Pasted line breaks become spaces. Use the button in the upper right to remove a photo.

Confirm library selections with **Add photos**. The count shows your pending selection, such as **3 photos selected**, and the button reads **Add 3 photos**. A note states the 50-photo album limit. When the selection fills the album, unselected photos show a lock in their selection control. Their previews stay visible. Deselect a photo to choose another.

Selections stay selected while you search or change pages. **Show selected** displays your pending photos from every page. Click a photo to deselect it. Deselecting the last photo returns to the library automatically. **Clear selection** clears all selections and returns to the library. **Back to library** restores your previous browsing position. Photos already included show **Added to album** over the preview and cannot be selected again.

If browsing fails, **Retry** repeats the failed request and keeps pending selections. Changing or clearing selections leaves the browsing error and Retry action visible. A successful reload clears the browsing error.

In short landscape windows, the chooser uses nearly the full height and tighter spacing. The photo list scrolls while Cancel, selection controls, and Retry remain visible.

Cancel closes the chooser without adding photos. Navigating away from the editor also closes the chooser and discards pending selections. Near the 50-photo limit, the controls show the remaining capacity. Upload and Library are disabled when the album is full. Removing a photo enables them again.

Use **Reorder** to drag photos or move them with the arrow buttons. On touch screens, drag from the grip in the upper left. Swipe elsewhere on a photo to scroll. A marker shows where the photo will go when released. The dialog fits short albums and scrolls for larger ones. On wide screens, the grid runs left to right, then down.

With a keyboard, focus a photo and use Left/Right to move one position or Up/Down to move one row. Home and End move it to the start or end. Moved photos briefly highlight so you can follow them.

Caption previews show up to two lines. Short captions stay plain text. If a caption is cut off, tap it or focus it and press Enter to expand the text within its photo card. Long text scrolls without moving neighboring cards. Use **Show less**, Escape, or a tap outside the caption to collapse it. Reorder controls reference their photo's caption for screen readers. Reordering preserves the full caption.

**Done** applies a changed order to the unsaved post. If the order is unchanged, **Done** closes without changing the post. **Cancel** or Escape discards the provisional order.

Use the remove icon beside a photo to remove it. Focus moves to another remove button so caption fields stay collapsed. **Undo** restores recent removals one at a time, including after adding photos or reordering. Recovery lasts for the current editor session and retains up to 50 removals. Adding a removed photo again clears its recovery entry. If the album is full, remove a photo before using **Undo**.

Save the post to apply changes. Each album accepts up to 50 photos with captions of at most 500 characters. Removing a photo from an album does not delete its upload.

Failed uploads show the filename and reason. Valid files still upload if another image is rejected. Authentication or service failures pause the remaining batch. Retrying keeps photos already added and attempts only failed or unprocessed files.

Readers see photo cards below the post, with captions attached to their images. Mobile albums use two columns when each card can be at least 140 pixels wide. Narrower galleries and single-photo albums use one column. Mobile grid captions show up to two lines. The viewer keeps the full caption.

Selecting a card opens a viewer that fills the available viewport, with the caption below the photo and aligned to its left edge. Previous/Next arrows, left/right arrow keys, Home, End, and touch swipes change photos. The toolbar can hide captions or the numbered thumbnail strip. On phones, the thumbnail strip starts collapsed. Closing returns focus and scroll position to the selected photo.

If a viewer caption does not fit, **Read full caption** expands it below the photo. If space is limited, the caption overlaps the photo's lower edge. The photo keeps its size. Use **Show less** or Escape to collapse the caption. Long text scrolls within the expanded caption.

While the next photo loads, the viewer keeps the previous photo and its caption together. Slow loads show a loading message. Failed loads hide the photo and caption and offer **Retry** for the same photo.

Use the zoom button, double-click, double-tap, mouse wheel, or pinch gesture to zoom. Drag a zoomed photo to pan, or use the arrow keys. Panning and pinch gestures do not advance the album. The plus and minus keys adjust zoom, and `0` resets it. Escape resets zoom first, then closes the viewer when pressed again. Without JavaScript, the grid links open individual images.

Albums are stored as Markdown images in the final top-level fenced block named `photo-album`. Earlier blocks and quoted or nested examples stay as code. Editing and rendering use the same rule. The editor manages the album block separately from the body. Without JavaScript, the album field accepts one Markdown image per line, such as `![Caption](/images/UPLOAD_ID)`. Albums use the same image access checks, saved versions, cleanup protection, feeds, and backups as the rest of the post. No database migration is needed.

## Find images

Media and the chooser show 24 images per page. Search matches filenames, labels, IDs, tags, and titles of current or historical content using an image. Matching is literal and case-insensitive. Names rank first, tags second, and content titles third. Newer uploads break ties.

Usage filters distinguish current content, history-only references, and unused files. Current content includes drafts, scheduled posts, pages, and the site profile. Unsaved editors and browser recovery copies are not included.

Labels and comma-separated tags help search. They do not change URLs, alt text, visibility, or cleanup rules. Tags allow 20 values of 50 characters each. Metadata is stored in SQLite. Files remain the media inventory, so missing files do not appear in search.

## Privacy and storage

New uploads are private to Admins and their uploader. Posters can also view images referenced by any saved version of their own posts. Published content and saved profile images make their uploads public. Shared images stay public while any public content uses them. Unpublishing a page also hides image references from its descendants.

The handler checks visibility before serving bytes or a conditional response. Original files remain private to Admins and their uploader. Uploading duplicate bytes never transfers ownership. Copies already downloaded or cached elsewhere cannot be recalled.

Each upload is a ZIP bundle containing its original, display copy, and thumbnail. The original's hash determines its stable filename and URL. Do not edit bundles in place. Back up the content database and uploads together. Deleting content does not delete its images.

Keep `uploadsDirectory` on persistent storage, outside every publicly served filesystem directory. Startup rejects overlap with `assetsDirectory`. Run one blog instance against these resources.

For public files outside Media, create an `assets/` directory and set `assetsDirectory: assets` in module configuration. A file such as `portrait.jpg` becomes `/local-assets/portrait.jpg`. Only top-level, non-hidden files are served, without directory listings. These files are public and are not optimized or cleaned up. Keep private files elsewhere.

## Cleanup

**Media → Clean up uploads** lists unused images and history-only images. Current references protect files, including draft and scheduled content. Bulk deletion excludes history-only images. Individual deletion of a historical image requires confirmation showing the affected versions.

Bulk confirmation identifies up to 5,000 files and their total size. Deletion checks references again and cannot overlap an upload or content save. New files are excluded. Files now referenced, uploaded again, or already removed are skipped. Partial failures report deleted, skipped, and unprocessed counts.

Save open editors before cleanup. The server cannot see unsaved references. If an unsaved editor loses an image, upload it again or remove the reference before saving. URLs on an old domain are conservatively retained. Cleanup never deletes bundled assets, local assets, or backups.

## Reprocess existing uploads

Build the optimizer from the repository root:

```sh
go build -o /tmp/optimize-images ./cmd/optimize-images
```

Back up uploads and stop the blog. Run `/tmp/optimize-images` from the application's configuration directory. The command preserves originals, URLs, and upload dates. Each bundle is replaced atomically. Failures report progress and leave the failed image unchanged. Rerunning is safe. Legacy uploads are not served publicly until processed.

Use the [backup guide](backup.md) for complete recovery, not just image files.
