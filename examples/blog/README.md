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

Open <http://127.0.0.1:8086/auth/register/email> and register the configured address. Open the verification message in <http://127.0.0.1:8025>, confirm the address, and log in. The **New post** link appears for the verified administrator.

Mailpit captures mail locally without delivering it externally. Stop the server with Ctrl+C.

Set `auth.registrationEnabled: false` after creating the administrator if desired. Login, recovery, and verification remain available. For deployment, configure real SMTP delivery, independent random signing keys, HTTPS, and `http.publicOrigin`.

## Structure

`application.go` selects the blog and authentication modules, the response layout, and the blog's email provider. HyperServer opens configured account and session services and owns their lifetime. `internal/blog` owns the administrator rule, templates, handlers, SQLite storage for posts, projects, and pages, and file-based image storage. The module loads `modules/blog/config.yaml` through the framework. Storage paths are relative to the application configuration directory.

EasyMDE supplies the editor toolbar and upload controls. The server validates all submissions and renders preview and published Markdown with Goldmark. Raw HTML is disabled. Uploaded images remain stored when posts or projects are deleted. This prototype does not import old blog databases.

Set `uploadsDirectory` in the module config to a persistent, application-owned directory. The sample uses `uploads/`, which is ignored by Git. Back up this directory along with the databases. Images use content-based filenames and immutable public URLs with a one-year browser cache lifetime. Files must not be edited in place. Upload a new image to change its content.

RSS caches the complete rendered feed. Recent Posts caches only the configured number of entries, not rendered HTML. Both caches are loaded on demand and invalidated after creating, editing, or deleting a post. Profile edits also invalidate RSS. Feed readers can revalidate with an ETag. Feed links and images use absolute URLs.

Run one blog instance per content database. The site profile is loaded at startup and refreshed after successful edits. Direct database edits require a restart to refresh the profile and caches.

## Projects

Open `/projects` and choose **New project** when signed in as the verified administrator. Projects have a title, card description, Markdown detail page, optional preview image, optional display order, and publication checkbox. Explicit display orders appear first, lowest number first. Projects without an order follow, sorted by most recently updated. Update time also breaks ties between matching orders. Unpublished projects remain visible to the administrator but return 404 to other visitors, and projects stay out of RSS and Recent Posts.

Preview images appear only on listing cards. Omitting or removing one selects the bundled default. Uploading, replacing, or removing the preview requires JavaScript, and saving the project applies the selection. All uploaded images are public, even when used by a draft. Project records use a separate table in the blog database, created at startup without changing existing post records.

## Profile and pages

Sign in as the verified administrator and choose **Site Profile** to edit the name, short bio, portrait URL, favicon URL, and background image URL. These values live in the blog database and take effect without a restart. A new installation starts with “My Blog,” a neutral portrait and favicon, and no background. Empty image fields restore those defaults. Image URLs must be local absolute paths or HTTPS URLs.

Each profile image has upload, preview, and reset controls. Upload PNG, JPEG, or GIF files of at most 8 MiB and 20 million pixels. Use PNG for an uploaded favicon. Uploads use the same public image storage as posts and projects. Save the profile to apply the selected images. Uploads require JavaScript. Existing local or HTTPS image URLs remain editable without it. Replacing or clearing an image does not delete the file.

For local images, create an `assets/` directory beside the application `config.yaml` and set `assetsDirectory: assets` in the module configuration. This directory is ignored by Git and is not embedded in the binary. For example, place `portrait.jpg` and `site-icon.ico` there and enter `/local-assets/portrait.jpg` and `/local-assets/site-icon.ico` in Site Profile. Only top-level, non-hidden files are served, without directory listings. These files are public, so keep private files elsewhere and copy the directory when deploying.

Sign in as the verified administrator and choose **Pages** to create, edit, publish, or delete standalone Markdown pages. Each page has a title and a unique slug. New pages start as drafts, visible only to the administrator. Published pages appear in site navigation, ordered by title. Pages stay out of RSS and Recent Posts.

Pages use `/page/{slug}`. The About navigation link appears only when the page with slug `about` is published. It uses `/about`. Missing, deleted, and unpublished pages return 404 to visitors. Changing a slug breaks existing links. Page content can be empty.

Ultrawide screens show site navigation below the bio, with published projects nested under Projects in their listing order. Mobile uses an expandable project list inside the hamburger menu. Middle widths show a horizontal row beside the bio, with project links beneath Projects. This navigation does not create parent/child relationships between pages.

## Tests

```sh
GOWORK=off go test ./...
GOWORK=off go vet ./...
HS_TEST_FIREFOX=firefox GOWORK=off go test ./internal/blog -run '^TestBrowser' -count=1
```
