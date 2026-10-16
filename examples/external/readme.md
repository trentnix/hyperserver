# External Go module

This application has its own `go.mod` and pins a public HyperServer commit. It serves a greeting form and shared notes using public framework packages. Reading notes is public. Saving notes requires login through the built-in email/password provider. Copy this directory outside the repository, then run its tests:

```sh
go test ./...
```

Use Go 1.27.2 or newer, CGO, and a C compiler. Accounts and revocable sessions use SQLite in `accounts.db`. Notes use files in `notes-data` by default. Neither store depends on the other's location or implementation.

## First run

Generate separate signing keys and create a local account. These Bash commands prompt for the credentials rather than putting the password in shell history:

```sh
export EXAMPLE_SESSION_KEY="$(openssl rand -hex 32)"
export EXAMPLE_AUTH_KEY="$(openssl rand -hex 32)"
read -r -p 'Email: ' EXAMPLE_USER_EMAIL
read -r -s -p 'Password: ' EXAMPLE_USER_PASSWORD
echo
export EXAMPLE_USER_EMAIL EXAMPLE_USER_PASSWORD
go run . -create-user
unset EXAMPLE_USER_EMAIL EXAMPLE_USER_PASSWORD
go run .
```

Keep the signing keys in a private environment or secret store and reuse them on restart. Changing the session key invalidates existing login cookies. Do not commit keys or credentials. Passwords must contain at least eight characters, an ASCII letter, a digit, and one of `@$!%*?&`. The maximum is 72 UTF-8 bytes. Repeating `-create-user` for an existing email fails without changing its password.

Open http://127.0.0.1:8083/notes and select **Log in**. The navigation shows a POST-based logout button when signed in. Registration is disabled, and accounts created locally do not require email verification. Mail delivery and email-based recovery are not configured yet. All users see the same notes, not private per-user collections.

To use SQLite for notes instead:

```sh
go run . -notes-store sqlite -notes-path ./notes.db
```

`-notes-path` selects the notes directory or database. `-accounts-path` selects the account/session database and must be the same for account creation and normal startup. Relative paths use the working directory. Switching notes providers does not transfer existing data. Each provider can reopen its own data after a restart.

This is a loopback-only example, not a public deployment configuration. Do not use it for sensitive data or expose it publicly. `example.com/greeting` is the application's Go module path, not a server it contacts.

## Storage and module setup

[main.go](main.go) owns module activation, rendering, middleware, HTTP limits, and shutdown. [accounts.go](accounts.go) configures account and session services through public APIs. [storage.go](storage.go) selects the notes provider. The application closes owned pools and sessions after requests drain. The file provider keeps no handles open between calls.

The [notes repository interface](notes/notes.go) has two operations: create a note and list recent notes. [The handlers](notes/module.go) use the same interface with either provider. Notes are append-only, limited to 1,000 Unicode code points, and displayed newest first, up to 100 at a time. Both providers reject duplicate IDs rather than overwriting notes.

Importing each module registers its factory. Setup supplies the notes repository and write-access middleware, then configures the auth module with the selected email provider. Session middleware loads the current account before routes run. Templates are embedded and prepared during initialization. Request input and stored notes are escaped when rendered.

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

Anonymous note writes redirect to `/auth/login/email` without saving. Authenticated writes redirect to `/notes`: 303 for ordinary navigation or 200 with `HX-Redirect` for HTMX. Field validation returns 200 with error markup. Malformed or oversized form input receives 400 or 413 after access checks. History-restoration requests receive a full page. Cross-origin writes are rejected, and responses use `Cache-Control: no-store`.

Build with `go build -o greeting .`. Embedded templates let the executable run from any directory. Set `-notes-path` to keep storage at a fixed location. `-port 0` selects an available loopback port.

Tests cover both notes providers, failed login, authenticated and anonymous writes, cross-origin requests, logout and cookie replay, and account/session persistence across restarts. They supply isolated databases, keys, and test credentials. The repository's external-module regression test copies this example to a temporary directory and replaces only that copy's HyperServer dependency with the current checkout. The example itself has no local `replace` directive.
