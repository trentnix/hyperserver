# Writing and site content

## Posts

Use **Posts → New post** to write a title and Markdown body. Choose **Draft**, **Publish**, or **Schedule** before saving. Scheduling is available only for never-published posts. Choose **Draft** to unpublish or cancel a schedule.

In a new post, choose **Import**. On phones, it’s under **More**. Choose or drop a folder containing exactly one Markdown file and the local images it references. Keep the image paths relative to the Markdown file. If any file is missing or extra, choose a corrected folder. You can choose a Markdown file by itself if it references no local images. Import accepts UTF-8 Markdown up to 256 KiB and PNG, JPEG, or GIF images. It preserves image sizes, alt text, and authored links. If the file contains HyperServer component data or placement markers, remove them before importing.

Choose **Open draft**. If import would replace unsaved post content, components, or the publication setting, it asks before uploading. Import keeps the other fields and uses the file name as the title if the title is blank. Review the result, then choose **Save draft**. If import fails, the picker appears with the attempted folder or file and the reason. If an upload fails or you cancel after uploads begin, completed uploads remain stored.

Set `timeZone` in `modules/blog/config.yaml` to an IANA name such as `America/Chicago`, then restart the blog. The default is `UTC`. The setting applies to scheduling, publication dates, and new Events. Events can use a different time zone. A change to the setting preserves the publication time of existing schedules and the time zone of saved Events.

Republishing keeps the original publication date. Canceling before a scheduled time clears that date. Scheduled content becomes public at the saved time without a background worker, so the server clock must be accurate.

The Posts list groups Drafts, Scheduled, and Published content. Search matches current titles, tags, summaries, and Markdown, ranked in that order within each group. Empty Drafts and Scheduled groups are hidden. Searching hides every empty group.

Cancel returns to the page that opened the editor, such as the post or management list. When editing a saved version, it returns to History. Posts and Pages refresh when revisited. The server validates input and renders Markdown with raw HTML disabled.

## Slugs and tags

Leave a slug blank to generate it from the title. Generated collisions get `-2`, `-3`, or the next available suffix. Custom slugs must be unique within their content type. Posts and pages have separate URL namespaces. Slugs allow lowercase ASCII letters, digits, and hyphens, up to 120 characters. If a title produces no usable slug, enter one yourself.

Changing a title preserves its existing slug. Changing a slug breaks old links. Editors warn when changing URLs of current or previously published content. Never-published drafts do not warn. Without JavaScript, the field shows the warning but cannot request confirmation.

Tags are optional search keywords, not public navigation. Enter comma-separated labels or choose suggestions. Saves normalize case and spacing and remove duplicates. Each item allows 20 tags of 50 characters each. Tags are included in history and browser recovery copies.

## Pages

Pages have a title, slug, optional parent, preview description/image, display order, and publication setting. **More → New subpage** selects the current page as parent. Each parent chooses whether to hide subpage previews, show a list, or show preview blocks. Blocks use two columns except on mobile.

About and Projects are ordinary pages with slugs `about` and `projects`, served at `/about` and `/projects`. Other pages use `/page/{slug}`. To make a Projects gallery, enable preview blocks and add projects as subpages. Legacy `/project/s/{slug}` links remain readable.

Navigation shows published top-level pages and their immediate children. Deeper pages remain reachable through their URLs and parent previews. Explicit display orders sort first, lowest first. Remaining pages and ties sort by most recently updated. Hiding body previews does not hide navigation links.

Draft pages and descendants of draft parents return 404 to visitors. Administrators can edit the whole tree. Move or delete subpages before deleting their parent. Pages can have empty bodies and do not appear in RSS or Recent Posts. Page search returns a flat list. Clear it to return to the tree.

## Site Profile

Edit the name, short bio, portrait, and favicon through **Site Profile**. Changes take effect when you save. Clearing an image restores its default. The administrator email comes from module configuration. Saving returns to the site root with a brief confirmation.

Choose **Default**, **Thanksgiving**, or **Christmas** under **Theme**. The theme supplies the page colors and background image. Choose **Preview theme** to try it in a new tab. The preview follows you as you browse that tab. Choose **Exit preview** to return to the site's theme. Choose **Save profile** to apply the selected theme site-wide. Profile history includes the theme.

Images can be uploaded, selected from Media, or supplied as local absolute paths or HTTPS URLs. Use PNG for uploaded favicons. Upload controls require JavaScript. See [images](media.md) for privacy and local-file setup.

## Saved versions

**History** appears after more than one version exists. Posts and pages expose it in their management lists and **More** menus. Site Profile has a History button.

**Edit this version** opens older writing for editing without saving. **Restore this version** applies it immediately and records a new version. Both preserve intervening history. Restoring post/page writing leaves the current slug, publication state, schedule, and page placement unchanged.

Identical content creates no new history and cannot be restored redundantly. If another tab has saved first, stale edits and restores are rejected. Open the current editor in another tab to compare changes.

Older revisions can be deleted. The latest version is protected. Deleting a revision does not delete current content, images, backups, or browser copies. Deleting a post or page removes its history.

Historical images do not become public merely because history references them. If an image was deleted, edit the affected version to replace or remove the reference before saving or restoring.

## Unsaved writing

Components show **Unsaved changes** when their settings or position differ from the saved post or page. Saving or returning a component to its saved values clears the indicator.

With JavaScript enabled, post and page editors save browser recovery copies after a half-second pause and when leaving. Reopen the editor to restore a copy. **Discard copies** clears the available copies of an existing post or page from this browser. A successful save clears copies that existed when it started and stayed unchanged. Saving a new item clears the copies used to create it.

Copies are separate by account, editor, and tab. They remain after logout, expire after seven days, and are cleaned up when an editor next opens. They are not encrypted, synced, or included in backups. Use a private browser profile on shared devices. Profile and authentication forms do not have recovery copies.

Editors warn about unsaved changes before navigation. Browsers may suppress reload/close warnings. Save to the server before clearing browser data.

## Reader navigation and sharing

All Posts lists 30 published posts per page, grouped by month. `recentPostCount` controls Recent Posts and must be positive.

In `modules/blog/config.yaml`, `rssPostLimit` controls how many published posts RSS includes with their full content. The default is 50. Use `0` for no limit. Negative values and non-integers fail at startup. Restart after changing either setting.

Older posts remain available through All Posts and their URLs. Post changes invalidate the feed and list caches, and scheduled publication expires them on time.

Search at `/search` matches literal phrases in public titles, tags, summaries, and Markdown, in that rank order. It excludes drafts, future posts, hidden descendants, and history. It returns up to 50 matches with a total count.

Search is limited to 30 requests/minute per IP and two seconds of database work. Search responses are not cached. A bounded cache reuses normalized bodies, but search still scans content rather than using a full-text index. See [search limits and benchmarks](../performance.md#blog-search).

Public pages include canonical URLs, descriptions, and Open Graph metadata. Summaries fall back to the site bio. Sharing images use the selected preview, then the first suitable content image, then the site portrait. SVGs are skipped. With no suitable image, sharing is text-only. Page preview blocks use a bundled placeholder when no preview image is selected.

Set `http.publicOrigin` to the deployed address. Sharing URLs never come from untrusted Host headers. Drafts, future posts, hidden descendants, editors, and authentication pages omit sharing metadata. Sharing services may cache older previews.

`/sitemap.xml` lists public content. It supports one sitemap of at most 50,000 URLs or 50 MiB. `/robots.txt` allows crawling and advertises that sitemap. Neither is an access-control mechanism.
