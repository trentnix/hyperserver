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

Open <http://127.0.0.1:8080/> and register the configured administrator address. With email registration, complete any required verification through Mailpit at <http://127.0.0.1:8025>, then log in. Google registration signs you in immediately. Your first login opens Site Profile. Add your name and bio, then save. Images are optional.

Public content opens after registration and any required verification. After saving Site Profile, you can view the site or write a post. Setup progress survives restarts, and verified administrators skip registration. If no administrator exists, enable `auth.registrationEnabled` to register.

A fresh installation shows “My Blog,” neutral portrait/favicon assets, no background, and no posts or pages. Use **Site Profile** for the name, bio, and images. Create About or Projects through **Pages**. Content belongs in the database. Operational settings and the administrator allowlist belong in configuration.

Runtime configuration, databases, uploads, and local assets are ignored by Git. Stop the server with Ctrl+C. Restart after changing configuration. After registering, you can disable registration with `auth.registrationEnabled: false`.

## Guides

- [Writing and site content](content.md): posts, pages, profile, search, sharing, and saved versions.
- [Images](media.md): uploads, reuse, privacy, cleanup, and optimization.
- [Backup and recovery](backup.md): offline ZIP archives and manual recovery.
- [Framework setup](../application.md): service selection, modules, rendering, and cleanup.

## Code structure

[main.go](../../cmd/web/main.go) loads configuration, holds the maintenance lock, and starts the server. [application.go](../../cmd/web/application.go) selects modules, rendering, and the blog's authentication policy. The framework opens configured account, session, and mail services and owns their lifetime.

[modules/blog](../../modules/blog) owns content rules, templates, handlers, SQLite content storage, and image files. Module configuration loads from `modules/blog/config.yaml`. Relative storage paths use the application configuration directory. Templates, scripts, styles, and default assets are embedded, so rebuild after changing them.

Fresh startup creates blog schema version 1 from the embedded [schema](../../modules/blog/migrations/000001_blog.up.sql). Current rows hold identity, publication settings, and a pointer to the selected content version. Posts, pages, and site profiles each store their current and historical content together. Image references belong to individual post or page versions. Read-only views join current pointers to content for rendering and search.

Existing databases must have a clean version 1 in `blog_schema_version`. Startup validates the schema without upgrading existing storage. Accounts and sessions keep their own [migration histories](../storage.md).

## Database migrations

The application supports one current schema, defined by the bootstrap migration. Earlier development layouts and backups are unsupported. Use fresh storage, not a changed version marker.

For later migrations, stop the server and all other writers. Create and verify a [backup](backup.md) with a compatible binary. From the repository root:

```sh
go install -tags sqlite3 github.com/golang-migrate/migrate/v4/cmd/migrate@v4.20.1
migrate -path modules/blog/migrations \
  -database 'sqlite3://file:///absolute/path/blog.db?x-migrations-table=blog_schema_version&_foreign_keys=on' up
```

Keep `x-migrations-table=blog_schema_version` and `_foreign_keys=on` in every command. The CLI does not acquire the blog's maintenance lock. Run only one migration process, with all other writers stopped. Accounts and sessions have separate migrations even when they share the database file.

The baseline's `down 1` deletes all blog content. It does not convert content to the obsolete layout. Back up before running it. Failed migrations roll back their SQL but leave a dirty version marker. Inspect and repair the failure before correcting that marker. `force` changes metadata, not tables or data.

Keep deployed migration files immutable. For a later schema change, add a numbered up/down pair and update the required version, current bootstrap and validation, repositories, and backup compatibility together. Runtime code must support only the resulting schema. Test populated upgrades, downgrades, failed migrations, and fresh startup. Set `HS_TEST_MIGRATE` to the pinned CLI executable to run migration integration tests.

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
