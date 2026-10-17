# Blog

A port of Trent Nix's original `me.com` blog using HyperServer. It serves Markdown posts, a month-grouped archive, RSS, About and Projects pages, and an admin editor. It runs separately from `cmd/web` and does not load `site` or its diagnostic routes.

## Run locally

From the repository root, set two independent signing keys and your admin email. Keep the same keys when restarting. This command does not read `config.yaml`.

```sh
export BLOG_AUTH_KEY="$(openssl rand -hex 32)"
export BLOG_SESSION_KEY="$(openssl rand -hex 32)"
export BLOG_ADMIN_EMAIL="you@example.com"
read -rsp 'Admin password: ' BLOG_ADMIN_PASSWORD
export BLOG_ADMIN_PASSWORD
go run ./cmd/blog -create-admin
unset BLOG_ADMIN_PASSWORD
go run ./cmd/blog
```

The password must satisfy HyperServer's password rules. Open http://127.0.0.1:8085 and log in. `-create-admin` creates a new account. It never overwrites an existing account or changes its password. Only the configured admin email can edit, even if other accounts exist in the database. Email matching follows HyperServer's account rules.

The command listens on loopback. It is a local application, not a deployment recipe. Registration and mail delivery are not configured. The shared authentication UI includes password recovery, but recovery cannot deliver mail in this application. A proxy, TLS, mail, and public-deployment controls need a separate setup before exposing it publicly.

| Flag | Default | Purpose |
| --- | --- | --- |
| `-port` | `8085` | Loopback HTTP port. |
| `-data-dir` | `blog-data` | Directory containing `blog.db`, including posts, images, accounts, and sessions. |
| `-public-origin` | Loopback URL with the selected port | Absolute HTTP(S) origin for RSS links. Request headers never supply it. |
| `-title` | `Trent Nix` | Blog title. |

The default data directory is ignored by Git. The old blog's database, uploaded files, and production configuration are not imported or modified.

## Write and publish

Use **New Post** after logging in. Posts publish immediately. The editor accepts Markdown, not raw HTML. Leave the slug blank to derive it from the title, or enter lowercase ASCII letters, numbers, and hyphens. If the title produces an empty slug, enter one explicitly.

Use **Upload an image** to upload PNG, JPEG, or GIF files up to 5 MiB and 16 million pixels. Copy the returned Markdown into the editor. Images become public immediately. Editing or deleting a post does not delete images that another post or open editor might use.

Concurrent edits cannot silently overwrite one another. A stale edit or deletion requires reloading the current version. The archive shows 20 posts per page. RSS includes the latest 100 posts and is generated from storage on request.

Public post URLs remain `/post/s/{slug}` and `/post/id/{id}`. ID URLs survive slug changes. The old blog's JSON editing API and Google sign-in are not retained. The editor uses HTML forms and local HTMX instead of EasyMDE. Forms also work without JavaScript.

## Change the application

- [application.go](application.go) assembles rendering, authentication, and modules.
- [storage.go](storage.go) selects and closes the SQLite providers. Replace providers here without changing blog handlers.
- [modules/blog](../../modules/blog) owns routes and embedded templates. Its `Repository` and `ImageStore` contracts keep post storage separate from HTTP and Markdown rendering.
- [modules/blog/sqlite](../../modules/blog/sqlite) owns the initial `blog_posts` and `blog_images` schema. Accounts and sessions retain their providers' separate schema ownership, even though all three share one database file. Future blog schema changes need provider-owned migrations.

The About and Projects copy lives in [blog.html](../../modules/blog/templates/blog.html). The layout uses the original blog's profile photo, icons, backgrounds, typography, and responsive breakpoints. [blog.css](../../modules/blog/static/blog.css) contains the original layout rules. [forms.css](../../modules/blog/static/forms.css) styles the HyperServer forms and admin controls. Recent posts appear in the sidebar on desktop and below the article on narrow screens. Keep `site` while its development routes remain part of framework regression tests.

Markdown rendering adds [Goldmark](https://github.com/yuin/goldmark), pinned in `go.mod`. It has no third-party Go dependencies at the pinned version. Raw HTML and unsafe URL rendering remain disabled. The local HTMX 2.0.11 asset matches the reference application's pinned copy. Its license is included beside the asset.

Source Sans 3 is bundled locally under the [SIL Open Font License](../../modules/blog/static/source-sans-3-LICENSE). Fonts and design assets do not require third-party requests.

## Verify

```sh
go test ./cmd/blog ./modules/blog/...
go test -race ./cmd/blog ./modules/blog/...
HS_TEST_FIREFOX=/usr/bin/firefox go test ./cmd/blog -run TestBlogBrowser -count=1
```

Tests cover storage conflicts, authorization, ordinary and HTMX forms, Markdown escaping, uploads, RSS, restart persistence, and failure paths. The optional browser test runs a real login, invalid submission, publish, edit, delete, and logout cycle against an isolated database.
