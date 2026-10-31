# Blog example

A personal Markdown blog with a responsive profile sidebar, post archive, About and Projects pages, RSS, image uploads, and an administrator editor. Authentication pages use the blog's layout and styles.

## Run locally

From this directory:

```sh
cp config-template.yaml config.yaml
cp modules/blog/config-template.yaml modules/blog/config.yaml
openssl rand -hex 32
openssl rand -hex 32
```

Put the two different generated keys in `http.session.jwtKey` and `auth.jwtKey`. Both `config.yaml` files and SQLite databases are ignored by Git. Keep any existing local configuration instead of overwriting it.

Before starting the server, set `adminEmail` in `modules/blog/config.yaml` to your email address. Only that address can register or log in. Administrative access also requires email verification. The administrator address stays in configuration, not the Site Profile editor.

Set `recentPostCount` in `modules/blog/config.yaml` to the number of Recent Posts links to show. It must be greater than zero. Restart after changing configuration. The All Posts archive lists published posts in pages of 30, newest first and grouped by month. Previous and Next work with ordinary navigation and HTMX.

Start a local SMTP inbox if one is not already running:

```sh
docker run --rm --name blog-mailpit -p 127.0.0.1:1025:1025 -p 127.0.0.1:8025:8025 -e MP_SMTP_AUTH_ACCEPT_ANY=1 -e MP_SMTP_AUTH_ALLOW_INSECURE=1 axllent/mailpit
```

Then start the blog:

```sh
GOWORK=off go run .
```

Open <http://127.0.0.1:8086/auth/register/email> and register the configured address. Open the verification message in <http://127.0.0.1:8025>, confirm the address, and log in. The **Posts** link appears for the verified administrator.

Mailpit captures mail locally without delivering it externally. Stop the server with Ctrl+C.

Set `auth.registrationEnabled: false` after creating the administrator if desired. Login, recovery, and verification remain available. For deployment, configure real SMTP delivery, independent random signing keys, HTTPS, and `http.publicOrigin`.

## Structure

`application.go` selects the blog and authentication modules, the response layout, and the blog's email provider. HyperServer opens configured account and session services and owns their lifetime. `internal/blog` owns the administrator rule, templates, handlers, SQLite storage for posts and pages, and file-based image storage. The module loads `modules/blog/config.yaml` through the framework. Storage paths are relative to the application configuration directory.

EasyMDE supplies the editor toolbar and upload controls. The server validates all submissions and renders preview and published Markdown with Goldmark. Raw HTML is disabled. Uploaded images remain stored when content is deleted. SQLite startup adds the required columns and indexes existing content's upload references. Other older page schemas are not upgraded automatically.

Set `uploadsDirectory` in the module config to a persistent, application-owned directory. The sample uses `uploads/`, which is ignored by Git. Back up this directory along with the databases. Each upload is one uncompressed ZIP bundle containing its original, display copy, and thumbnail. The original's hash determines the filename and stable image URL. The handler serves individual image bytes, not the bundle. Do not edit stored files in place.

Uploads produce a display copy with a maximum 2,048-pixel edge and a 480-pixel thumbnail, without enlargement or cropping. JPEG copies use quality 85. PNG copies retain transparency. EXIF orientation is applied before resizing, and JPEG/PNG copies omit metadata, including location data. GIFs keep their original bytes, animation, dimensions, and metadata. Animated PNGs are rejected rather than flattened. Only one upload is processed at a time. A concurrent upload receives a retry message.

Originals remain available to the verified administrator through **Download original** in Media. Public image URLs serve display copies, and library previews request thumbnails. Both copies follow the content visibility rules below. Cleanup removes the original and copies together. Media sizes include all stored copies. Bundled and local assets are not processed.

For existing uploads or a change to the processing settings, stop the blog and run this command from this directory before restarting:

```sh
GOWORK=off go run ./cmd/optimize-images
```

The command uses the existing configuration, preserves originals, URLs, and upload dates, and replaces each image atomically. If it fails, it reports the number completed and leaves the failed image unchanged. It can be rerun. Legacy uploads are not served publicly until processed. Keep an uploads backup before running maintenance.

