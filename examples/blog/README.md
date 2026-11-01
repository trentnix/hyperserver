# Blog example

A Markdown blog with posts, nested pages, images, search, RSS, and administrator-only publishing. It uses HyperServer's configured services, prepared templates, and HTMX responses. Authentication pages share the blog's styling.

This is an experimental application, not a production-readiness guarantee. Run one instance per content database and uploads directory. The current process lock requires Linux, macOS, or BSD. Windows is not supported.

## Run locally

Use the Go version in [go.mod](go.mod), CGO, and a C compiler. From this directory, copy the templates **only if the destination files do not already exist**:

```sh
cp config-template.yaml config.yaml
cp modules/blog/config-template.yaml modules/blog/config.yaml
openssl rand -hex 32
openssl rand -hex 32
```

Put the two different generated values in `http.session.jwtKey` and `auth.jwtKey`. Set `adminEmail` in `modules/blog/config.yaml` before starting. Only that address can register or log in. The supplied configuration requires email verification before administrative access.

If a local SMTP inbox is not already running, start Mailpit:

```sh
docker run --rm --name blog-mailpit -p 127.0.0.1:1025:1025 -p 127.0.0.1:8025:8025 -e MP_SMTP_AUTH_ACCEPT_ANY=1 -e MP_SMTP_AUTH_ALLOW_INSECURE=1 axllent/mailpit@sha256:0790692be935ea4420d085694f7985b1f692ca6a8771ab0e0bd8ba75ac180498
```

Mailpit captures mail locally rather than delivering it. In another terminal, start the blog from this directory:

```sh
GOWORK=off go run .
```

Open <http://127.0.0.1:8086/>. First-time setup asks you to register the configured administrator address. Use the verification message at <http://127.0.0.1:8025>, then log in. Your first login opens Site Profile.

Public content, including the homepage, opens after registration and any required verification. Verification leads the administrator to login, and the first login opens Site Profile. Setup progress survives restarts. Existing installations with a verified administrator skip setup. If no administrator exists, enable `auth.registrationEnabled` to register.

A fresh installation shows “My Blog,” neutral portrait/favicon assets, no background, and no posts or pages. Use **Site Profile** for the name, bio, and images. Create About or Projects through **Pages**. Content belongs in the database. Operational settings and the administrator allowlist belong in configuration.

Runtime configuration, databases, uploads, and local assets are ignored by Git. Stop the server with Ctrl+C. Restart after changing configuration. After registering, you can disable registration with `auth.registrationEnabled: false`.

## Guides

- [Writing and site content](docs/content.md): posts, pages, profile, search, sharing, and saved versions.
- [Images](docs/media.md): uploads, reuse, privacy, cleanup, and optimization.
- [Backup and recovery](docs/backup.md): offline ZIP archives and manual recovery.
- [Framework setup](../../docs/application.md): service selection, modules, rendering, and cleanup.

## Code structure

[main.go](main.go) loads configuration, holds the maintenance lock, and starts the server. [application.go](application.go) selects modules, rendering, and the blog's authentication policy. The framework opens configured account, session, and mail services and owns their lifetime.

[internal/blog](internal/blog) owns content rules, templates, handlers, SQLite content storage, and image files. Module configuration loads from `modules/blog/config.yaml`. Relative storage paths use the application configuration directory. Templates, scripts, styles, and default assets are embedded, so rebuild after changing them.

Storage startup creates fresh content tables and applies selected additive changes. It is not a general migration system for arbitrary older blog schemas. Back up before upgrading. The framework's account and session schemas have their own [migration rules](../../docs/storage.md).

## Deployment limits

Configure real SMTP delivery, independent signing keys, HTTPS, and `http.publicOrigin` before public use. The default Mailpit credentials are local-only. Review [proxy settings](../../docs/http.md#addresses-tls-and-proxies) and the project's [security status](../../SECURITY.md).

Edit content through the application. Direct database writes bypass reference tracking and cache invalidation. Use persistent storage for databases and uploads, protect configuration files, and keep verified backups off the serving machine. The [container rehearsal](../../docs/deployment.md) exercises `cmd/web`, not this blog.

## Tests

The blog is a separate Go module. From this directory:

```sh
GOWORK=off go test ./...
GOWORK=off go vet ./...
GOWORK=off go test -race ./...
HS_TEST_FIREFOX=firefox GOWORK=off go test ./internal/blog -run '^TestBrowser' -count=1
```

Ordinary test runs skip Firefox checks. Recovery tests use isolated data and verify accounts, sessions, content history, and image access after manual extraction of a backup.
