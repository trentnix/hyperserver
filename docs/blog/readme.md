# Blog

A Markdown blog with posts, nested pages, images, search, RSS, and Admin and Poster roles. It uses HyperServer's configured services, prepared templates, and HTMX responses. Authentication pages share the blog's styling.

This is an experimental application, not a production-readiness guarantee. Run one instance per content database and uploads directory. The current process lock requires Linux, macOS, or BSD. Windows is not supported.

## Run locally

Use the Go version in [go.mod](../../go.mod), CGO, and a C compiler. From the repository root, copy the [application](../../config-template.yaml) and [module](../../modules/blog/config-template.yaml) templates **only if the destination files do not already exist**:

```sh
cp config-template.yaml config.yaml
cp modules/blog/config-template.yaml modules/blog/config.yaml
```

Set `adminEmail` in `modules/blog/config.yaml` before starting. That address must register first and becomes the first Admin. The supplied configuration requires email verification before administrative access.

You can enable [Google authentication](../google-authentication.md) alongside email or on its own. The login page shows only enabled providers. Existing administrators must log in and connect Google before disabling their original login method.

If using email authentication without a local SMTP inbox, start Mailpit. Google-only authentication skips this step:

```sh
docker run --rm --name blog-mailpit -p 127.0.0.1:1025:1025 -p 127.0.0.1:8025:8025 -e MP_SMTP_AUTH_ACCEPT_ANY=1 -e MP_SMTP_AUTH_ALLOW_INSECURE=1 axllent/mailpit@sha256:0790692be935ea4420d085694f7985b1f692ca6a8771ab0e0bd8ba75ac180498
```

Mailpit captures mail locally. Start the blog from the repository root:

```sh
go run ./cmd/web
```

Open <http://127.0.0.1:8080/> and register the configured administrator address. With email registration, complete any required verification through Mailpit at <http://127.0.0.1:8025>, then log in. Google registration signs you in immediately. Your first login opens Site Profile. Add your name and bio, then save. Images are optional.

Saving Site Profile returns to the site root with a brief “Profile saved” confirmation. Validation errors stay in the editor and preserve your changes.

Public content opens after registration and any required verification. After saving Site Profile, you can view the site or write a post. Setup progress survives restarts, and verified administrators skip registration. If no administrator exists, enable `auth.registrationEnabled` to register.

A fresh installation shows “My Blog,” neutral portrait/favicon assets, no background, and no posts or pages. Use **Site Profile** for the name, bio, and images. Create About or Projects through **Pages**. Content and user access belong in the database. Operational settings and the first administrator address belong in configuration.

Runtime configuration, databases, uploads, and local assets are ignored by Git. Stop the server with Ctrl+C. Restart after changing configuration. After registering, you can disable registration with `auth.registrationEnabled: false`.

## Users and invitations

Set `multiUser: true` in `modules/blog/config.yaml` and restart to enable invitations. Admins manage access through **Users**.

| Role | Access |
| --- | --- |
| Admin | All posts, pages, site settings, media, and users. |
| Poster | Create, publish, schedule, edit, and delete their own posts. Upload images in the post editor. |

Editing or restoring a post preserves its owner. Posters cannot manage other users' posts, pages, the site profile, the shared media library, or users.

Invitations require mail delivery and `auth.registrationEnabled: true`. Set `inviteExpiration` in the blog configuration to control link lifetime. The default is `168h` (seven days). Resending replaces the previous link. Canceling removes the invitation and its access. Link expiry does not remove the invited account.

Without a valid link, the recipient can still register at the invited address and must complete configured email verification. A valid link proves that address during registration. Google registration uses Google's verified address.

Changing a role or removing access invalidates existing sessions. Removing access keeps the account, credentials, posts, and images so an Admin can invite that address again. Admins cannot remove themselves, but can remove other Admins. The last active Admin cannot be demoted. Changing `adminEmail` after setup does not transfer ownership or grant access to another address.

With `multiUser: false` (the default), invitations and invited registration are disabled. Registered users keep access and can finish email verification. Admins can still manage existing users. **Users** remains visible while multiple users or pending invitations exist.

Accounts, roles, content, and tokens are preserved. Unexpired invitations work again after re-enabling the setting. First-time administrator setup is always available.

## Guides

- [Writing and site content](content.md): posts, pages, profile, search, sharing, and saved versions.
- [Images](media.md): uploads, reuse, privacy, cleanup, and optimization.
- [File downloads](downloads.md): post attachments, limits, access, and storage.
- [Backup and recovery](backup.md): offline ZIP archives and manual recovery.
- [Framework setup](../application.md): service selection, modules, rendering, and cleanup.