Only the verified administrator can view new uploads and images used exclusively by drafts or scheduled posts. An image becomes public when a published post, publicly visible page, or saved site profile references it. Shared images stay public while any public content still uses them. Unpublishing a parent page hides its descendants' image references too.

Image responses recheck access before returning bytes or a cache-validation response. Previously downloaded copies cannot be recalled, including copies cached under the former one-year policy. Startup rejects overlapping `uploadsDirectory` and `assetsDirectory` paths, including symlinks. Keep uploads outside any other publicly served filesystem directory.

Choose **Media** to upload images or browse existing uploads, newest first, with 24 images per page. Select an image to see links to its current uses and saved versions. Use **Browse library** beside a preview, **Choose existing** beside a profile image, or **Choose existing image** in the Markdown toolbar to reuse a file without uploading another copy. In the chooser, click an image card to select it and return to the editor. Cancel leaves the editor unchanged. Selection takes effect only when you save and does not change image visibility by itself.

Both views search filenames, editable image labels, IDs, and the titles of current or historical content that uses an image. Search is literal and case-insensitive. Usage filters apply before pagination. **Current content** includes drafts, scheduled posts, pages, and the site profile. **History only** excludes current uses. **Unused** excludes both current uses and saved versions. Unsaved editor content is not included.

New uploads retain their first filename. Change the label in Image details without changing the URL or image alt text. Existing uploads fall back to their ID until labeled. Names are stored in SQLite, while files remain the source of the media inventory. Deleting a file hides it from search. Its name record remains available if the same image is uploaded again.

Choose **Clean up uploads** in Media to review unused images and delete them individually or with **Delete all unused**. Bulk confirmation shows the image count and total bytes, with at most 5,000 images per batch. Only the confirmed files are considered. New uploads are excluded, and files now referenced, uploaded again, or already removed are skipped. The result reports deleted, skipped, and unprocessed counts, including partial storage failures.

Current posts, pages, previews, and profile images protect their uploads, including drafts and scheduled posts. Images used only by history appear separately. You can delete one after reviewing the affected versions and confirming that their image will become unavailable. Bulk cleanup excludes these images. History keeps the writing unchanged and marks missing images in its preview. Editing an affected version requires replacing or removing missing image references before saving.

Deletion checks references again and cannot overlap a content save or upload. A repeated upload invalidates an older deletion confirmation. Cleanup does not touch bundled assets, local assets, or backups.

Save open editors before cleanup. The server cannot inspect unsaved edits or browser recovery copies. If cleanup removes an image from an unsaved editor, upload it again or remove its reference before saving. Matching upload URLs on an old domain are conservatively retained. Run only one blog instance against the content database and uploads directory.

RSS caches the complete rendered feed. Recent Posts caches only the configured number of entries, not rendered HTML. Both caches are loaded on demand and invalidated after saving, publishing, unpublishing, or deleting a post. Profile edits also invalidate RSS. Feed readers can revalidate with an ETag. Feed links and images use absolute URLs.

Full pages include Recent Posts in the initial HTML. CSS selects the sidebar or footer placement, and HTMX navigation retains both without fetching the list again.

Run one blog instance per content database. The site profile is loaded at startup and refreshed after successful edits. Edit posts and pages through the application. Direct database writes bypass image-reference updates and cache invalidation.

The administrator's Posts and Pages lists search current titles, summaries, and Markdown, including drafts and scheduled posts. Search is literal and case-insensitive, with title matches first, then summary matches, then content matches. Posts retain their Drafts, Scheduled, and Published groups, with matches ranked within each group. Empty Drafts and Scheduled groups are hidden. Search hides every empty group. Pages appear as a flat list while searching. Clear search to restore the full lists or page tree. Saved versions are not searched.

Deleting from Posts or Pages removes the row without reloading the list, preserves the search and scroll position where the remaining page height allows, and moves keyboard focus to nearby actions. Deleting from an individual post or page, or submitting without JavaScript, returns to its management list.

## Sharing links

Public posts and pages use their summary for the HTML description tag, falling back to the site bio when the summary is blank. The home page and archive use the site bio.

