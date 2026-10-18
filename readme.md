# HyperServer

HyperServer is an experimental Go framework for server-rendered applications with first-class HTMX support. It handles common web infrastructure while keeping Go handlers, HTML templates, and standard middleware visible.

The core provides rendering and HTMX responses. Authentication, sessions, storage, serialization, and mail use replaceable services. HyperServer favors the standard library and fewer dependencies, while retaining established security implementations.

## Project status

HyperServer is not production-ready. Public APIs and configuration can change without backward compatibility during `v0.x`. See the [compatibility policy](docs/compatibility.md) for upgrades and deprecation. Performance and operational readiness still need validation.

The reference application is for local development, not public deployment or sensitive data. Its loopback restriction does not prevent exposure through a tunnel or proxy. The framework itself is not restricted to loopback.

![Hyper Gopher](hypergo.png)

## Start here

Use the Go version in [go.mod](go.mod). SQLite and race tests require CGO and a C compiler.

For a minimal application without configuration files, storage, authentication, or mail:

```sh
go run ./examples/stateless
```

Open http://127.0.0.1:8082. The [example](examples/stateless/readme.md) shows module setup, embedded templates, and full-page and HTMX responses.

For a separate Go module that you can copy outside this repository, see the [external application example](examples/external/readme.md).

To run the reference application, copy [config-template.yaml](config-template.yaml) to the ignored `config.yaml`, configure the enabled services, and run:

```sh
go run ./cmd/web
```

[cmd/web](cmd/web) owns application setup. [modules/site](modules/site) contains the reference pages, contact form, and development routes. Production applications must not load that module.

| Guide | Use it for |
| --- | --- |
| [Application setup](docs/application.md) | Adding modules, replacing services, startup, and cleanup. |
| [Rendering and forms](pkg/components/readme.md) | Prepared templates, HTMX responses, validation, and notifications. |
| [Storage](docs/storage.md) | Repository guarantees, migrations, backend access, and maintenance. |
| [Sessions](pkg/services/session/readme.md) | Store selection, rotation, serialization, and replacement contracts. |
| [Testing](docs/testing.md) | Go tests, browser checks, and diagnostics. |
| [Local deployment](docs/deployment.md) | Containers, health checks, logs, backups, and optional recovery/soak tests. |
| [Roadmap](roadmap.md) | Remaining work and architectural direction. |

## Configuration

The [loader](config/load.go) reads the first `config.yaml` in `.`, `config`, `../config`, or `../../config`. Precedence is defaults, then file, then `HYPERSERVER_` environment values, including empty values. For example, `HYPERSERVER_MAIL_PASSWORD` overrides `mail.password`. Unknown settings and invalid values stop startup. Durations require units, except `0`.

Configuration loads once. Supplying `config.Config` directly bypasses file loading and its defaults. The reference commands resolve storage and TLS paths relative to the selected config file. `app.workingDirectory` overrides that base without changing the process directory.

