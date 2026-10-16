# External Go module

This application has its own `go.mod` and pins a public HyperServer commit. It serves a greeting form and shared notes using public framework packages. Reading notes is public. Saving notes requires login through the built-in email/password provider. Copy this directory outside the repository, then run its tests:

```sh
go test ./...
```

Use Go 1.27.2 or newer, CGO, and a C compiler. Accounts and revocable sessions use SQLite in `accounts.db`. Notes use files in `notes-data` by default. Neither store depends on the other's location or implementation.

## First run

Start the isolated Mailpit container, generate separate signing keys, and enable registration:

```sh
docker compose up -d
export EXAMPLE_SESSION_KEY="$(openssl rand -hex 32)"
export EXAMPLE_AUTH_KEY="$(openssl rand -hex 32)"
go run . -registration
```

Keep the signing keys in a private environment or secret store and reuse them on restart. Changing the session key invalidates existing login cookies. Do not commit keys or credentials.

Open http://127.0.0.1:8083/notes and select **Register**. Mailpit captures the verification email at http://127.0.0.1:18026. Open its link and confirm your address. You can sign in before verification to request another email, but you cannot save notes until verified. All users see the same notes, not private per-user collections.

Mailpit does not deliver to real inboxes. The sample uses the framework SMTP sender with a three-second timeout and local-only test credentials. A delivery failure leaves the account pending and offers sign-in and resend instructions. Stop the container with `docker compose down`. Captured mail is disposable.

Registration is disabled unless `-registration` is set. For login-only testing without Docker, use `-smtp-port 0`. Provision an account with `go run . -smtp-port 0 -create-user` after setting `EXAMPLE_USER_EMAIL` and `EXAMPLE_USER_PASSWORD` privately in your environment. Locally provisioned accounts do not require verification, and repeating the command for an existing email never changes its password.

`-smtp-port` defaults to Mailpit's loopback port 11025. `-public-origin` selects the origin used in email links and defaults to `http://127.0.0.1:<port>`. Request headers cannot override it. When registration uses `-port 0`, supply an explicit public origin. Registration rejects disabled delivery or a missing origin at startup. Startup checks configuration, not whether Mailpit is reachable.

To use SQLite for notes instead:

```sh
go run . -registration -notes-store sqlite -notes-path ./notes.db
```

`-notes-path` selects the notes directory or database. `-accounts-path` selects the account/session database and must be the same for account creation and normal startup. Relative paths use the working directory. Switching notes providers does not transfer existing data. Each provider can reopen its own data after a restart.

This is a loopback-only example, not a public deployment configuration. Do not use it for sensitive data or expose it publicly. `example.com/greeting` is the application's Go module path, not a server it contacts.

## Storage and module setup

[main.go](main.go) owns module activation, rendering, middleware, HTTP limits, and shutdown. [accounts.go](accounts.go) configures accounts and sessions, [mail.go](mail.go) selects the mail sender, and [storage.go](storage.go) selects the notes provider. Setup uses public framework APIs. The application closes owned pools and sessions after requests drain. The file and SMTP providers keep no handles open between calls.

The [notes repository interface](notes/notes.go) has two operations: create a note and list recent notes. [The handlers](notes/module.go) use the same interface with either provider. Notes are append-only, limited to 1,000 Unicode code points, and displayed newest first, up to 100 at a time. Both providers reject duplicate IDs rather than overwriting notes.

Importing each module registers its factory. `authweb.Email` takes accounts, sessions, and mail once. Its `Setup` method initializes the selected modules and returns a handler with session and identity loading attached. The application uses its authentication guard to protect note writes. Layout data and security headers remain application choices.

Greeting and notes modules prepare a `content.View` during initialization, binding their embedded templates and named partial. Handlers supply request data through `view.Content(r, data)` and use the existing renderer for pages, HTMX fragments, and form errors. Request input and stored notes are escaped when rendered.

The file provider writes each JSON record inside a temporary directory, then renames the directory to `<id>.note`. It requires atomic directory renames, not hard links. Existing nonempty record directories cannot be overwritten. Keep storage private through directory permissions or mount settings. Older `<id>.json` records remain readable, but stop old application processes before using the new writer.

Listings scan records in bounded batches and report corrupt records as errors. This provider is intended for small local datasets and does not guarantee durability across power loss. The SQLite provider creates its initial table and index. Neither provider includes schema upgrades or data migration tooling.

## HTTP behavior

The forms work with ordinary browser navigation. This example does not load HTMX JavaScript. Exercise fragment responses and redirects directly:

```sh
curl -H 'HX-Request: true' 'http://127.0.0.1:8083/?name=Ada'
curl -H 'HX-Request: true' http://127.0.0.1:8083/notes
curl -H 'HX-Request: true' http://127.0.0.1:8083/auth/login/email
curl -i -H 'HX-Request: true' -d 'text=Requires login' http://127.0.0.1:8083/notes
```

Anonymous note writes redirect to `/auth/login/email` without saving. Unverified accounts receive 403. Authorized writes redirect to `/notes`: 303 for ordinary navigation or 200 with `HX-Redirect` for HTMX. Field validation returns 200 with error markup. Malformed or oversized form input receives 400 or 413 after access checks.

History-restoration requests receive a full page. Cross-origin writes are rejected, and responses use `Cache-Control: no-store`. Visiting a verification link displays a confirmation form. Only its POST submission verifies the account. Expired and already-used links are rejected.

Build with `go build -o greeting .`. Embedded templates let the executable run from any directory. Set `-notes-path` to keep storage at a fixed location. `-port 0` selects an available loopback port.

Tests cover both notes providers, login and session persistence, registration, verification, resend, delivery failure, expired links, and replay. They supply isolated databases, keys, credentials, and captured mail. To also test real SMTP delivery and redeem the captured link, start this example's Mailpit container and run:

```sh
EXAMPLE_MAILPIT_TEST=1 go test -run '^TestMailpitRegistration$' -count=1 .
```

The optional test adds one uniquely addressed message without deleting other mail. Ordinary tests do not require Docker. The repository's external-module regression test copies this example to a temporary directory and replaces only that copy's HyperServer dependency with the current checkout. The example itself has no local `replace` directive.
