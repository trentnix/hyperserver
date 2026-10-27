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

Set `recentPostCount` in `modules/blog/config.yaml` to the number of Recent Posts links to show. It must be greater than zero. Restart after changing configuration. The All Posts page always lists every post.

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

Set `uploadsDirectory` in the module config to a persistent, application-owned directory. The sample uses `uploads/`, which is ignored by Git. Back up this directory along with the databases. Images use content-based filenames. Files must not be edited in place. Upload a new image to change its content.

Only the verified administrator can view new uploads and images used exclusively by drafts. An image becomes public when a published post, publicly visible page, or saved site profile references it. Shared images stay public while any public content still uses them. Unpublishing a parent page hides its descendants' image references too.

Image responses recheck access before returning bytes or a cache-validation response. Previously downloaded copies cannot be recalled, including copies cached under the former one-year policy. Startup rejects overlapping `uploadsDirectory` and `assetsDirectory` paths, including symlinks. Keep uploads outside any other publicly served filesystem directory.

RSS caches the complete rendered feed. Recent Posts caches only the configured number of entries, not rendered HTML. Both caches are loaded on demand and invalidated after saving, publishing, unpublishing, or deleting a post. Profile edits also invalidate RSS. Feed readers can revalidate with an ETag. Feed links and images use absolute URLs.

Run one blog instance per content database. The site profile is loaded at startup and refreshed after successful edits. Edit posts and pages through the application. Direct database writes bypass image-reference updates and cache invalidation.

## Sharing links

Public posts and pages use their summary for the HTML description tag, falling back to the site bio when the summary is blank. The home page and archive use the site bio.

Public posts, pages, the home page, and the archive include Open Graph metadata in full-page HTML. Post and page previews use their title and summary, falling back to the site bio for an empty summary. Posts and pages prefer their uploaded preview image. Otherwise, content uses its first suitable Markdown image, then the site portrait. Choose or remove the preview image in the editor. It does not appear in the article body. SVGs are skipped. If no suitable image exists, the preview has text only. Images must be publicly accessible for sharing services to retrieve them.

Set `http.publicOrigin` to the deployed site's public address. Preview URLs use that configuration, not request host headers. Drafts, descendants of unpublished pages, editors, and account pages do not advertise sharing metadata. HTMX fragments omit it because sharing services fetch the full URL. Individual services control preview appearance and may cache older versions.

## Publishing posts

Open **Posts**, choose **New post**, and write a title and Markdown body. Check **Publish post** to make it live when you choose **Save post**. Leave it unchecked to save a private draft. Existing posts show their current publication setting. The Posts page groups drafts and published posts, with links to preview, edit, and delete them.

Uncheck **Publish post** and save to return a post to drafts. **Move to drafts** on the Posts page and **Publish** or **Unpublish** on a post provide shortcuts without editing its content. The original publication date stays unchanged when editing or republishing. Uploaded images remain public even when their post is a draft.

For posts and pages, leave the slug blank to generate it from the title. The field shows a suggestion while you type. If that slug is taken, saving adds `-2`, `-3`, or the next available number. Custom slugs must be unique. Slugs use lowercase ASCII letters, digits, and hyphens, up to 120 characters. If the title produces no usable slug, enter a custom one. Editing a title preserves the existing slug unless you clear or replace it. Changing a slug breaks existing links.

If content is or was published, the editor asks for confirmation before saving a changed or cleared slug. Never-published drafts do not warn. Without JavaScript, the slug field displays the warning but does not ask for confirmation. Page publication history is retained from this version onward. Existing published pages are marked during startup, but earlier publication of pages already in draft cannot be inferred.

Post and page editors warn before closing or following links with unsaved edits. They also request the browser's reload/close warning, which browsers may suppress. With JavaScript enabled, edits are copied to browser storage after a half-second pause and when leaving the editor. Reopen the same editor to choose **Restore copy** or **Discard copy**. Recovery never changes the server's content until you explicitly save. Failed saves retain the copy. Successful saves remove the current and restored copies.

Recovery copies are separate for each administrator, editor, and tab. They stay in that browser profile, including after logout, and expire after seven days. Expired copies are removed when an editor next opens. Copies are not encrypted, synced between devices, or a substitute for backups. Browser-history navigation preserves copies even when no warning appears. Storage failures appear in the editor. Use a private browser profile on shared devices, and save to the server before clearing browser data. Profile and authentication forms do not use draft recovery.

Existing posts stay published when storage adds draft support. Back up the content database and uploads before deploying changes.

## Pages and projects

Choose **Pages** to create and manage Markdown pages. Each page has a title, unique slug, optional parent, preview description and image, display order, and publication setting. Use **New subpage** on an existing page to select its parent automatically. Pages stay out of RSS and Recent Posts.

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