The template documents settings and defaults. Enabled authentication and signed session stores require independent [signing keys](docs/application.md#signing-keys).

### Registration and email

Registration is disabled by default. With `auth.enabled`, set `auth.registrationEnabled: true` to allow new accounts. Existing accounts can still log in and reset passwords when registration is disabled.

Verification and password reset use emailed, single-use links. If registration requires verification, startup checks delivery configuration. Failed delivery leaves the account pending verification. The user can log in to reach `/auth/request/verify` and resend the email.

Mail is sent during the request. `auth.resetMinimumResponseTime` defaults to `2s` for admitted, valid reset submissions. It delays fast responses, not slow ones, and does not cancel delivery. Set it to `0` to disable the wait. `mail.timeout` is independent. There is no durable delivery queue or automatic retry.

Authentication requires atomic session rotation and server-side revocation. The SQLite session provider supports both. Cookie-only sessions cannot provide authentication storage. Cookie-store data is signed, not encrypted, and must not contain secrets.

### Addresses, TLS, and proxies

`http.listenHost` and `http.port` select the listener. `http.publicOrigin` selects the address used in generated links. When the origin is empty, links use listener settings and the connection's TLS state. Wildcard listeners require an explicit origin. Request Host and forwarding headers never supply the link's address.

For direct HTTPS, set `http.tls.enabled`, `http.tls.certificate`, and `http.tls.key`. The listener supports TLS 1.2 or newer. Restart to reload certificates. Certificate issuance, renewal, and HTTP-to-HTTPS redirects are not automatic.

For one HTTPS-terminating proxy that connects over HTTP:

```yaml
http:
  listenHost: "127.0.0.1"
  port: 8080
  publicOrigin: "https://example.com"
  trustedProxies: ["127.0.0.1/32"]
```

Trust only the direct proxy peer and restrict backend access to it. Trusting loopback also trusts local processes. The proxy must overwrite `X-Forwarded-For` with one client IP and `X-Forwarded-Proto` with `http` or `https`, preserving the browser-facing Host and origin headers. Missing, duplicate, chained, or malformed forwarding values from trusted peers receive 400. Other peers' forwarding headers are ignored.

Verified proxy information controls client-IP limits and cookie security. `publicOrigin` alone does not enable TLS or secure cookies. For a custom HTTP stack, install [proxy verification](pkg/requestinfo/request.go) before rate limits and session loading.

The reference stack includes security headers and Go's `http.CrossOriginProtection`. Other applications must select and install their own middleware. Keep Host and origin headers intact through proxies. For HTTPS-only deployments, `http.hstsMaxAge` enables HSTS for direct or verified proxy HTTPS. The default `0` omits it but does not clear a browser's existing policy.

## Module registration

Imports register factories, not live instances:

```go
func init() {
    handlers.Register(handlers.Descriptor{
        Name: "reports",
        New: func() handlers.Handler { return new(ReportsModule) },
    })
}
```

Factories must return fresh instances without I/O. Application setup supplies shared services and selects a copy of `handlers.Registered()` or a local `[]handlers.Descriptor`. Imports make modules available. The selected catalog controls which modules initialize.

Call `handlers.Setup(ctx, app, routes, catalog, selections)` after configuring services and rendering. It initializes modules, binds routes, and registers reverse-order cleanup. A nil catalog selects no modules. If setup fails, discard the application because its mux may contain routes.

Modules use ordinary handlers and return registration errors:

```go
func (m *ReportsModule) Routes(routes *routing.Routes) error {
    routes.HandleFunc("GET /reports", m.List)
    routes.HandleFunc("POST /reports", m.Create)
    return routes.Err()
}
```

Modules can load optional settings from `modules/<registered-name>/config.yaml` under the application's resource directory. See [module configuration](docs/application.md#optional-module-configuration) for defaults, validation, and the `Configuration` hook.

See [application setup](docs/application.md) for service injection, capability-based dependencies, and lifecycle ownership. The [registration decision](docs/decisions/0001-module-registration.md) explains why catalogs and runtime instances are separate.

## Account storage

Auth accepts an initialized [AccountRepository](pkg/services/user/account_repository.go). Replacement providers must preserve uniqueness, concurrency, expiration, and atomic security operations, not just implement the methods.

For SQLite, import `pkg/services/user/sqlite` and initialize the repository with an application-owned pool:

```go
accounts := sqlite.NewAccountRepository(pool)
if err := accounts.Initialize(ctx); err != nil {
    return err
}
```

The repository does not close the supplied pool. Close owned resources after their consumers stop.

The reference application's `auth.accountStorage.provider` selects storage separately from login providers under `auth.services`. Empty selects SQLite using `database.connection`. Other providers require registration in application setup.

Account email matching is case-insensitive. Equivalent Unicode and Punycode domains share a comparison key. The submitted address remains available for display and delivery. See the [exact rules and provider guarantees](docs/storage.md#storage-guarantees).

Fresh SQLite stores initialize automatically. Existing stores require a supported, clean schema version and explicit migrations. Each provider owns its migration history, even when sharing a database. See [migration commands](docs/storage.md#account-schema-migrations).

To exercise file-backed contacts alongside SQLite accounts, run `go run ./cmd/web -contact-directory ./tmp/contacts`. This sample requires hard-link support. See the [file format](modules/site/storage/readme.md).

Run `go run ./cmd/cleanup -batch-size 100 -timeout 10s` with the same configuration to remove bounded batches of expired sessions and account tokens. Schedule cleanup externally. Expiration checks do not depend on cleanup.

## Request and response rules

The [rendering guide](pkg/components/readme.md) covers pages, fragments, redirects, and forms. Templates and assets are embedded in the reference modules. Rebuild to change bundled assets. Prepared templates are shared, but request data is not.

Rendered responses default to `Cache-Control: no-store` and vary by `HX-Request` and `HX-History-Restore-Request`. Keep that default for personalized or transient content. Public pages can opt into caching with `page.Headers = map[string]string{"Cache-Control": "public, max-age=60"}`. HyperServer does not provide a response cache.

See [performance checks](docs/performance.md) for benchmarks, measured budgets, and the current cache decision.

Redirects accept local paths beginning with a single `/`. Ordinary requests receive 303. HTMX requests receive 200 with `HX-Redirect`. History restoration renders a full page and uses ordinary redirects. The reference site serves [HTMX locally](modules/site/templates/html/js/vendor/htmx/README.md) with script evaluation and local history snapshots disabled.

### Body and form limits

`routing.Routes` limits body reads to 64 KiB by default:

```go
routes := routing.NewRoutes(mux)
routes.Handle("POST /contact", contactHandler)
routes.HandleWithBodyLimit("POST /upload", uploadHandler, 8*routing.MiB)
routes.HandleWithoutBodyLimit("POST /stream", streamHandler)
```

Direct registration on the underlying mux bypasses these defaults. Handlers must check read errors before changing data and return 413 for `*http.MaxBytesError`.

[Form parsing](pkg/components/form/request.go) separately limits decoded fields to 16 KiB and encoded queries to 64 KiB. Its options can change field limits, not route body or query limits. JSON and multipart handlers use their own parsers.

Forms implement ordinary Go binding and validation methods. `ParseAndValidate` returns an error for parsing, binding, or invalid fields. Stop on errors before changing data or sending mail. Account email inputs use `type="text" inputmode="email"` to allow internationalized addresses. Server validation remains required, and syntax validation does not establish deliverability.

### Rate limiting

[Rate limiting](pkg/ratelimit/limiter.go) separates inherited route policies from shared budgets. All allowances below apply per client IP: the verified forwarded address when a trusted proxy is configured, otherwise the direct peer.

#### Route policies: the most specific setting wins

A handler policy replaces its module policy, which replaces the application default. Create registration scopes to select those policies:

```go
policy := ratelimit.Policy{Requests: 20, Window: time.Minute, MaxClients: 4096}
appRoutes, err := routing.NewRoutes(mux).WithRateLimit(policy)
if err != nil {
    return nil, err
}
appRoutes.Handle("GET /ordinary", ordinaryHandler)

// Changing this value does not change appRoutes: scopes copy their policy.
policy.Requests = 10
moduleRoutes, err := appRoutes.WithRateLimit(policy)
if err != nil {
    return nil, err
}
moduleRoutes.Handle("GET /module", moduleHandler)
moduleRoutes.Handle("GET /sibling", siblingHandler)

policy.Requests = 200
busyRoute, err := moduleRoutes.WithRateLimit(policy)
if err != nil {
    return nil, err
}
busyRoute.Handle("GET /poll", pollingHandler)
moduleRoutes.WithoutRateLimit().Handle("GET /unlimited", streamingHandler)
```

| Route | Requests per minute | Rule |
| --- | ---: | --- |
| `/ordinary` | 20 | Inherits the application default. |
| `/module` | 10 | Module policy replaces 20 with 10. |
| `/sibling` | 10 | Has its own counter, independent of `/module`. |
| `/poll` | 200 | Handler policy replaces 10 with 200. Neither parent caps it. |
| `/unlimited` | Unlimited | Explicitly removes the inherited route policy. Body limits still apply. |

Creating a child scope does not change its parent or existing registrations. Exhausting `/module` leaves `/sibling`'s allowance untouched. These are per-route defaults, not a combined module or application budget.

#### Counters belong to registered patterns

Continuing with the 10-request module policy:

```go
moduleRoutes.Handle("GET /items/{id}", getItemHandler)
moduleRoutes.Handle("POST /items/{id}", updateItemHandler)
```

Within one window, ten GET requests to `/items/one` exhaust the allowance for GET `/items/two` and HEAD `/items/one` too. Changing path parameters or query strings does not create a fresh counter. POST `/items/one` still has its own 10-request allowance because it is a separate registration.

#### Shared budgets: every attached limit must allow the request

Reuse one named limiter when several routes must share a combined allowance:

```go
mailBudget, err := ratelimit.New("shared mail", ratelimit.Policy{
    Requests: 5, Window: time.Minute, MaxClients: 4096,
})
if err != nil {
    return nil, err
}
moduleRoutes.Handle("POST /contact", mailBudget.Handler(contactHandler))
busyRoute.Handle("POST /resend", mailBudget.Handler(resendHandler))
moduleRoutes.WithoutRateLimit().Handle("POST /feedback", mailBudget.Handler(feedbackHandler))
```

From one IP within the same window, three admitted contact requests and two resend requests exhaust the shared mail budget. The next request to any of these three routes receives 429. Neither the 200-request override on `/resend` nor the opt-out on `/feedback` bypasses that budget. Unrelated routes, such as `/poll`, do not consume the mail budget.

Each limiter counts admission before calling the next handler. A later rejection does not refund earlier counters. Do not wrap a request twice with the same limiter instance.

#### Application defaults are not application-wide budgets

The reference application's [configuration](config-template.yaml) exposes both settings. Both are off by default. To enable a 20-request default and a separate 120-request shared budget:

```yaml
http:
  defaultRateLimit:
    enabled: true
    requests: 20
  sharedRateLimit:
    enabled: true
    requests: 120
```

Both settings default to one-minute windows and 4,096 tracked IPs per limiter. With this configuration, an ordinary route allows 20 requests per route window. A route with a 200-request override can admit at most 120 during one shared window before the shared budget rejects it. Other requests from the same IP also consume that shared allowance, including pages, assets, logout, and unmatched requests.

The shared application budget runs before session loading. It also covers direct registrations on the underlying mux, which bypass inherited route policies. In another application, wrap the HTTP stack with `budget.Handler(stack)` before session loading to apply a shared budget.

The reference modules set 20 requests/minute for each authentication mutation route and 10 for each contact/test-email route. Logout explicitly opts out of the inherited policy, but not the shared application budget.

Configure their client capacities separately from the HTTP defaults:

```yaml
auth:
  rateLimit:
    maxClients: 10000
app:
  siteRateLimit:
    maxClients: 2000
```

These capacities apply per route, not across a module. Both default to 4,096 and must be positive. Override them with `HYPERSERVER_AUTH_RATELIMIT_MAXCLIENTS` and `HYPERSERVER_APP_SITERATELIMIT_MAXCLIENTS`. HTTP rate-limit capacities do not change these settings.

Startup warns when a route's allowance exceeds an enabled `http.sharedRateLimit` with the same window duration. The warning identifies the route and both per-IP allowances. Startup continues with both limits enforced. Different windows and explicit opt-outs do not trigger this warning.

#### Client capacity is a temporary admission cap

With `maxClients: 4096`, if all entries are unexpired, a new IP receives 429 with `Retry-After`—even on its first request. The limiter does not replace active entries or reset their counters.

Existing clients retain their remaining allowances. New IPs can enter as entries expire. A route limiter restricts only its route, while a shared application limiter can reject new IPs across the application. This is not a permanent user limit, but an undersized capacity can block legitimate newcomers.

Size capacity for distinct IPs within the window, including anonymous clients, not registered accounts. Evicting active entries would let clients bypass throttling. Expired entries are removed on later requests, not by a background worker.

#### Responses and limits

Each window starts with the client's first admitted request to that limiter. Throttled requests receive a plain-text 429 with `Retry-After`, including HTMX requests. With request logging configured, rejections identify the registered route or named shared budget, its settings, and whether its allowance or client capacity was exhausted. Submitted account values do not affect recovery throttling or appear in these rejection logs.

Clients behind a NAT or an unconfigured proxy share an allowance. Memory capacity applies per limiter, not across all routes. State resets on restart and is not shared across servers. Fixed windows allow bursts near window boundaries. These controls do not prevent distributed abuse or cap concurrent work.

### Checks

```sh
go test ./...
go vet ./...
go test -race ./...
```

[CI](.github/workflows/ci.yml) runs Go and Firefox checks in independent jobs. Browser tests are opt-in locally. See the [testing guide](docs/testing.md) and [migration-test setup](docs/storage.md#account-schema-migrations).

## Feedback

Email design feedback to [trentnix@gmail.com](mailto:trentnix@gmail.com). For vulnerabilities, follow the [security-reporting policy](SECURITY.md).

## License

HyperServer is available under the [MIT License](LICENSE), copyright Trent Nix. Third-party components retain their own licenses.