Public pages include a canonical link to their preferred URL, without tracking parameters. Posts use their slug URL, pages use their current page URL, and the home page keeps `/`. Canonical links share the configured origin and visibility rules below. They do not redirect visitors or preserve old slugs.

Public posts, pages, the home page, and the archive include Open Graph metadata in full-page HTML. Post and page previews use their title and summary, falling back to the site bio for an empty summary. Posts and pages prefer their uploaded preview image. Otherwise, content uses its first suitable Markdown image, then the site portrait. Choose or remove the preview image in the editor. It does not appear in the article body. SVGs are skipped. If no suitable image exists, the preview has text only. Images must be publicly accessible for sharing services to retrieve them.

Set `http.publicOrigin` to the deployed site's public address. Preview URLs use that configuration, not request host headers. Drafts, scheduled posts, descendants of unpublished pages, editors, and account pages do not advertise sharing metadata. HTMX fragments omit it because sharing services fetch the full URL. Individual services control preview appearance and may cache older versions.

`/sitemap.xml` lists the home page, archive, published posts, and publicly visible pages using their canonical URLs. It reads current metadata on each request and excludes drafts and descendants of unpublished pages, even for signed-in administrators. Set `http.publicOrigin` before submitting the sitemap to a search engine. The single-document limit is 50,000 URLs or 50 MiB. Larger sites need multiple sitemaps, which this example does not yet generate.

`/robots.txt` allows crawling and advertises the sitemap at the configured public address. Authentication and publication rules protect private content, not `robots.txt`.

## Publishing posts

Open **Posts**, choose **New post**, and write a title and Markdown body. Under **Publish**, choose **Draft**, **Now**, or **Schedule**, then **Save post**. For **Schedule**, enter a future date and time in UTC. Scheduling is available only for posts that have never been published. The Posts page groups drafts, scheduled posts, and published posts.

To remove a post from public view or cancel a schedule, choose **Draft** and save. The original publication date stays unchanged when editing or republishing. Canceling before a scheduled time clears that date. Images remain public only while public content or the site profile references them.

Scheduled posts become public at their saved time, without a background worker. The home page, archive, feed, sitemap, and image handler use the same visibility rule. RSS and Recent Posts caches expire at the next publication time. A server restart does not lose schedules. The server clock must be accurate.

For posts and pages, leave the slug blank to generate it from the title. The field shows a suggestion while you type. If that slug is taken, saving adds `-2`, `-3`, or the next available number. Custom slugs must be unique. Slugs use lowercase ASCII letters, digits, and hyphens, up to 120 characters. If the title produces no usable slug, enter a custom one. Editing a title preserves the existing slug unless you clear or replace it. Changing a slug breaks existing links.

If content is or was published, the editor asks for confirmation before saving a changed or cleared slug. Never-published drafts do not warn. Without JavaScript, the slug field displays the warning but does not ask for confirmation. Page publication history is retained from this version onward. Existing published pages are marked during startup, but earlier publication of pages already in draft cannot be inferred.

Post and page editors warn before closing or following links with unsaved edits. They also request the browser's reload/close warning, which browsers may suppress. With JavaScript enabled, edits are copied to browser storage after a half-second pause and when leaving the editor. Reopen the same editor to choose **Restore copy** or **Discard copy**. Recovery never changes the server's content until you explicitly save. Failed saves retain the copy. Successful saves remove the current and restored copies.

Recovery copies are separate for each administrator, editor, and tab. They stay in that browser profile, including after logout, and expire after seven days. Expired copies are removed when an editor next opens. Copies are not encrypted, synced between devices, or a substitute for backups. Browser-history navigation preserves copies even when no warning appears. Storage failures appear in the editor. Use a private browser profile on shared devices, and save to the server before clearing browser data. Profile and authentication forms do not use draft recovery.

### Saved versions

Choose **History** from the Posts or Pages list, or under **More** when viewing content. The action appears only when more than one saved version exists. Selecting a version updates its preview in place without scrolling or replacing the list. The URL retains the selection for refreshes and bookmarks. Without JavaScript, revision links load a full page.

Choose **Edit** for the latest version or **Edit this version** for an earlier one. The editor starts with that version's title, summary, Markdown, and preview image. The current slug, publication setting, and page placement stay unchanged. Cancel returns to the selected version in History. Nothing changes until you save. If publication is checked, saving makes the edited content live.

