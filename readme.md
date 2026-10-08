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

### Account storage

`auth.accountStorage.provider` selects account persistence, separate from the login providers under `auth.services`. Empty or omitted selects `sqlite`, which uses the application-owned `database.connection`. SQLite accepts no `auth.accountStorage.options`. Unknown provider names stop startup when authentication is enabled.

Applications can register another provider before calling `InitializeAccounts`:

```go
if err := app.RegisterAccountProvider("company", server.AccountProvider{
    Open: openCompanyAccounts,
    OpenMaintenance: openCompanyTokenMaintenance, // Optional. Opens existing storage only.
}); err != nil {
    return err
}
if err := app.InitializeAccounts(ctx); err != nil {
    return err
}
```

The application supplies the factories in [AccountProvider](pkg/server/accounts.go). `Open` receives the startup context and a copy of the configured options. It returns an initialized `user.AccountRepository`, an optional close function for resources it owns, and an error. HyperServer calls close after failed setup or during shutdown after requests drain. Borrowed pools must not be closed by the provider.

Select the registered name and its options in configuration:

```yaml
auth:
  accountStorage:
    provider: company
    options:
      connection: your-provider-connection
```

Provider registrations belong to one application. Names are case-sensitive, and `sqlite` is reserved. Supplying `app.AccountRepository` directly bypasses selection and initialization. The supplier must initialize that repository and close any resources the application does not already own. The reference application ships only SQLite. Its shared database and session setup remain separate from account-provider selection.

`OpenMaintenance` returns the separate [TokenMaintenance](pkg/services/user/cleanup.go) capability, an optional close function, and an error. It must not create or migrate tables. `CleanupAccountTokens` opens this capability, runs one batch, and closes its resources even when opening or deletion fails. An initialized repository can also implement `TokenMaintenance` directly. A missing capability returns an error rather than silently skipping cleanup.

### Logging

The reference application uses [SlogLogger](pkg/services/logger/sloglogger.go) with JSON output on stderr at Info level. Records use slog's `time`, `level`, and `msg` fields, with uppercase levels. Request records include `requestID`, `method`, and the path without query values. The default handler does not sample messages or add caller locations or stack traces.

Pass a `slog.Handler` to `logger.NewSlogLogger` to choose a writer, text output, levels, source locations, or `ReplaceAttr` redaction. Nil selects the default JSON handler. Application code must not log secrets. The adapter preserves handler redaction and `slog.LogValuer` behavior but does not automatically recognize sensitive fields.

### Rendered-response caching

`Content.Render` adds `Vary: HX-Request` to pages and fragments without removing existing `Vary` fields. An existing `Vary: *` already covers every request header. Rendered responses default to `Cache-Control: no-store`, including the reference site's personalized navigation and notifications. Static-file handlers are unchanged.

For a public page with no user-specific content, secrets, or session changes, opt into caching before rendering:

```go
page.Headers = map[string]string{"Cache-Control": "public, max-age=60"}
```

An existing `Cache-Control` response header takes precedence over `page.Headers`. Authentication routes and site errors enforce `no-store`. Keep that policy for sensitive or transient content. A handler can use `private, max-age=60` for content that may be stored in the user's browser but must not be stored in shared caches. These headers define cache behavior. HyperServer does not yet provide a response cache.

### Registration and email

Registration is disabled by default. With `auth.enabled`, set `auth.registrationEnabled: true` to allow new accounts. Existing accounts can log in and reset passwords when registration is disabled.

Account verification and password reset use emailed, single-use links. If registration requires verification, startup validates the delivery configuration. Failed delivery leaves the account pending verification so the user can log in and request another email.

Users who still need verification land on `/auth/request/verify` after login. The page explains restricted access and offers a resend form. Signed-in users can return to that page at any time. Only submitting the form sends email.

Mail is sent during the request. `auth.resetMinimumResponseTime` sets the minimum response time for admitted, valid password-reset submissions, defaulting to `2s`. Set it to `0` to disable the wait. Slower requests finish without an additional wait, and `mail.timeout` remains independent. Timing tests under load are still needed.

HyperServer does not save pending delivery work or retry it after a restart. If no email arrives, request another password reset or sign in to resend verification.

### Addresses, TLS, and proxies

`http.listenHost` and `http.port` select the listening address. For direct connections, leave `http.publicOrigin` empty to build links from the listener settings and connection's TLS state. Wildcard listeners require an explicit public origin for links. Request host and forwarding headers never supply the link's address.

