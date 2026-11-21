# Blog

The included application supports Markdown posts, nested pages, media, search, RSS, and Admin and Poster roles. It is experimental. Run one instance per content database and uploads directory. Its process lock supports Linux, macOS, and BSD, not Windows.

## Run locally

Use the Go version in [go.mod](../../go.mod), CGO, and a C compiler. From the repository root, copy the [application](../../config-template.yaml) and [module](../../modules/blog/config-template.yaml) templates only if the destination files do not exist:

```sh
cp config-template.yaml config.yaml
cp modules/blog/config-template.yaml modules/blog/config.yaml
```

Set `adminEmail` in `modules/blog/config.yaml`. That address must register first and becomes the first Admin. The supplied email configuration requires verification before administrative access. [Google authentication](../google-authentication.md) can run alongside email or on its own. Connect existing administrators to Google before disabling their previous login method.

For local email authentication, start Mailpit. Google-only authentication needs no mail service:

```sh
docker run --rm --name blog-mailpit -p 127.0.0.1:1025:1025 -p 127.0.0.1:8025:8025 -e MP_SMTP_AUTH_ACCEPT_ANY=1 -e MP_SMTP_AUTH_ALLOW_INSECURE=1 axllent/mailpit@sha256:0790692be935ea4420d085694f7985b1f692ca6a8771ab0e0bd8ba75ac180498
go run ./cmd/web
```

Open <http://127.0.0.1:8080/> and register the configured administrator. Complete required email verification through Mailpit at <http://127.0.0.1:8025>, then log in. Google registration signs in immediately.

First login opens Site Profile. Add your name and bio, then save. Images are optional. Setup progress survives restarts.

A fresh installation shows “My Blog,” neutral images, and no content. Manage profile content through **Site Profile** and create About or Projects through **Pages**. Runtime configuration, databases, uploads, and local assets are ignored by Git. Stop with Ctrl+C and restart after configuration changes. If no administrator exists, registration must remain enabled. After setup, you can set `auth.registrationEnabled: false`.

## Users and invitations

Set `multiUser: true` in the blog configuration and restart to enable invitations. Admins manage access through **Users**.

| Role | Access |
| --- | --- |
| Admin | All posts, pages, site settings, media, and users. |
| Poster | Create, publish, schedule, edit, and delete their own posts. Upload images in the post editor. |

Edits and restores preserve post ownership. Posters cannot manage other authors' posts, pages, profile settings, the shared media library, or users.

Invitations require mail and `auth.registrationEnabled: true`. `inviteExpiration` defaults to `168h`. Resending replaces the link. Canceling removes the invitation and access.

Expiry leaves the invited account intact. A valid link proves the email address during registration. Without it, recipients can register at the invited address and complete configured verification. Google uses its verified address.

Role changes and access removal invalidate sessions. Removal preserves credentials, posts, and uploads so an Admin can invite the address again. Admins cannot remove themselves or demote the last active Admin. Changing `adminEmail` after setup grants no new access or ownership.

With `multiUser: false`, invitations and invited registration stop. Existing users keep access and can finish verification. Admins can still manage them. **Users** remains visible while multiple users or pending invitations exist. Unexpired invitations work again after re-enabling the setting. Initial administrator setup remains available.

## Guides

- [Writing and site content](content.md): publication, pages, profile, history, recovery, and navigation.
- [Content components](components.md) for posts and pages.
- [Images](media.md): uploads, galleries, privacy, cleanup, and optimization.
- [Series](series.md): shared reading order and conflict handling.
- [Backup and recovery](backup.md): offline archives and staged recovery.
- [Component development](component-development.md): registration, formats, rendering, and editor contracts.
- [Tests](../testing.md) and [deployment rehearsal](../deployment.md).

## Code and storage

[main.go](../../cmd/web/main.go) loads configuration, holds the maintenance lock, and starts the server. [application.go](../../cmd/web/application.go) selects storage providers, modules, and blog admission policy. The framework owns services it opens. [modules/blog](../../modules/blog) owns writing rules, templates, content storage, and uploads.

The [layout](../../modules/blog/templates/layout.html) composes named components for navigation, account controls, content, dialogs, and the footer. [presentation.go](../../modules/blog/presentation.go) registers defaults before template preparation. Applications can override layouts and components after `Site.Configure` and before preparation. See [rendering and composition](../../pkg/components/readme.md).

Writing operations require the authenticated actor and enforce roles and post ownership. New posts belong to that actor. HTTP guards also protect editors and history. Direct database writes bypass access, reference tracking, and cache invalidation.

Posts, pages, and profiles store revisions separately from current identity and publication settings. Current rows point to selected revisions. Post ownership belongs to the current row. Posts and pages share the component editor, `blog_media` metadata, and revision references for uploads. Series use `blog_collections` and `blog_collection_content`, separate from Markdown components.

## Database migrations

Fresh storage bootstraps blog schema 2 from the embedded [baseline](../../modules/blog/migrations/000001_blog.up.sql) and [collections migration](../../modules/blog/migrations/000002_collections.up.sql). Existing databases must have clean schema 2 and pass structural validation. Startup does not upgrade them. Version 1 databases using the current baseline can apply migration 2, which preserves posts. Earlier development schemas remain unsupported.

Stop all writers and verify a [backup](backup.md) before migrating. Install the CLI and follow the shared [migration precautions](../storage.md#account-schema-migrations), using the blog path and version table:

```sh
migrate -path modules/blog/migrations \
  -database 'sqlite3://file:///absolute/path/blog.db?x-migrations-table=blog_schema_version&_foreign_keys=on' up
```

Downgrading migration 2 deletes series data. Reversing the baseline deletes all blog content and ownership metadata. Accounts and sessions have independent version tables. Never change a version marker to bypass validation. Deployed migrations must remain immutable. Add numbered migration pairs and update bootstrap, validation, repositories, and backup compatibility together.

## Public deployment

Configure HTTPS, `http.publicOrigin`, and mail when enabled features need it. Mailpit is for local testing. Review [proxy settings](../http.md#addresses-tls-and-proxies), [backup procedures](backup.md), and [security status](../../SECURITY.md). Keep databases and uploads on persistent storage, protect configuration, and store verified backups off-host.
