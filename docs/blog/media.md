# Images and photo albums

## Upload and reuse

Use **Media** to upload images and browse files. **Browse library** beside a preview/profile image and **Choose existing image** in the Markdown toolbar reuse an upload. Single-image choosers return to the editor when you select a card. Save to apply the selection.

Media management and library browsing require Admin access. Posters can upload in their post editor and reuse public images or images in their own saved posts.

Uploads accept PNG, JPEG, or GIF, up to 8 MiB and 20 million pixels per file. The blog keeps the original and creates display and thumbnail copies with maximum edges of 2,048 and 480 pixels. Small images are not enlarged.

JPEG copies use quality 85. PNG keeps transparency. JPEG/PNG copies apply EXIF orientation and remove metadata, including location data. GIF keeps its original bytes, animation, and metadata. Animated PNG is rejected.

The server processes one upload at a time. Concurrent uploads receive a retry response. The optional [imaging package](../../pkg/imaging/image.go) handles processing. The blog owns upload limits, storage, and access rules.

## Add an album

Use **Add component → Photo album** below a post's Markdown editor. Existing albums appear automatically. **Remove album** removes the component from the unsaved post. **Undo** restores it. Adding Photo album again starts an empty album. Removing an album or photo does not delete uploaded files.

**Upload** selects one or more device files and starts uploading. **Library** selects existing photos. Failed uploads identify the file and reason. Invalid images do not block later files. Authentication or service failures pause the batch. Retry keeps successful uploads and attempts only failed or unprocessed files.

Select a photo to preview its unsaved caption and position. Caption fields expand over the next row. Press Enter or click outside to finish editing without submitting the post. Pasted line breaks become spaces.

Use a photo's upper-right remove button to remove it. **Undo** restores removals one at a time, including after adding or reordering photos. Undo retains up to 50 removals during the current editor session. Adding a removed photo again clears its undo entry.

Save the post to apply album changes. Albums hold up to 50 photos with captions of at most 500 characters. Near capacity, controls show the remaining spaces. A full album disables Upload, Library, and Undo until you remove a photo.

### Select library photos

Select cards, then choose **Add photos**. The selection count and button show the number selected. Selections remain while searching or changing pages. **Added to album** marks photos that cannot be selected again. When the selection fills the album, other cards show a lock. Deselect a photo to choose another.

**Show selected** displays pending selections across pages. Click a card to deselect it. Removing the last selection or choosing **Clear selection** returns to browsing. **Back to library** restores the previous browsing position. Cancel or navigation away discards pending selections.

If browsing fails, **Retry** repeats the request without losing selections. The error remains until browsing succeeds. Short landscape windows keep the chooser's actions visible while the list scrolls.

### Reorder photos

Use **Reorder**, then drag photos or use the arrow buttons. On touch screens, drag from the upper-left grip. Swipe elsewhere to scroll. A marker shows the drop location. Wide layouts use a grid. Narrow layouts use a list.

With a photo focused, Left/Right move one position, Up/Down move one row, and Home/End move to the start/end. Moved photos briefly highlight.

Caption previews show two lines. Select a truncated caption to expand it within its card. Long text scrolls without moving neighboring cards. **Show less**, Escape, or a tap outside collapses it. Reordering keeps the full caption.

**Done** applies the order to the unsaved post. **Cancel** or Escape discards the provisional order.

## View an album

Readers see photo cards below the post. Mobile galleries use two columns when each card can be at least 140 pixels wide. Narrower galleries and single-photo albums use one column. Mobile captions show two lines. The viewer keeps the full caption.

Select a card to open the viewer. Captions appear below the photo, aligned left. Previous/Next buttons, arrow keys, Home/End, and touch swipes change photos. The toolbar can hide captions or thumbnails. On phones, thumbnails start collapsed. Closing restores focus and scroll position to the selected card.

**Read full caption** expands truncated text below the photo. In limited space, it overlaps the photo's lower edge without resizing the photo. Long text scrolls. **Show less** or Escape collapses the caption.

During navigation, the previous photo and caption stay together until the next photo loads. Slow loads show a loading message. Failed loads hide the photo and caption and offer **Retry**.

Use the zoom button, double-click, double-tap, mouse wheel, or pinch gesture to zoom. Drag or use arrow keys to pan a zoomed photo. Panning and pinch gestures do not change photos. Plus/minus adjust zoom, and `0` resets it. Escape resets zoom before closing the viewer. Without JavaScript, cards link to individual images.

## Find images

Media and the chooser show 24 images per page. Search is literal and case-insensitive. Filenames, labels, and IDs rank first, tags second, and titles of current or historical content using the image third. Newer uploads break ties.

Usage filters distinguish current content, history-only references, and unused files. Current content includes drafts, scheduled posts, pages, and Site Profile. Unsaved editors and browser recovery copies are excluded.

Labels and comma-separated tags affect search, not URLs, alt text, visibility, or cleanup. Tags allow 20 values of 50 characters each. Metadata lives in SQLite. Files remain the inventory, so missing files do not appear in results.

## Privacy and storage

New uploads are private to Admins and their uploader. Posters can also view images in any saved version of their own posts. Published content and saved profile images make their uploads public. Shared images stay public while any public content uses them. Unpublishing a page also hides image references from its descendants.

Access checks run before serving bytes or conditional responses. Originals remain private to Admins and their uploader. Duplicate uploads never transfer ownership. Previously downloaded copies cannot be recalled.

Each upload is a ZIP bundle of its original, display copy, and thumbnail. The original's hash determines its filename and URL. Do not edit bundles in place.

Keep `uploadsDirectory` on persistent storage outside publicly served directories. Startup rejects overlap with `assetsDirectory`. Run one blog instance per content database and uploads directory. Back up both together.

Albums use Markdown images in the final top-level fenced block named `photo-album`. Earlier, quoted, and nested blocks remain code. The editor manages the album separately from the body. Without JavaScript, enter one Markdown image per line in the album field, such as `![Caption](/images/UPLOAD_ID)`. Albums share normal image access, history, cleanup, feed, and backup rules. They require no separate database tables.

For public files outside Media, set `assetsDirectory: assets` and create that directory. `portrait.jpg` becomes `/local-assets/portrait.jpg`. Only top-level, non-hidden files are served, without directory listings. These files are public and are not optimized or cleaned up.

## Clean up uploads

**Media → Clean up uploads** lists unused and history-only images. Current references protect files, including draft and scheduled content. Bulk deletion excludes history-only images. Individual deletion of a historical image shows the affected versions before confirmation.

Bulk confirmation covers up to 5,000 files and their total size. Deletion rechecks references and excludes files added after confirmation. Files now referenced, uploaded again, or already removed are skipped. Partial failures report deleted, skipped, and unprocessed counts. Deletion cannot overlap an upload or content save.

Save open editors before cleanup. Unsaved references are invisible to the server. If cleanup removes an image from an unsaved editor, upload it again or remove the reference before saving. URLs on an old domain are conservatively retained. Cleanup excludes bundled assets, local assets, and backups.

## Reprocess existing uploads

Build the optimizer from the repository root:

```sh
go build -o /tmp/optimize-images ./cmd/optimize-images
```

Back up uploads and stop the blog. Run `/tmp/optimize-images` from the application's configuration directory. It preserves originals, URLs, and upload dates, and replaces each bundle atomically. Failures leave the affected file unchanged. Rerunning is safe. Legacy uploads are not served publicly until processed.

See [backup and recovery](backup.md) to preserve the complete application.