For direct HTTPS, configure a PEM certificate chain and its matching private key:

```yaml
http:
  listenHost: "127.0.0.1"
  port: 8443
  tls:
    enabled: true
    certificate: "certs/server.pem"
    key: "certs/server.key"
```

The listener supports TLS 1.2 or newer. Invalid or unreadable files stop startup. Relative paths use `app.workingDirectory` when configured. Restart to reload certificates. Certificate issuance, renewal, and HTTP-to-HTTPS redirects are not automatic.

For a local reverse proxy that terminates HTTPS and connects to HyperServer over HTTP:

```yaml
http:
  listenHost: "127.0.0.1"
  port: 8080
  publicOrigin: "https://example.com"
  trustedProxies: ["127.0.0.1/32"]
```

`trustedProxies` lists the direct proxy peer's CIDR ranges and requires `publicOrigin`. Empty means trust no forwarding headers. Use only the proxy's addresses, not broad client networks. Restrict backend access to the proxy. Trusting loopback also trusts other local processes. The reference application remains loopback-only and unsuitable for public deployment.

The supported setup has one edge proxy. It must overwrite `X-Forwarded-For` with exactly one client IP and `X-Forwarded-Proto` with `http` or `https`. It must preserve the browser-facing Host and origin headers. Missing, duplicate, chained, or malformed forwarding values from a trusted peer receive 400 before rate limiting or session loading. Forwarding headers from other peers are ignored. `Forwarded` and `X-Forwarded-Host` are not used.

Verified proxy information supplies rate-limit client IPs and session-cookie security. Direct HTTPS and verified proxy HTTPS set `Secure` cookies. Setting `publicOrigin` alone does not enable TLS or make cookies secure. Go's `RemoteAddr` and `r.TLS` still describe the actual backend connection. Other applications can install [requestinfo.NewProxyMiddleware](pkg/requestinfo/request.go) before rate limits and sessions and use `ClientIP` and `IsHTTPS` to read verified values.

### Browser request protection

The reference application sets `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, and disables camera, microphone, and geolocation access through `Permissions-Policy`. Its [Content Security Policy](cmd/web/security.go) allows local assets, the pinned HTMX script, and Google Fonts. It blocks inline scripts, `eval`, framing, and cross-origin form submissions. Headers also cover fragments and error responses.

The site disables HTMX's [evaluation, fragment scripts, inline indicator styles, and local history cache](https://htmx.org/docs/#config). Put scripts and styles in external files. Console messages render as escaped text and are logged by `site.js`. Other applications must choose a CSP that matches their assets and HTMX features. The optional [SecurityHeaders middleware](pkg/services/middleware/security.go) accepts that policy and must wrap handlers that can reject requests.

For an HTTPS-only deployment, set `http.hstsMaxAge` to a lifetime in seconds, for example `31536000` for one year. HSTS tells browsers to use HTTPS for future visits. It is sent only for direct or verified proxy HTTPS, without `includeSubDomains` or `preload`. The default `0` omits the header. It does not clear a [policy the browser already remembers](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Strict-Transport-Security). Install `NewHSTS` after proxy verification in a custom HTTP stack.

Session cookies use `HttpOnly`, `SameSite=Lax`, `Path=/`, no `Domain`, and a lifetime bounded by session configuration. `Secure` requires direct or verified proxy HTTPS. HTTP is supported for local development, not public authentication traffic.

The reference application uses Go's [CrossOriginProtection](https://pkg.go.dev/net/http#CrossOriginProtection) before session loading. It rejects cross-origin mutations with HTTP 403 for ordinary forms and HTMX, without form tokens. Requests with neither `Sec-Fetch-Site` nor `Origin` are allowed for non-browser clients. When only `Origin` is available, Go compares its host and port with the request Host, not its scheme.

Other applications must include this middleware in their HTTP stack. Reverse proxies must preserve the browser-facing Host and origin headers. `http.publicOrigin` controls generated links, not which origins may submit requests.

[Route registration](pkg/routing/routes.go) limits body reads to 64 KiB by default using `http.MaxBytesReader`. For application-supplied handlers:

```go
routes := routing.NewRoutes(mux)

