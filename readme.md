# HyperServer

HyperServer is an experimental Go framework for server-rendered web applications with first-class HTMX support. The goal is high performance and low overhead, with code that stays straightforward to read, extend, and maintain.

## Project status

HyperServer is in early development and is not production-ready. Public APIs and configuration can change without backward compatibility during `v0.x`.

Known security and correctness issues affect authentication, recovery, sessions, request handling, and application lifecycle. Do not expose the reference application to the public internet or use it with real accounts or sensitive data.

The reference application is loopback-only, but a tunnel or reverse proxy can still expose it. This restriction applies to `cmd/web`, not the framework.

High performance is a design goal, not an established benchmark result. Passing tests do not establish production readiness.

![Hyper Gopher](hypergo.png)

## What is here today

- A reference application with server-rendered pages, HTMX fragments, forms, and notifications.
- Import-based module registration with route and initialization hooks.
- Experimental email/password authentication, account verification and recovery, cookie and SQLite session stores, SMTP mail, and request logging.
- SQLite-backed persistence used by the reference application and framework services.

Authentication requires SQLite sessions. Login and account verification rotate the session ID. Password reset or change signs out all devices. Cookie-only sessions are available for non-authentication data.

Applications define access rules through [route-level authorization policies](pkg/services/middleware/authorization_example_test.go), separate from authentication. Policies can inspect the loaded identity and route values. Denial returns 403, and policy failures return 500 without exposing the error, for both ordinary and HTMX requests.

Session data uses JSON, limited to 64 KiB before base64 encoding. Cookies are limited to 4 KiB including their name and attributes. Cookie-store data is signed, not encrypted, and must not contain secrets. See the [session guide](pkg/services/session/readme.md) for supported values and typed reads.

Some services require other framework services and cannot be replaced independently. Module instances share process-wide state.

## Direction

Keep the core focused on rendering and HTMX, with replaceable services and self-registering modules. Preserve Go's `net/http` model, prefer the standard library, and keep dependencies small. Use maintained security implementations rather than custom cryptography. Storage and serialization must be independently replaceable. Applications can use ordinary forms or depend on HTMX.

The [roadmap](roadmap.md) and [architecture decisions](docs/decisions/readme.md) describe intended capabilities and priorities. Security and correctness fixes come first.

## Exploring the code

Use the Go version declared in [go.mod](go.mod), which also controls CI. SQLite tests and the race detector require CGO and a C compiler.

Start with [cmd/web](cmd/web) for application composition, [modules/site](modules/site) for the reference application, and [pkg](pkg) for framework components and services.

The development [site module owns the diagnostic and sample routes](modules/site/router.go). Production applications must not import it. Omitting it does not resolve the framework's outstanding security issues.

`POST /test-email` allows logged-in users who have completed any required account verification. Other users receive 403, and no mail is sent.

Use [config-template.yaml](config-template.yaml) as the starting point for a local `config.yaml`, not a production configuration.

To exercise two storage platforms together, run `go run ./cmd/web -contact-directory ./tmp/contacts`. Accounts stay in SQLite, while the site saves contacts as individual JSON files in a private directory. The file provider requires hard-link support. Without this flag, contacts also use SQLite. This is reference-application wiring, not a framework-wide storage setting.

### Registration and email

Registration is disabled by default. With `auth.enabled`, set `auth.registrationEnabled: true` to allow new accounts. Existing accounts can log in and reset passwords when registration is disabled.

Account verification and password reset use emailed, single-use links. If registration requires verification, startup validates the delivery configuration. Failed delivery leaves the account pending verification so the user can log in and request another email.

Mail is sent during the request. `auth.resetMinimumResponseTime` sets the minimum response time for valid password-reset submissions, defaulting to `2s`. Set it to `0` to disable the wait. Slower requests finish without an additional wait, and `mail.timeout` remains independent. Rate limiting and timing tests under load are still needed.

HyperServer does not save pending delivery work or retry it after a restart. If no email arrives, request another password reset or sign in to resend verification.

### Public links and the server address

`http.listenHost` and `http.port` select the listening address. Set `http.publicOrigin` when public links need a different address, such as behind a reverse proxy:

```yaml
http:
  listenHost: "127.0.0.1"
  port: 8080
  publicOrigin: "https://example.com"
```

For direct connections, leave `publicOrigin` empty. Links then use the listener settings and connection's TLS state, not request host or forwarding headers. Wildcard listeners require an explicit public origin for links. This setting does not configure TLS, cookie security, or trusted proxies. See the configuration template for defaults and validation rules.

### Browser request protection

The reference application uses Go's [CrossOriginProtection](https://pkg.go.dev/net/http#CrossOriginProtection) before session loading. It rejects cross-origin mutations with HTTP 403 for ordinary forms and HTMX, without form tokens. Requests with neither `Sec-Fetch-Site` nor `Origin` are allowed for non-browser clients. When only `Origin` is available, Go compares its host and port with the request Host, not its scheme.

Other applications must include this middleware in their HTTP stack. Reverse proxies must preserve the browser-facing Host and origin headers. `http.publicOrigin` controls generated links, not which origins may submit requests.

Redirects accept local paths beginning with a single `/`, including queries and fragments. Absolute URLs are not accepted, even for the same host. Ordinary requests receive HTTP 303, and HTMX requests receive HTTP 200 with `HX-Redirect`. Invalid login destinations fall back to the configured home path, then `/` if that path is also invalid.

### Expired-record cleanup

Run `go run ./cmd/cleanup -batch-size 100 -timeout 10s` with the same configuration and working directory as `cmd/web`. Each run deletes at most 100 expired rows from each selected SQLite session store and, when authentication is enabled, account-token storage. The tables must already exist. Live records and accounts are unchanged.

Schedule the command externally for regular cleanup. The server does not run cleanup automatically. The command reports deleted counts and exits with an error if either cleanup fails. Expiration checks still reject expired sessions and tokens before cleanup runs.

### Checks

From the repository root, run the existing checks with:

```sh
go test ./...
go vet ./...
go test -race ./...
```

The [CI workflow](.github/workflows/ci.yml) runs these checks on pushes and pull requests. See [AGENTS.md](AGENTS.md) for working agreements and regression-test guidance.

## Feedback

Feedback on the design, code, and developer experience is welcome. Email me at trentnix at gmail.com.

Send security reports privately to the same address. Do not include credentials, personal data, or exploit details in public issues.
