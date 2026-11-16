# Before and after images

Choose **Add component → Before & after** below the post editor. Add an optional title and caption, then upload or choose a different image for each side. Matching dimensions and framing work best. Each image has an editable label and an optional description. Leave the labels blank to use **Before** and **After**. Save the post to apply changes.

Readers can drag across the image. The divider handle is the only visible control. Tab focuses the handle, arrow keys adjust the comparison, and Home and End reveal either image completely. Both images fit the same 4:3 frame without cropping. Images with other proportions have empty space around them. Without JavaScript, both labeled images appear side by side. Feeds and historical previews also show both images without interactive controls.

An added comparison must contain two different uploaded images. Each upload follows the site's image limits: PNG, JPEG, or GIF, up to 8 MiB and 20 megapixels. Each label allows 80 characters, the title 240, the caption 1,000, and each image description 500. Long labels display on one line with an ellipsis. One comparison component is allowed per post.

The existing version 2 `post-components` block stores an `image-comparison` record with a `comparison` object. Required fields are `beforeID` and `afterID`. Optional fields are `beforeLabel`, `afterLabel`, `beforeAlt`, `afterAlt`, `title`, and `caption`. No database migration is required. Image permissions, revision history, backups, cleanup protection, ordering, removal, Undo, unsaved indicators, and draft recovery use the shared component paths.
