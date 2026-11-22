# Content components

Choose **Add component** from the Content toolbar when editing a post or page. Edit its settings and choose **Done** to add it at the cursor, or at the end if you have not set a cursor. If the cursor is inside a paragraph, list, or code block, the component goes after that block. Choose **Edit** on the component row to reopen its settings. **Cancel** discards changes made since you opened the dialog. You can add the same type more than once. Labels use the title, filename, or another detail when available.

Drag the **Move** handle to reposition a component in the writing or in the list below it. Choose **Move** to show **Up** and **Down** for moving one block or row at a time. The Content editor’s Undo and Redo include component additions, removals, and moves. Posts and pages can consist entirely of components.

Choose **Remove** on the component row. The latest removal offers **Undo** for a few seconds. Earlier removals remain in the editor’s Undo history. Removing a component keeps its uploads.

**Done** checks the component settings. If something needs correction, the dialog stays open and keeps your edits. Saving also checks the complete post or page, including its media and related-content references. Unsaved indicators, browser recovery, and revision history apply to every component.

Fields that support Markdown have a formatting toolbar for headings, lists, links, and code blocks.

| Component | Content |
| --- | --- |
| [Photo album](media.md#add-an-album) | Up to 50 photos with captions and a full-screen viewer. |
| [File downloads](#file-downloads) | Up to 20 PDF, ZIP, UTF-8 text, or MP3 attachments. |
| [PDF viewer](#pdf-viewer) | One PDF in the browser's viewer or a page-by-page presentation. |
| [Audio](#audio) | One MP3 with an optional transcript. |
| [Event](#event) | An all-day or timed event with an optional location and details link. |
| [Image comparison](#image-comparison) | Two images with a draggable divider. |
| [Expandable details](#expandable-details) | A titled section readers can open inline. |
| [Callout](#callout) | A highlighted Markdown note. |
| [Timeline](#timeline) | Dated milestones in chronological order. |
| [Table of Contents](#table-of-contents) | Jump links to headings in the content. |
| [Link preview](#link-preview) | An editable card linking to another site. |
| [Action link](#action-link) | A button with a label you write and a destination URL. |
| [Video](#video) | A YouTube or Vimeo player. |
| [Embed](#embed) | An interactive page or widget from an approved site. |
| [Social media](social-posts.md) | A public post, with its platform detected from the URL. |
| [Related content](#related-content) | An ordered list of links to posts and pages. |

[Series](series.md) provide shared navigation across member posts and are managed separately. For registration, rendering, and saved formats, see [developing components](component-development.md).

## File downloads

Use **Upload** to add files or **Library** to reuse uploads. Edit display names and optional descriptions, then arrange the files with the arrow buttons. Downloads keep their original filenames. Upload batches block Save until they finish. Retry attempts only failed or unprocessed files.

Files must use `.pdf`, `.zip`, `.txt`, or `.mp3` extensions. Each file can be at most 32 MiB. A reverse proxy must allow at least 34 MiB for `/post/files/upload`, for example Nginx `client_max_body_size 34m;`.

Images, downloads, PDFs, and audio share a 1 GiB default storage limit. Set `fileStorageLimitMiB` in `modules/blog/config.yaml` to a positive whole number of MiB and restart the blog to change it. **Media** shows used and available storage. Lowering the limit preserves stored uploads. If an upload needs more space than is available, free space or raise the limit.

The server validates file contents rather than trusting the submitted MIME type. It checks PDF signatures and trailers, ZIP structure, UTF-8 text, and MP3 frames. Archives are never extracted. Validation does not scan for malware. Ordinary downloads use attachment responses with `nosniff` and support HEAD, ranges, resume, and cache revalidation.

Admins can access every file. Posters can access their uploads, files used by any saved version of their posts, and files used by public content. Anonymous access requires a current public reference. Drafts, future scheduled posts, and history do not grant public access. Removing the last public reference revokes anonymous access, including conditional and range requests. Re-uploading identical bytes preserves the original uploader and filename.

Files live under `uploadsDirectory/files`, identified by SHA-256 hashes and validated extensions. Display names never become storage paths. [Media cleanup](media.md#clean-up-uploads) and [backups](backup.md) cover these files. Unsaved editors and browser recovery copies cannot protect an upload from cleanup.

## PDF viewer

Upload or choose one PDF, then edit its display name and optional description. **Open PDF** opens the file in a separate tab. **Replace PDF** and **Library** replace the document while preserving its details and display settings. Canceling or failing to replace keeps the current file.

The browser's PDF viewer is the default. Clear **Show download link** to hide the component's Download link. The browser may still offer downloading. This setting controls the link's visibility. The PDF viewer and File downloads can use the same stored PDF.

Enable **Presentation mode** to show one page at a time, with Previous/Next controls, a page indicator, and fullscreen where supported. Focus the presentation to use Left/Right, Page Up/Page Down, or Home/End. Pages fit without cropping, and portrait pages use more vertical space on narrow screens.

Mozilla PDF.js renders presentations locally in the browser. If rendering fails or JavaScript is unavailable, the ordinary viewer or **Open PDF** remains available.

The PDF viewer uses the file limits and access rules above. `/files/{id}?preview=1` permits inline same-origin viewing after access checks. Ordinary pages and attachments retain their framing restrictions. Feeds and historical previews show links and descriptions. Search indexes the display name and description, not text inside the PDF.

## Audio

Upload or choose one MP3. The title, description, and Markdown transcript are optional. Readers get native playback controls, autoplay disabled, and `preload="none"`. The transcript expands below the player. Raw HTML and unsafe links are omitted. **Show download link** controls the separate Download MP3 link and cannot prevent downloading.

Replacing the recording preserves its details and download setting. Titles allow 240 characters, descriptions 1,000, and transcripts 20,000. MP3s use the file limits and access rules above.

Validation requires two complete MPEG Layer III frames after an optional ID3v2 tag. Standard constant and variable bitrates work. Free-format bitrates do not. The application does not transcode audio.

Feeds provide a listening link. Search includes authored details and transcript text.

## Event

Enter a name and start date. Select **All day** to disable the times and time zone. Add an end date for an event that spans several days. Readers see both the first and last days.

For a timed event, enter a start time. New events use the [site time zone](content.md#posts) by default. To change the event's time zone, search by city or region. An end time without an end date uses the start date. If the event ends on another day, enter both the end date and end time. If a clock change repeats a time, enter it in UTC.

Timed events show their UTC offset. If a time-zone rule change makes a saved time invalid, the card shows the entered local time and zone. Update the event time to restore the calendar download.

**Add to calendar** downloads an `.ics` file for import into a calendar app. The file captures the event as it appears at download time. All-day events include every displayed date.

You can add a place or describe where to join online. For a physical venue, paste a map share link in **Map link** to show **View map** below the location. Use **Event website** for more information or registration. You can upload an image or choose one from the library. If the image conveys information, add an image description. Event dates are independent of the post's publication date.

## Image comparison

Choose **Before & after**. Add an optional title and caption, then choose two different uploaded images. Matching dimensions and framing work best. Each image has an editable label and an optional description used as alt text. Blank labels default to **Before** and **After**.

Readers drag the divider to compare. Tab focuses its handle. Arrow keys adjust it, and Home/End reveal either image completely. Images fit a shared 4:3 frame without cropping. Other proportions leave empty space. Without JavaScript, in feeds, and in historical previews, both labeled images appear side by side.

Each image follows the [image limits](media.md#upload-and-reuse). Labels allow 80 characters, the title 240, the caption 1,000, and each image description 500. Long labels truncate to one line.

## Expandable details

Enter a title and Markdown body. The post shows the title by default. Readers can select it to open or close the details. RSS shows the full body. Search includes the title and body.

Titles allow 240 characters and bodies allow 20,000. The body supports Markdown links, images, and code blocks. Add other components separately through the Content toolbar.

## Callout

Enter Markdown text and an optional heading. Each callout defaults to Parchment. Choose blue-gray Slate for a neutral note, red-orange Caution for a warning, or green Success for good news. Use the heading or text to convey the message. The full text appears inline and in RSS. Search includes the heading and text.

## Timeline

Add up to 20 milestones with a date and title. Enter a year such as `2025`, a month such as `2025-10`, or a day such as `2025-10-18`. Each date appears at the precision you entered. You can give the timeline a heading and use **Add detail** to write Markdown for a milestone.

Choose a **Color** to preview how the timeline will look. Slate is the default. White, Parchment, Forest, and Brick are also available.

Readers see the oldest milestones first. A year entry comes before its months, and a month entry comes before its days. Milestones with identical dates keep the order you entered. The timeline also appears in RSS.

## Table of Contents

Choose **Add component → Table of Contents**. H2 and H3 are selected by default. Choose the heading levels to include, then select **Done**. The outline nests headings under the nearest included parent and appears when at least two headings in the body match. On viewports at least 1,680 pixels wide and 21:9 or wider, the first Table of Contents appears in a sticky rail to the right of the content. Pages with **Preview blocks** keep the outline inline.

## Link preview

Enter an HTTP or HTTPS URL and choose **Fetch preview**, or enter the details yourself. Edit the title, description, and site name. Upload or choose an image, hide it with **Show image**, or remove it. The preview updates as you edit. Refresh asks before replacing details. Edits made during a request take precedence over its response.

Readers see a linked card with the saved details and optional image. The destination comes from the authored URL. Metadata cannot replace it. Hidden images retain their saved references. Removing an image clears its current reference. Readers, feeds, search, and history never fetch remote metadata.

Fetching requires an Admin or Poster session and same-origin protection. Link previews and video details share ten requests per author per minute, two concurrent requests, and at most 1,024 active author windows. Each fetch has a five-second deadline, a 1 MiB decoded HTML limit, and at most three redirects. Every destination and image address must resolve to a permitted public IP. Credentials, cookies, and proxies are not forwarded. HTTPS redirects cannot downgrade to HTTP.

The fetcher reads UTF-8 Open Graph or HTML title/description metadata. It ignores embed HTML, document base URLs, and `og:url`. Relative image URLs resolve against the fetched page. Imported thumbnails use the [image pipeline](media.md#upload-and-reuse). If importing fails, text remains usable. Sites requiring login or JavaScript may need manual details.

Titles allow 240 characters, descriptions 1,000, site names 200, and URLs 2,048 bytes. Search includes the saved title, description, and site name.

## Action link

Enter a **Button label** such as “View the project” and a **Destination URL**. A site path such as `/page/projects` opens in the same tab. A full HTTP or HTTPS URL opens in a new tab. The button appears wherever you position the component. RSS shows a text link.

## Video

Paste a YouTube or Vimeo URL. Title and caption are optional. **Fetch details** copies available source text. You can edit or clear it. Refresh asks before replacing text. Missing source fields or failed requests leave your text intact.

Fetching uses the link-preview limits above and imports no thumbnail or embed HTML.

YouTube accepts watch, `youtu.be`, Shorts, live, and embed links. Vimeo accepts ordinary, unlisted, and `player.vimeo.com/video/` links. Keep the privacy hash in unlisted Vimeo URLs. YouTube `t`/`start` and Vimeo `#t=` accept whole seconds or durations such as `1m30s`, from zero to seven days. Other playback and tracking parameters are ignored.

Readers see the provider's preview and controls. Autoplay is disabled. The iframe loads near the viewport and contacts the provider before Play. It also works without the blog's JavaScript. Provider restrictions can prevent embedding. Saving does not check availability.

Titles allow 240 characters, captions 1,000, and URLs 2,048 bytes. Text is fetched only at the author's request and saved with the post. Feeds use a viewing link with authored details. Search includes the title and caption. Video files remain with the provider.

## Embed

Paste the provider’s HTTPS embed URL and enter a title that describes the content. You can add up to 2,000 characters of Markdown above the embed. Choose **Preview embed** to check the URL before saving. Readers can open the source in a new tab. Link-only views, feeds, and revision previews show the description and link.

Set `embedOrigins` in `modules/blog/config.yaml` to a list of exact HTTPS origins, such as `https://charts.example.com`, then restart the blog. The list starts empty. A URL whose origin is absent from the list appears as a link. Providers must permit framing and work within a sandbox.

## Related content

Search by title and select up to 12 posts and pages. Results stay open so you can add several at once. Click outside, press Escape, or clear the search to dismiss them. Arrange the links with the arrow buttons. An optional heading replaces “Related content.”

Links appear on the post or page where you add the component. Readers see current titles and URLs for public targets. Drafts, scheduled posts, pages under draft parents, and deleted targets stay hidden. A target that becomes public again returns to its place in the list.

If no target is public, the component disappears. Feeds and historical previews use the same visibility rules.

Authors can select public content and private posts they can edit. Admins can also select draft pages and pages under draft parents. Saved selections survive later changes in visibility or deletion, including when you restore a saved version. New missing or inaccessible targets, duplicates, self-links, and empty components fail validation. Selections use stable IDs.
