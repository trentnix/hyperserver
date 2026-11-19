# Post components

Choose **Add component** below a post's Markdown editor. Components appear below the body in the order you add them. Move them with the heading's arrow buttons. Save the post to apply changes. A post can consist entirely of components.

An added component must contain valid content. Fill it in or remove it before saving. A rejected save preserves your edits. Removal offers Undo and does not delete uploads. Unsaved indicators, browser recovery, and revision history apply to every component. Page editors do not expose components yet.

| Component | Content |
| --- | --- |
| [Photo album](media.md#add-an-album) | Up to 50 photos with captions and a full-screen viewer. |
| [File downloads](#file-downloads) | Up to 20 PDF, ZIP, UTF-8 text, or MP3 attachments. |
| [PDF preview](#pdf-preview) | One PDF in the browser's viewer or a page-by-page presentation. |
| [Audio](#audio) | One MP3 with an optional transcript. |
| [Event](#event) | An all-day or timed event with an optional location and details link. |
| [Image comparison](#image-comparison) | Two images with a draggable divider. |
| [Link preview](#link-preview) | An editable card linking to another site. |
| [Video](#video) | A YouTube or Vimeo player. |
| [Embed](#embed) | An interactive page or widget from an approved site. |
| [Social media](social-posts.md) | A public post, with its platform detected from the URL. |
| [Related posts](#related-posts) | An ordered list of links shown only on this post. |

[Series](series.md) provide shared navigation across member posts and are managed separately. For registration, rendering, and saved formats, see [developing components](component-development.md).

## File downloads

Use **Upload** to add files or **Library** to reuse uploads. Edit display names and optional descriptions, then arrange the files with the arrow buttons. Downloads keep their original filenames. Upload batches block Save until they finish. Retry attempts only failed or unprocessed files.

Files must use `.pdf`, `.zip`, `.txt`, or `.mp3` extensions. Each file can be at most 32 MiB. File storage totals at most 1 GiB per site, shared by downloads, PDF previews, and audio. Images have separate limits. A reverse proxy must allow at least 34 MiB for `/post/files/upload`, for example Nginx `client_max_body_size 34m;`.

The server validates file contents rather than trusting the submitted MIME type. It checks PDF signatures and trailers, ZIP structure, UTF-8 text, and MP3 frames. Archives are never extracted. Validation does not scan for malware. Ordinary downloads use attachment responses with `nosniff` and support HEAD, ranges, resume, and cache revalidation.

Admins can access every file. Posters can access their uploads, files used by any saved version of their posts, and files used by public content. Anonymous access requires a current public reference. Drafts, future scheduled posts, and history do not grant public access. Removing the last public reference revokes anonymous access, including conditional and range requests. Re-uploading identical bytes preserves the original uploader and filename.

Files live under `uploadsDirectory/files`, identified by SHA-256 hashes and validated extensions. Display names never become storage paths. [Media cleanup](media.md#clean-up-uploads) and [backups](backup.md) cover these files. Unsaved editors and browser recovery copies cannot protect an upload from cleanup.

## PDF preview

Upload or choose one PDF, then edit its display name and optional description. **Open PDF** opens the file in a separate tab. **Replace PDF** and **Library** replace the document while preserving its details and display settings. Canceling or failing to replace keeps the current file.

The browser's PDF viewer is the default. Clear **Show download link** to hide the component's Download link. The browser may still offer downloading. This setting controls the link's visibility. The same PDF can appear in a preview and File downloads without storing another copy.

Enable **Presentation mode** to show one page at a time, with Previous/Next controls, a page indicator, and fullscreen where supported. Focus the presentation to use Left/Right, Page Up/Page Down, or Home/End. Pages fit without cropping, and portrait pages use more vertical space on narrow screens.

Mozilla PDF.js renders presentations locally in the browser. If rendering fails or JavaScript is unavailable, the ordinary viewer or **Open PDF** remains available.

PDF previews use the file limits and access rules above. `/files/{id}?preview=1` permits inline same-origin viewing after access checks. Ordinary pages and attachments retain their framing restrictions. Feeds and historical previews show links and descriptions. Search indexes the display name and description, not text inside the PDF.

## Audio

Upload or choose one MP3. The title, description, and Markdown transcript are optional. Readers get native playback controls, autoplay disabled, and `preload="none"`. The transcript expands below the player. Raw HTML and unsafe links are omitted. **Show download link** controls the separate Download MP3 link and cannot prevent downloading.

Replacing the recording preserves its details and download setting. Titles allow 240 characters, descriptions 1,000, and transcripts 20,000. MP3s use the file limits and access rules above.

Validation requires two complete MPEG Layer III frames after an optional ID3v2 tag. Standard constant and variable bitrates work. Free-format bitrates do not. The application does not transcode audio.

Feeds provide a listening link. Search includes authored details and transcript text.

## Event

Enter a name and start date. Select **All day** to disable the times and time zone. Add an end date for an event that spans several days. Readers see both the first and last days.

For a timed event, enter a start time. New events use the [site time zone](content.md#posts) by default. To change the event's time zone, search by city or region. An end time without an end date uses the start date. If the event ends on another day, enter both the end date and end time. If a clock change repeats a time, enter it in UTC.

Timed events show their UTC offset. If a time-zone rule change makes a saved time invalid, the card shows the entered local time and zone.

You can add a place or describe where to join online. For a physical venue, paste a map share link in **Map link** to show **View map** below the location. Use **Event website** for more information or registration. You can upload an image or choose one from the library. If the image conveys information, add an image description. Event dates are independent of the post's publication date.

## Image comparison

Choose **Before & after**. Add an optional title and caption, then choose two different uploaded images. Matching dimensions and framing work best. Each image has an editable label and an optional description used as alt text. Blank labels default to **Before** and **After**.

Readers drag the divider to compare. Tab focuses its handle. Arrow keys adjust it, and Home/End reveal either image completely. Images fit a shared 4:3 frame without cropping. Other proportions leave empty space. Without JavaScript, in feeds, and in historical previews, both labeled images appear side by side.

Each image follows the [image limits](media.md#upload-and-reuse). Labels allow 80 characters, the title 240, the caption 1,000, and each image description 500. Long labels truncate to one line. One comparison is allowed per post.

## Link preview

Enter an HTTP or HTTPS URL and choose **Fetch preview**, or enter the details yourself. Edit the title, description, and site name. Upload or choose an image, hide it with **Show image**, or remove it. The preview updates as you edit. Refresh asks before replacing details. Edits made during a request take precedence over its response.

Readers see a linked card with the saved details and optional image. The destination comes from the authored URL. Metadata cannot replace it. Hidden images retain their saved references. Removing an image clears its current reference. Readers, feeds, search, and history never fetch remote metadata.

Fetching requires an Admin or Poster session and same-origin protection. Link previews and video details share ten requests per author per minute, two concurrent requests, and at most 1,024 active author windows. Each fetch has a five-second deadline, a 1 MiB decoded HTML limit, and at most three redirects. Every destination and image address must resolve to a permitted public IP. Credentials, cookies, and proxies are not forwarded. HTTPS redirects cannot downgrade to HTTP.

The fetcher reads UTF-8 Open Graph or HTML title/description metadata. It ignores embed HTML, document base URLs, and `og:url`. Relative image URLs resolve against the fetched page. Imported thumbnails use the [image pipeline](media.md#upload-and-reuse). If importing fails, text remains usable. Sites requiring login or JavaScript may need manual details.

One preview is allowed per post. Titles allow 240 characters, descriptions 1,000, site names 200, and URLs 2,048 bytes. Search includes the saved title, description, and site name.

## Video

Paste a YouTube or Vimeo URL. Title and caption are optional. **Fetch details** copies available source text. You can edit or clear it. Refresh asks before replacing text. Missing source fields or failed requests leave your text intact.

Fetching uses the link-preview limits above and imports no thumbnail or embed HTML.

YouTube accepts watch, `youtu.be`, Shorts, live, and embed links. Vimeo accepts ordinary, unlisted, and `player.vimeo.com/video/` links. Keep the privacy hash in unlisted Vimeo URLs. YouTube `t`/`start` and Vimeo `#t=` accept whole seconds or durations such as `1m30s`, from zero to seven days. Other playback and tracking parameters are ignored.

Readers see the provider's preview and controls. Autoplay is disabled. The iframe loads near the viewport and contacts the provider before Play. It also works without the blog's JavaScript. Provider restrictions can prevent embedding. Saving does not check availability.

One video is allowed per post. Titles allow 240 characters, captions 1,000, and URLs 2,048 bytes. Text is fetched only at the author's request and saved with the post. Feeds use a viewing link with authored details. Search includes the title and caption. Video files remain with the provider.

## Embed

Paste the provider’s HTTPS embed URL and enter a title that describes the content. You can add up to 2,000 characters of Markdown above the embed. Choose **Preview embed** to check the URL before saving. Readers can open the source in a new tab. Link-only views, feeds, and revision previews show the description and link.

Set `embedOrigins` in `modules/blog/config.yaml` to a list of exact HTTPS origins, such as `https://charts.example.com`, then restart the blog. The list starts empty. A URL whose origin is absent from the list appears as a link. Providers must permit framing and work within a sandbox.

## Related posts

Type a title to search for posts, then select up to 12 results. Results stay open so you can add several at once. Click outside, press Escape, or clear the search to dismiss them. Arrange the links with the arrow buttons. An optional heading replaces “Related posts.”

Links appear only on the source post. Selecting a target neither changes it nor creates a reciprocal link. Readers see current titles and URLs for published targets. Draft, future scheduled, and deleted targets remain referenced but hidden. Republishing restores their links in the same positions.

If no target is public, the component disappears. Feeds and historical previews use the same visibility rules.

Authors can select public posts and private posts they can edit. Private titles remain hidden from other authors. Existing unavailable references stay saved. New missing or inaccessible targets, duplicates, self-links, and empty components fail validation. The component stores stable post IDs, not copied titles or URLs.