## Code structure

[main.go](../../cmd/web/main.go) loads configuration, holds the maintenance lock, and starts the server. [application.go](../../cmd/web/application.go) selects modules, rendering, and the blog's authentication policy. The framework opens configured account, session, and mail services and owns their lifetime.

[modules/blog](../../modules/blog) owns content rules, templates, handlers, SQLite content storage, and image files. Module configuration loads from `modules/blog/config.yaml`. Relative storage paths use the application configuration directory. Templates, scripts, styles, and default assets are embedded, so rebuild after changing them.

The [page layout](../../modules/blog/templates/layout.html) composes named components for setup, navigation, account controls, content, the footer, and dialogs. The footer shares its archive component with HTMX updates. [Rendering setup](../../modules/blog/presentation.go) registers components before preparing templates. Applications can append component overrides or replace layouts after `Site.Configure` and before template preparation. See [prepared rendering and composition](../../pkg/components/readme.md).

Writing operations enforce roles and post ownership, including publishing and restores. They require the authenticated account in the context. HTTP route guards also protect editors and history. Private persistence helpers support authorized operations and trusted test fixtures. New posts belong to the actor. Edits preserve the stored owner.

Fresh startup creates blog schema version 1 from the embedded [initial migration](../../modules/blog/migrations/000001_blog.up.sql). Current rows hold identity, publication settings, and a pointer to the selected content version. Post ownership belongs to the current row. Posts, pages, and site profiles each store their current and historical content together. Read-only views join current pointers to content for rendering and search.

`blog_media` holds upload metadata for images and file downloads. `blog_post_media` and `blog_page_media` track URLs used by each saved revision. References retain URLs to preserve existing image links and host-specific access checks. Posts support PDF, ZIP, and text downloads through the shared metadata. Audio uploads and page component controls are not implemented.

Existing databases must have a clean version 1 in `blog_schema_version` and match the current baseline. Startup validates the schema without upgrading existing storage. Accounts and sessions keep their own [migration histories](../storage.md).

## Database migrations

The initial migration includes shared media metadata and revision references for posts and pages. Earlier baseline databases and backups are unsupported, even if they record version 1. Stop all writers and replace the blog database with fresh storage before running this baseline. Startup rejects the earlier layout without changing it. Never change a version marker to bypass validation.

For future migrations, stop the server and all other writers. Create and verify a [backup](backup.md) with a compatible binary. From the repository root:

```sh
go install -tags sqlite3 github.com/golang-migrate/migrate/v4/cmd/migrate@v4.20.1
migrate -path modules/blog/migrations \
  -database 'sqlite3://file:///absolute/path/blog.db?x-migrations-table=blog_schema_version&_foreign_keys=on' up
```

Keep `x-migrations-table=blog_schema_version` and `_foreign_keys=on` in every command. The CLI does not acquire the blog's maintenance lock. Run only one migration process, with all other writers stopped. Accounts and sessions have separate migrations even when they share the database file.

Reversing the baseline deletes all blog content and ownership metadata. Back up before downgrading. Failed migrations roll back their SQL but leave a dirty version marker. Inspect and repair the failure before correcting that marker. `force` changes metadata, not tables or data.

Keep deployed migration files immutable. For a later schema change, add a numbered up/down pair and update the required version, current bootstrap and validation, repositories, and backup compatibility together. Runtime code must support only the resulting schema. Test populated upgrades, downgrades, failed migrations, and fresh startup. Set `HS_TEST_MIGRATE` to the pinned CLI executable to run migration integration tests.

## Deployment limits

Configure HTTPS and `http.publicOrigin` before public use. Configure SMTP if enabled features need email. Google-only login needs no mail service. Mailpit is for local testing. Review [proxy settings](../http.md#addresses-tls-and-proxies) and the project's [security status](../../SECURITY.md).

Edit content through the application. Direct database writes bypass reference tracking and cache invalidation. Use persistent storage for databases and uploads, protect configuration files, and keep verified backups off the serving machine. The [container rehearsal](../deployment.md) runs the blog with disposable data.

## Tests

The blog shares the framework's Go module. From the repository root:

```sh
go test ./...
go vet ./...
go test -race ./...
HS_TEST_FIREFOX=firefox go test ./modules/blog -run '^TestBrowser' -count=1 -timeout=15m
```

Ordinary test runs skip Firefox checks. Recovery tests use isolated data and verify accounts, sessions, content history, and image access after manual extraction of a backup.
