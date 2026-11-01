# Images

## Upload and reuse

Use **Media** to upload images and browse existing files. Use **Browse library** beside a preview/profile image or **Choose existing image** in the Markdown toolbar to reuse one. Clicking a chooser card selects it and returns to the editor. Selection takes effect when you save.

Uploads accept PNG, JPEG, or GIF, up to 8 MiB and 20 million pixels. The blog keeps the original and generates display and thumbnail copies with maximum edges of 2,048 and 480 pixels. Small images are not enlarged. JPEG copies use quality 85. PNG keeps transparency. JPEG/PNG copies apply EXIF orientation and remove metadata, including location data. GIF retains its original bytes, animation, and metadata. Animated PNG is rejected.

Only one upload is processed at a time. A concurrent upload receives a retry message. Image processing uses the framework's optional [imaging package](../../pkg/imaging/image.go). The blog owns limits, storage, access rules, and editor behavior.

## Find images

Media and the chooser show 24 images per page. Search matches filenames, labels, IDs, tags, and titles of current or historical content using an image. Matching is literal and case-insensitive. Names rank first, tags second, and content titles third. Newer uploads break ties.

Usage filters distinguish current content, history-only references, and unused files. Current content includes drafts, scheduled posts, pages, and the site profile. Unsaved editors and browser recovery copies are not included.

Labels and comma-separated tags help search. They do not change URLs, alt text, visibility, or cleanup rules. Tags allow 20 values of 50 characters each. Metadata is stored in SQLite. Files remain the media inventory, so missing files do not appear in search.

## Privacy and storage

New uploads and images used only by drafts or future posts are administrator-only. Published content and saved profile images make their uploads public. Shared images stay public while any public content uses them. Unpublishing a page also hides image references from its descendants.

The handler checks visibility before serving bytes or a conditional response. **Download original** remains administrator-only. Copies already downloaded or cached elsewhere cannot be recalled.

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