Changed content creates a new version. Unchanged content and settings-only changes do not. Restoring old writing preserves intervening versions. In History, **Delete revision** permanently removes an older version after confirmation. The latest version cannot be deleted. Deleting history leaves current content and publication settings unchanged and does not remove uploaded images, backups, or browser recovery copies. Deleting a post or page removes all its history. Existing content gets one initial snapshot when this feature first starts. Earlier versions cannot be recovered. Historical images stay stored but do not become public merely because history references them.

If another tab saves or changes publication after an editor opens, that editor's save is rejected without losing its input. Open the current editor in another tab and compare before saving your changes there. History is server-side saved content. Browser recovery holds unsaved writing.

Back up the content database and uploads before deploying changes.

## Pages and projects

Choose **Pages** to create and manage Markdown pages. Each page has a title, unique slug, optional parent, preview description and image, display order, and publication setting. Use **More → New subpage** on an existing page to select its parent automatically. Pages stay out of RSS and Recent Posts.

Each parent chooses whether to hide subpage previews, show a preview list, or show preview blocks. Blocks use two columns except on mobile. Images appear only in previews, not on the child's content page. An empty image selects the bundled default. Upload controls require JavaScript. Images used only by drafts remain private.

Projects is an ordinary page with slug `projects` and preview blocks enabled. Add individual projects as its subpages. About is a page with slug `about`. Their short URLs are `/projects` and `/about`. Other pages use `/page/{slug}`. Existing `/project/s/{slug}` links remain readable. Changing a slug breaks existing links.

Navigation shows published top-level pages and their immediate children. Deeper pages remain available through their URLs and parent-page previews, and the administrator can manage the complete tree. Siblings with an explicit display order come first, lowest number first. The rest sort by most recently updated, which also breaks matching orders. Mobile collapses subpages behind toggles. Middle and ultrawide layouts show them directly. Hiding previews in a page body does not hide its navigation links.

Draft pages and descendants of draft parents return 404 to visitors. Administrators can view and edit the whole tree. Move or delete a parent's subpages before deleting it. Page content can be empty. New installations have no pages until the administrator creates them.

## Profile

Sign in as the verified administrator and choose **Site Profile** to edit the name, short bio, portrait URL, favicon URL, and background image URL. These values live in the blog database and take effect without a restart. A new installation starts with “My Blog,” a neutral portrait and favicon, and no background. Empty image fields restore those defaults. Image URLs must be local absolute paths or HTTPS URLs.

Each profile image has upload, preview, and reset controls. Upload PNG, JPEG, or GIF files of at most 8 MiB and 20 million pixels. Use PNG for an uploaded favicon. Uploads use the same public image storage as posts and pages. Save the profile to apply the selected images. Uploads require JavaScript. Existing local or HTTPS image URLs remain editable without it. Replacing or clearing an image does not delete the file.

For local images, create an `assets/` directory beside the application `config.yaml` and set `assetsDirectory: assets` in the module configuration. This directory is ignored by Git and is not embedded in the binary. For example, place `portrait.jpg` and `site-icon.ico` there and enter `/local-assets/portrait.jpg` and `/local-assets/site-icon.ico` in Site Profile. Only top-level, non-hidden files are served, without directory listings. These files are public, so keep private files elsewhere and copy the directory when deploying.

## Tests

```sh
GOWORK=off go test ./...
GOWORK=off go vet ./...
HS_TEST_FIREFOX=firefox GOWORK=off go test ./internal/blog -run '^TestBrowser' -count=1
```

Image processing uses the framework's optional [`imaging` package](../../pkg/imaging/image.go). The blog chooses limits and sizes and owns storage, upload throttling, access checks, and editor messages. The package uses Go's JPEG/PNG encoders, `golang.org/x/image/draw` for resizing, and `github.com/bep/imagemeta` for orientation metadata.

From the repository root, run `go test ./pkg/imaging -run '^$' -bench '^BenchmarkProcess$' -benchmem` to measure processing of a generated 3,000 × 2,000 JPEG. This is not a production capacity estimate.
