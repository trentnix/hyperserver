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

Set `recentPostCount` in `modules/blog/config.yaml` to the number of Recent Posts links to show. It must be greater than zero. Restart after changing configuration. The All Posts page always lists every post.

Start a local SMTP inbox if one is not already running:

```sh
docker run --rm --name blog-mailpit -p 127.0.0.1:1025:1025 -p 127.0.0.1:8025:8025 -e MP_SMTP_AUTH_ACCEPT_ANY=1 -e MP_SMTP_AUTH_ALLOW_INSECURE=1 axllent/mailpit
```

Then start the blog:

```sh
GOWORK=off go run .
```

Open <http://127.0.0.1:8086/auth/register/email> and register `trentnix@gmail.com`. Other addresses cannot register or log in. Open the verification message in <http://127.0.0.1:8025>, confirm the address, and log in. The **New post** link appears for the verified administrator. Mailpit captures mail locally and does not deliver it to Gmail. Stop the server with Ctrl+C.

Set `auth.registrationEnabled: false` after creating the administrator if desired. Login, recovery, and verification remain available. For deployment, configure real SMTP delivery, independent random signing keys, HTTPS, and `http.publicOrigin`.

## Structure

`application.go` selects the blog and authentication modules, the response layout, and the blog's email provider. HyperServer opens configured account and session services and owns their lifetime. `internal/blog` owns the administrator rule, templates, handlers, SQLite post and project repositories, and file-based image storage. The module loads `modules/blog/config.yaml` through the framework. Storage paths are relative to the application configuration directory.

EasyMDE supplies the editor toolbar and upload controls. The server validates all submissions and renders preview and published Markdown with Goldmark. Raw HTML is disabled. Uploaded images remain stored when posts or projects are deleted. This prototype does not import old blog databases.

Set `uploadsDirectory` in the module config to a persistent, application-owned directory. The sample uses `uploads/`, which is ignored by Git. Back up this directory along with the databases. Images use content-based filenames and immutable public URLs with a one-year browser cache lifetime. Files must not be edited in place. Upload a new image to change its content.

RSS caches the complete rendered feed. Recent Posts caches only the configured number of entries, not rendered HTML. Both caches are loaded on demand and invalidated after creating, editing, or deleting a post. Feed readers can revalidate with an ETag. Feed links and images use absolute URLs. Run one blog instance per post database. Direct database edits require a restart to refresh the caches.

## Projects

Open `/projects` and choose **New project** when signed in as the verified administrator. Projects have a title, card description, Markdown detail page, optional preview image, optional display order, and publication checkbox. Explicit display orders appear first, lowest number first. Projects without an order follow, sorted by most recently updated. Update time also breaks ties between matching orders. Unpublished projects remain visible to the administrator but return 404 to other visitors, and projects stay out of RSS and Recent Posts.

Preview images appear only on listing cards. Omitting or removing one selects the bundled default. Uploading, replacing, or removing the preview requires JavaScript, and saving the project applies the selection. All uploaded images are public, even when used by a draft. Project records use a separate table in the blog database, created at startup without changing existing post records.

## Tests

```sh
GOWORK=off go test ./...
GOWORK=off go vet ./...
HS_TEST_FIREFOX=firefox GOWORK=off go test ./internal/blog -run '^TestBrowser' -count=1
```
