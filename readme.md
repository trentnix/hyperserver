# HyperServer

HyperServer is an experimental Go framework for server-rendered web applications with first-class HTMX support. The goal is high performance and low overhead, with code that stays straightforward to read, extend, and maintain.

## Project status

HyperServer is in early development and is not production-ready. The repository is intended for exploration, local experiments, and discussion as the framework takes shape. All exported Go APIs and configuration formats are experimental throughout `v0.x`. Breaking changes are allowed when real usage demonstrates a better contract. There is no backward-compatibility guarantee during this phase.

Known security and correctness issues remain in authentication, account recovery, sessions, and request handling. Startup validation, shutdown, and deployment safeguards are incomplete. Do not expose the example application to the public internet or use it with real accounts or sensitive data.

The reference application accepts only loopback listen addresses. An empty hostname or `localhost` binds to `127.0.0.1`. IPv6 loopback (`::1`) is also supported. This restriction applies to `cmd/web`, not the framework, and does not prevent exposure through a tunnel or reverse proxy.

High performance is a design goal, not an established benchmark result. Test coverage remains limited and does not establish application-wide correctness or security.

![Hyper Gopher](hypergo.png)

## What is here today

- A reference application with server-rendered pages, HTMX fragments, forms, and notifications.
- Import-based module registration with route and initialization hooks.
- Experimental email/password authentication, account verification and recovery, cookie and SQLite session stores, SMTP mail, and request logging.
- SQLite-backed persistence used by the reference application and framework services.

These implementations are a starting point. Services are not yet consistently optional or independently replaceable, and module instances still share process-wide state.

## Direction

- Keep Go's `net/http` model and standard middleware available.
- Prefer the standard library and a small dependency set, even when that means fewer features. Use maintained security implementations rather than custom cryptography.
- Keep rendering and HTMX response handling in the core. Make authentication, sessions, persistence, caching, and mail replaceable services that activate only when needed.
- Preserve self-registering modules while giving each application explicit initialization, dependency resolution, and shutdown.
- Make storage and serialization independently replaceable through contracts based on consumer needs, not a universal database API.
- Support ordinary navigation and form submissions where applications need them. Applications can also depend on HTMX.

The [roadmap](roadmap.md) describes the intended architecture and implementation order. Security and correctness fixes come first, followed by lifecycle work, shared response handling, storage contracts, measured performance work, and deployment validation.

The [architecture decisions](docs/decisions/readme.md) record concrete choices and list unresolved contracts. Accepted decisions describe direction, not necessarily implemented capabilities.

## Exploring the code

Start with [cmd/web](cmd/web) for application composition, [modules/site](modules/site) for the reference application, and [pkg](pkg) for framework components and services. The [configuration template](config-template.yaml) describes the current settings but is not a production configuration.

### Development routes

The local development application imports the [site module](modules/site) in [cmd/web/site_modules.go](cmd/web/site_modules.go). That module registers the diagnostic and sample routes below. Production applications must not import it. HyperServer does not use an application-wide development flag to control module routes.

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/test-email` | Send a test email using the configured mail provider. |
| POST | `/session-example` | Increment and display a session visit counter. |
| GET | `/login` | Display the sample email-login page. |
| GET | `/register` | Display the sample email-registration page. |
| POST | `/logout` | Redirect to the auth logout handler. |

The `/auth/...` routes remain controlled by `auth.enabled`. The reference application remains loopback-only. Omitting the site module does not resolve the framework's outstanding security issues.

### Account email delivery

Reset requests and verification resend email links to the account's stored address. Their HTTP acknowledgments do not include usable links or tokens. Delivery runs during the request using the configured SMTP timeout. Reset acknowledgments confirm receipt of the request, not successful delivery. Delivery failures are logged without including tokens or email bodies.

### Public links and the server address

`http.hostname` serves two roles: the listener uses it, and link generation uses it as a fallback. `http.publicOrigin` overrides the address used for public links without changing where the server listens. For example, a proxy can expose `https://example.com` while HyperServer listens on `127.0.0.1:8080`.

Set `http.publicOrigin` to `https://example.com` in that deployment. Use `HYPERSERVER_HTTP_PUBLICORIGIN` for an environment override. Paths, credentials, queries, and fragments are not allowed. This setting does not configure TLS listeners, cookie security, or trusted proxies.

For direct connections, `publicOrigin` can be left empty. Absolute links then use `http.hostname`, `http.port`, and the incoming connection's TLS state. Request host and forwarding headers do not control those links.

Use `util.BuildPublicURL(r, cfg.HTTP, path, params)` for absolute links. Account emails and reset-form actions use this shared helper. Other modules can use it without depending on email or authentication. Relative links need no public origin.

Recovery response timing and atomic single-use token redemption remain outstanding. The reference application is not ready for public exposure.

### Checks

From the repository root, run the existing checks with:

```sh
go test ./...
go vet ./...
go test -race ./...
```

The [CI workflow](.github/workflows/ci.yml) runs these checks on pushes and pull requests using the Go version in `go.mod`. Test runs bypass cached results and have a five-minute timeout. SQLite tests and the race detector require CGO and a C compiler.

Before fixing a defect, add a regression test and confirm it fails. The HTTP tests cover invalid form submissions and check that rejected input does not create accounts, change passwords, consume reset tokens, save contact messages, or send mail. A passing CI run does not mean the known security issues are resolved.

See [AGENTS.md](AGENTS.md) for repository working agreements and verification guidance.

## Feedback

Feedback on the design, code, and developer experience is welcome, especially concrete examples of applications HyperServer should support. Email me at trentnix at gmail.com.

Send security reports privately to the same address. Do not include credentials, personal data, or exploit details in public issues.
