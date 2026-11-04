# Blog

A Markdown blog with posts, nested pages, images, search, RSS, and administrator-only publishing. It uses HyperServer's configured services, prepared templates, and HTMX responses. Authentication pages share the blog's styling.

This is an experimental application, not a production-readiness guarantee. Run one instance per content database and uploads directory. The current process lock requires Linux, macOS, or BSD. Windows is not supported.

## Run locally

Use the Go version in [go.mod](../../go.mod), CGO, and a C compiler. From the repository root, copy the [application](../../config-template.yaml) and [module](../../modules/blog/config-template.yaml) templates **only if the destination files do not already exist**:

```sh
cp config-template.yaml config.yaml
cp modules/blog/config-template.yaml modules/blog/config.yaml
```

Set `adminEmail` in `modules/blog/config.yaml` before starting. Only that address can register or log in. The supplied configuration requires email verification before administrative access.

You can enable [Google authentication](../google-authentication.md) alongside email or on its own. The login page shows only enabled providers. Existing administrators must log in and connect Google before disabling their original login method.

If a local SMTP inbox is not already running, start Mailpit:

```sh
docker run --rm --name blog-mailpit -p 127.0.0.1:1025:1025 -p 127.0.0.1:8025:8025 -e MP_SMTP_AUTH_ACCEPT_ANY=1 -e MP_SMTP_AUTH_ALLOW_INSECURE=1 axllent/mailpit@sha256:0790692be935ea4420d085694f7985b1f692ca6a8771ab0e0bd8ba75ac180498
```

Mailpit captures mail locally rather than delivering it. In another terminal, start the blog from the repository root:

```sh
go run ./cmd/web
```

Open <http://127.0.0.1:8080/>. First-time setup asks you to register the configured administrator address. Use the verification message at <http://127.0.0.1:8025>, then log in. Your first login opens Site Profile. Add your name and bio, then save. Images are optional. After the first save, you can view your site or write your first post.

Public content, including the homepage, opens after registration and any required verification. Verification leads the administrator to login, and the first login opens Site Profile. Setup progress survives restarts. Existing installations with a verified administrator skip setup. If no administrator exists, enable `auth.registrationEnabled` to register.

A fresh installation shows “My Blog,” neutral portrait/favicon assets, no background, and no posts or pages. Use **Site Profile** for the name, bio, and images. Create About or Projects through **Pages**. Content belongs in the database. Operational settings and the administrator allowlist belong in configuration.

Runtime configuration, databases, uploads, and local assets are ignored by Git. Stop the server with Ctrl+C. Restart after changing configuration. After registering, you can disable registration with `auth.registrationEnabled: false`.

### Existing installations

The blog now runs through `cmd/web` instead of `examples/blog`. The source move does not move configuration or data. To keep an existing installation, build with `go build -o /tmp/hyperserver-blog ./cmd/web` from the repository root, then run that binary from the existing configuration directory. Review [database upgrades](#database-upgrades) before starting it.

If the root `config.yaml` belongs to the old contact demo, keep that configuration separate. Use the blog templates in a new configuration directory and copy the module template into its `modules/blog/` directory. Run the built blog binary there. The old contact demo has been removed.

## Guides

- [Writing and site content](content.md): posts, pages, profile, search, sharing, and saved versions.
- [Images](media.md): uploads, reuse, privacy, cleanup, and optimization.
- [Backup and recovery](backup.md): offline ZIP archives and manual recovery.
- [Framework setup](../application.md): service selection, modules, rendering, and cleanup.

## Code structure

[main.go](../../cmd/web/main.go) loads configuration, holds the maintenance lock, and starts the server. [application.go](../../cmd/web/application.go) selects modules, rendering, and the blog's authentication policy. The framework opens configured account, session, and mail services and owns their lifetime.

[modules/blog](../../modules/blog) owns content rules, templates, handlers, SQLite content storage, and image files. Module configuration loads from `modules/blog/config.yaml`. Relative storage paths use the application configuration directory. Templates, scripts, styles, and default assets are embedded, so rebuild after changing them.

Fresh startup creates blog schema version 1 from the embedded [SQL migration](../../modules/blog/migrations/000001_blog.up.sql). Existing databases must have a clean version 1 in `blog_schema_version`. Startup checks the schema and rejects missing, dirty, or unsupported versions without repairing tables. Accounts and sessions keep their own [migration histories](../storage.md).

## Database upgrades

Before upgrading an existing installation, stop the server and all other writers. Create and verify a [backup](backup.md). Keep the compatible application binary with the backup.

For an unversioned database from the last pre-migration release (`1502fba`), run this once from the application configuration directory:

```sh
/path/to/blog-maintenance adopt-schema
```

Build `blog-maintenance` from this checkout with `go build -o /path/to/blog-maintenance ./cmd/blog-maintenance`. Adoption validates required columns, keys, foreign keys, named indexes, and the page-cycle trigger, then records version 1 in one transaction. Extra indexes are allowed. Validation errors identify the incompatible schema element. Adoption does not change content, history, or image files. Repeating it is safe.

Incomplete or older schemas are rejected. Use the compatible older application to complete its supported upgrades before adopting. Do not add version metadata by hand.

Later upgrades use the same pinned `golang-migrate` CLI as the framework. From the repository root:

```sh
go install -tags sqlite3 github.com/golang-migrate/migrate/v4/cmd/migrate@v4.20.1
migrate -path modules/blog/migrations \
  -database 'sqlite3://file:///absolute/path/blog.db?x-migrations-table=blog_schema_version&_foreign_keys=on' up
```

Keep `x-migrations-table=blog_schema_version` in every command. Run one migration process at a time. The CLI does not acquire the blog's maintenance lock. Migrate accounts and sessions separately if they require an upgrade, even when they share the file. Do not run version 1's `up` on an unversioned database. Adopt it first.

A failed migration rolls back its SQL but leaves a dirty version marker. Inspect the failure before repairing the marker. `force` changes metadata, not tables. Do not use it to bypass an incompatible schema. Version 1's `down` deletes all blog tables. For production rollback, restore a verified backup into a separate directory and use the compatible binary.

Migration files are immutable after release. Add numbered migrations for later changes, update the application's supported schema and validation, and test both fresh installation and upgrades. Set `HS_TEST_MIGRATE` to the pinned CLI executable to run migration integration tests. CI already enables them.

## Deployment limits

Configure real SMTP delivery, HTTPS, and `http.publicOrigin` before public use. The default Mailpit credentials are local-only. Review [proxy settings](../http.md#addresses-tls-and-proxies) and the project's [security status](../../SECURITY.md).

Edit content through the application. Direct database writes bypass reference tracking and cache invalidation. Use persistent storage for databases and uploads, protect configuration files, and keep verified backups off the serving machine. The [container rehearsal](../deployment.md) runs the blog with disposable data.

## Tests

The blog shares the framework's Go module. From the repository root:

```sh
go test ./...
go vet ./...
go test -race ./...
HS_TEST_FIREFOX=firefox go test ./modules/blog -run '^TestBrowser' -count=1
```

Ordinary test runs skip Firefox checks. Recovery tests use isolated data and verify accounts, sessions, content history, and image access after manual extraction of a backup.