routes.Handle("POST /contact", contactHandler)
routes.HandleWithBodyLimit("POST /upload", uploadHandler, 8*routing.MiB)
routes.HandleWithoutBodyLimit("POST /stream", streamHandler)
```

Prefer a finite limit. Direct registration on the underlying `http.ServeMux` bypasses these defaults. Handlers must check read errors before changing data and return 413 for `*http.MaxBytesError`.

[Form parsing](pkg/components/form/request.go) limits decoded field names and values to 16 KiB and encoded query strings to 64 KiB. `ParseWithOptions` and `ParseAndValidateWithOptions` accept an explicit field limit or `UnlimitedFields: true`. These options do not change body or query limits. Account and contact forms return 413 for oversized input, 400 for malformed forms, and 415 for unsupported encodings. JSON and multipart handlers use their own parsers under the route's body limit.

Redirects accept local paths beginning with a single `/`, including queries and fragments. Absolute URLs are not accepted, even for the same host. Ordinary requests receive HTTP 303, and HTMX requests receive HTTP 200 with `HX-Redirect`. Invalid login destinations fall back to the configured home path, then `/` if that path is also invalid.

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

These settings bound tracked IPs per route, not requests per IP or the total across a module. Both default to 4,096 and must be positive. Environment overrides are `HYPERSERVER_AUTH_RATELIMIT_MAXCLIENTS` and `HYPERSERVER_APP_SITERATELIMIT_MAXCLIENTS`. Changing either HTTP rate-limit capacity does not change these module capacities.

At startup, the reference application logs a warning if a route's allowance exceeds `http.sharedRateLimit` for the same window duration. The warning names the route and both per-IP allowances so you can confirm the restriction is intentional. Startup continues and both limits still apply. Equal or lower allowances, different window durations, explicit opt-outs, and a disabled shared budget do not trigger this warning.

#### Client capacity is a temporary admission cap

With `maxClients: 4096`, if all 4,096 tracked IPs have unexpired entries, a request from a new, 4,097th IP receives 429 with `Retry-After`. This applies even to that IP's first request. The limiter does not replace an existing entry or reset another client's counter.

Existing clients can still use their remaining request allowances. Once an entry expires and capacity is available, a new IP can be admitted. A route limiter restricts only that route. A shared application limiter can reject the new IP across the application. This is not a permanent user limit, but an undersized capacity can block legitimate newcomers.

Choose capacity for the distinct IPs expected within a limiter's window, not the number of registered accounts. Anonymous requests occupy entries too. Active entries are never evicted to make room because eviction would erase their counters and allow throttling to be bypassed. Expired entries are removed when the limiter receives another request, not by a background worker.

#### Responses and limits

Each window starts with the client's first admitted request to that limiter. Throttled requests receive a plain-text 429 with `Retry-After`, including HTMX requests. With request logging configured, rejections identify the registered route or named shared budget, its settings, and whether its allowance or client capacity was exhausted. Submitted account values do not affect recovery throttling or appear in these rejection logs.

Clients behind a NAT or an unconfigured proxy share an allowance. Memory capacity applies per limiter, not across all routes. State resets on restart and is not shared across servers. Fixed windows allow bursts near window boundaries. These controls do not prevent distributed abuse or cap concurrent work.

### Expired-record cleanup

Run `go run ./cmd/cleanup -batch-size 100 -timeout 10s` with the same configuration and working directory as `cmd/web`. Each run deletes at most 100 expired rows from each selected server-side session store and, when authentication is enabled, account-token storage. Storage providers own maintenance setup and pool handling. The tables must already exist. Live records and accounts are unchanged.

The supplied command includes the built-in providers. A custom account provider must also be registered in the application's maintenance executable. Unknown providers and missing maintenance capabilities produce an error without falling back to SQLite. Independent session cleanup still runs.

Schedule the command externally for regular cleanup. The server does not run cleanup automatically. The command reports deleted counts and exits with an error if either cleanup fails. Expiration checks still reject expired sessions and tokens before cleanup runs.

### Checks

From the repository root, run the existing checks with:

```sh
go test ./...
go vet ./...
go test -race ./...
```

The [CI workflow](.github/workflows/ci.yml) runs these checks on pushes and pull requests. See [AGENTS.md](AGENTS.md) for working agreements and regression-test guidance.

CI also tests error responses in headless Firefox. Run `HS_TEST_FIREFOX=firefox go test -count=1 ./cmd/web -run '^TestBrowserSiteErrors$'` locally with Firefox installed. The test downloads the HTMX version pinned in the site layout and checks its integrity hash. Ordinary Go test runs skip this browser test.

## Feedback

Feedback on the design, code, and developer experience is welcome. Email me at trentnix at gmail.com.

Send security reports privately to the same address. Do not include credentials, personal data, or exploit details in public issues.
