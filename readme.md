# HyperServer

HyperServer is an experimental Go framework for server-rendered web applications with first-class HTMX support. It aims to handle common web infrastructure so developers can focus on features using Go handlers and HTML templates.

The core focuses on rendering and HTMX. Authentication, sessions, storage, serialization, and mail must be replaceable. HyperServer favors the standard library and fewer dependencies, while keeping established security implementations. Applications can use ordinary forms or depend on HTMX.

## Project status

HyperServer is in early development and is not production-ready. Public APIs and configuration can change without backward compatibility during `v0.x`.

Known security and correctness issues affect authentication, recovery, sessions, request handling, and application lifecycle. Do not expose the reference application to the public internet or use it with real accounts or sensitive data.

The reference application is loopback-only, but a tunnel or reverse proxy can still expose it. This restriction applies to `cmd/web`, not the framework.

High performance is a design goal, not an established benchmark result. Passing tests do not establish production readiness.

![Hyper Gopher](hypergo.png)

## What is here today

- A reference application with server-rendered pages, HTMX fragments, forms, and notifications.
- Import-based module registration with application-owned instances, routes, and initialization hooks.
- Experimental email/password authentication, account verification and recovery, cookie and SQLite session stores, SMTP mail, and request logging.
- SQLite-backed persistence used by the reference application and framework services.

Authentication requires a session provider that supports atomic rotation and server-side revocation. The supplied SQLite provider meets those requirements. Login and account verification rotate the session ID. Password reset or change signs out all devices. Cookie-only sessions are available for non-authentication data.

Applications define access rules through [route-level authorization policies](auth/middleware/authorization_example_test.go). Denial returns 403. Policy failures return 500 without exposing the error.

Session data uses JSON, limited to 64 KiB before base64 encoding. Cookies are limited to 4 KiB including their name and attributes. Cookie-store data is signed, not encrypted, and must not contain secrets. See the [session guide](pkg/services/session/readme.md) for supported values and typed reads.

Applications supply optional services through typed constructors. Modules can declare dependencies on other modules for ordered initialization. Application setup owns shared services and their cleanup.

The [roadmap](roadmap.md) describes priorities and intended architecture. [Architecture decisions](docs/decisions/readme.md) explain specific choices.

## Exploring the code

Use the Go version declared in [go.mod](go.mod), which also controls CI. SQLite tests and the race detector require CGO and a C compiler.

Start with [cmd/web](cmd/web) for application composition, [modules/site](modules/site) for the reference application, and [pkg](pkg) for framework components and services.

For a smaller starting point, run `go run ./examples/stateless` and open http://127.0.0.1:8082. The [stateless greeting application](examples/stateless/readme.md) uses its own module and embedded templates without a configuration file, database, sessions, authentication, or mail.

The development [site module owns the diagnostic and sample routes](modules/site/router.go). Production applications must not import it. Omitting it does not resolve the framework's outstanding security issues.

`POST /test-email` allows logged-in users who have completed any required account verification. Other users receive 403, and no mail is sent.

### Configuration

Use [config-template.yaml](config-template.yaml) as the starting point for a local `config.yaml`, not a production configuration.

The [loader](config/load.go) uses the first `config.yaml` in `.`, `config`, `../config`, or `../../config`. File settings override defaults. `HYPERSERVER_` environment values override the file, including empty values. For example, `HYPERSERVER_MAIL_PASSWORD` overrides `mail.password`. Unknown settings and invalid values stop startup. Durations require units such as `5s` or `2h`, except `0`. Use a YAML list or comma-separated environment value for `http.trustedProxies`.

Configuration loads once. Changing files or environment values does not reconfigure a running application. Supplying `config.Config` directly bypasses the loader and its defaults. Constructors still apply their documented zero-value defaults, such as SMTP's `30s` timeout.

Provider selection uses the same resolved configuration:

| Service | Default and replacement |
| --- | --- |
| Accounts | Empty `auth.accountStorage.provider` selects SQLite. Select a registered provider or supply an initialized repository. See [account storage](#account-storage). |
| Sessions | `http.session.types` maps session names to stores, with `default` as the fallback. There is no implicit store when that mapping is absent. Unknown or disabled selections fail. [Custom stores](pkg/services/session/readme.md#ownership-and-extension) currently require code wiring. |
| Authentication | Only configured, enabled providers in the selected auth catalog are instantiated when `auth.enabled` is true. Importing a provider alone does not enable it. |
| Mail | The reference application creates an SMTP sender from `mail` settings unless its `Mail` field already holds a client. Use `messaging.NewMailClientWithSender` for another sender. Incomplete SMTP configuration leaves delivery unavailable, not replaced by another sender. |

To exercise two storage platforms together, run `go run ./cmd/web -contact-directory ./tmp/contacts`. Accounts stay in SQLite, while the site saves contacts as individual JSON files in a private directory. See the [contact-file format and compatibility policy](modules/site/storage/readme.md). The file provider requires hard-link support. Without this flag, contacts also use SQLite. This is reference-application wiring, not a framework-wide storage setting.

See the [storage guide](docs/storage.md) for schema checks, provider guarantees, and maintenance.

### Application startup

`server.NewApplicationServer(cfg)` validates and copies configuration and creates the router and content manager. It does not load files or initialize optional services. The [stateless application](examples/stateless/main.go) demonstrates a complete setup without those services.

Application setup prepares shared services, passes them to modules, selects rendering and middleware, and registers cleanup. Modules must not close services owned by the application. The [setup guide](docs/application.md) covers service wiring, dependency resolution, and lifecycle APIs.

`app.Run(ctx, prepare)` coordinates preparation, serving, and shutdown. The preparation function returns a configured `*http.Server` and `net.Listener` without starting them. On shutdown, Run drains requests before closing services. If draining fails, it returns `server.ErrHTTPDrainIncomplete` and leaves services open for active handlers. The command must exit or wait for those handlers before cleanup.

### Module registration

Go package imports register factories, not live modules:

```go
func init() {
    handlers.Register(handlers.Descriptor{
        Name: "reports",
        New: func() handlers.Handler { return new(ReportsModule) },
    })
}
```

`handlers.Registered()` returns a catalog copy. Applications can use a local `[]handlers.Descriptor` instead. Factories must return fresh instances without I/O. Imports make modules available, but application setup selects and initializes them.

Configure module dependencies before calling `handlers.Setup`. For example, supply the site's contact storage and configure email authentication with the same mail client:

```go
catalog, err := handlers.Replace(handlers.Registered(), site.Module(
    func(context.Context) (site.ContactRepository, error) { return contactStore, nil },
    mailClient, // nil leaves the site's test-email delivery unavailable
))
if err != nil {
    return err
}

providers := []auth.Descriptor{email.Provider(accounts, sessions, mailClient)}
catalog, err = handlers.Replace(catalog, auth.Module(accounts, sessions, providers))
if err != nil {
    return err
}

if err := site.ConfigureRendering(app.ContentManager, app.Config.Auth.Enabled); err != nil {
    return err
}

if err := handlers.Setup(ctx, app, nil, catalog, nil); err != nil {
    return err
}
```

`handlers.Setup` resolves dependencies, initializes modules, binds routes, and registers reverse-order cleanup. Pass a configured `*routing.Routes` instead of nil for route defaults. A nil catalog selects no modules. If setup fails, discard the application: initialized modules are closed, but registered routes remain in its mux.

The example uses initialized `accounts`, `sessions`, and `contactStore` services owned by application setup. The explicit auth provider list selects only email authentication. See [reference module setup](cmd/web/site_modules.go) to preserve other discovered providers.

### Module dependencies

Give each dependency one source: a typed constructor parameter for application-owned services, or a declared capability supplied by another module. Do not use both for the same service. See [dependency declarations and binding](docs/application.md#module-dependencies) for required, optional, and multiple providers.

### Module cleanup

A module that owns resources can implement `Shutdown(context.Context) error`. A failing `Init` must release its partial resources. Cleanup runs consumers before providers, after requests drain.

The reference application defaults to `10s` for `app.initializationTimeout` and `http.shutdownTimeout`. The first covers all module initialization. The second covers HTTP draining, not module cleanup. See [lifecycle deadlines and failure handling](docs/application.md#module-cleanup).

### Files and templates

The site and authentication modules embed their templates and static assets. A built binary does not need the repository beside it. Rebuild to apply changes to bundled assets.

Application setup selects shared rendering defaults before module initialization. The reference application's [rendering setup](cmd/web/rendering.go) calls `site.ConfigureRendering` for layouts, navigation, response handlers, and redirect URLs. It selects `flash.GetNotifications` only when `app.renderNotifications` is enabled. The stateless application calls `pages.ConfigureRendering` for its layouts. Module initialization does not install either policy.

Apply overrides after selecting defaults and before serving. For example:

```go
if err := site.ConfigureRendering(app.ContentManager, app.Config.Auth.Enabled); err != nil {
    return err
}
app.ContentManager.AuthURL = "/auth/login"
app.ContentManager.LoadNotifications = nil // no session-backed notifications
app.ContentManager.RegisterLayouts(contentservice.PageType, layoutFiles, "layouts/page.html")
```

`ContentManagerService.ApplyDefaults(owner, configure)` selects one shared rendering policy. A second call returns an error, even for the same policy. Applications can still override fields and templates afterward. Modules must leave shared settings to application setup.

Rendering accepts explicit filesystems. A layout and its page content can come from different modules. Supply an `embed.FS`, `os.DirFS` rooted at an absolute directory, or another `fs.FS`:

```go
page.AddLayout(layoutFiles, "layouts/page.html")
page.AddContents(templateFiles, "pages/home.html", "partials/banner.html")
```

Paths use forward slashes and are relative to the supplied filesystem. Missing files and invalid paths return rendering errors. The email provider accepts a `TemplateFS` override before initialization, with the same paths as its bundled `templates` directory.

The reference commands resolve local storage and TLS paths relative to the selected `config.yaml`. `app.workingDirectory` overrides that base without changing the process directory. Relative overrides start at the config file's directory. `-contact-directory` uses the same base. SQLite memory connections and other drivers' connection strings are unchanged. Applications that construct configuration in Go can call `ResolvePaths()` before opening services to capture their resource base.

### Route registration errors

`routing.Routes` uses Go's `http.ServeMux` to check patterns. Duplicate or conflicting patterns, malformed patterns, and nil handlers set `Routes.Err()` instead of panicking. Module route methods must return that error:

```go
func (m *ReportsModule) Routes(routes *routing.Routes) error {
    routes.HandleFunc("GET /reports", m.List)
    routes.HandleFunc("POST /reports", m.Create)
    return routes.Err()
}
```

Child scopes share the first error and ignore later registrations. Earlier routes remain registered, so a failed mux must not serve traffic. Checks run after module initialization, during route binding. They do not roll back module resources.

Direct `http.ServeMux.Handle` calls retain Go's panic behavior. Panics in module code, diagnostic callbacks, or request handlers are not converted into registration errors.

### Account storage

The reference application's `auth.accountStorage.provider` selects account storage, separate from login providers under `auth.services`. Empty or omitted selects SQLite using `database.connection`. Unknown providers stop startup when authentication is enabled.

Applications supply an initialized [`user.AccountRepository`](pkg/services/user/account_repository.go) to auth factories and middleware. For SQLite:

```go
accounts := sqlite.NewAccountRepository(pool)
if err := accounts.Initialize(ctx); err != nil {
    return err
}
```

Import [`user/sqlite`](pkg/services/user/sqlite) for that constructor. The repository uses the supplied pool without owning it. Application setup must close owned resources after consumers stop.

Replacement providers must preserve uniqueness, concurrency, and atomic security operations—not just implement the interface. Email matching is case-insensitive, with equivalent Unicode and Punycode domains sharing a key. The original address is preserved for display and delivery. See [storage guarantees](docs/storage.md#storage-guarantees) for the exact comparison rules.

Fresh SQLite stores initialize automatically. Existing stores must have a supported, clean schema version. Startup does not repair or migrate them. Providers have independent migration histories even when sharing a database.

See the [storage guide](docs/storage.md) for migrations, SQLite settings, native backend access, and [provider selection](docs/storage.md#provider-selection). Session storage and serializer replacement are covered in the [session guide](pkg/services/session/readme.md).

### Logging

The reference application uses [SlogLogger](pkg/services/logger/sloglogger.go) with JSON output on stderr at Info level. Records use slog's `time`, `level`, and `msg` fields, with uppercase levels. Request records include `requestID`, `method`, and the path without query values. The default handler does not sample messages or add caller locations or stack traces.

Pass a `slog.Handler` to `logger.NewSlogLogger` to choose a writer, text output, levels, source locations, or `ReplaceAttr` redaction. Nil selects the default JSON handler. Application code must not log secrets. The adapter preserves handler redaction and `slog.LogValuer` behavior but does not automatically recognize sensitive fields.

### Response dispatch

`Content` describes a response. `Content.Render(w, r)` selects the page or HTMX templates, loads optional notifications, and renders before writing the status, headers, and body. Existing form-error handling uses the same path. Validation forms use HTTP 200 by default so HTMX displays their errors. Setting `ResponseStatusCode` to 422 requires the application to configure HTMX to swap that response. See [HTMX response handling](https://htmx.org/docs/#response-handling).

Use `pkg/components/htmx` for optional response metadata:

```go
page := content.NewManagedContent(r, contentManager)
page.AddContent(templateFiles, "templates/editor.html")
page.PartialName = "editor"
page.Data = form
page.HTMX.Response = &htmx.Response{
    Trigger:  "saved",
    Retarget: "#editor",
    Reswap:   "outerHTML",
}
if err := page.Render(w, r); err != nil {
    // Handle the rendering or delivery error.
}
```

`TriggerAfterSwap`, `TriggerAfterSettle`, `PushURL`, `ReplaceURL`, and `Refresh` map to their HTMX response headers. These fields are server-selected instructions. Do not copy untrusted request values into them. Ordinary requests ignore `HTMX.Response`.

The reference site embeds and serves [HTMX 2.0.11](modules/site/templates/html/js/vendor/htmx/README.md) locally with an integrity hash and disables local history snapshots. The vendored asset includes its license and update instructions. Back/forward navigation fetches the page again. Requests with `HX-History-Restore-Request: true` render a full page, omit fragment metadata and out-of-band updates, and use ordinary 303 redirects. HTMX follows those redirects while retaining the requested history URL. [Browser tests](cmd/web/history_browser_test.go) cover push, replace, back, forward, and redirects during restoration.

For out-of-band updates, add named templates to the loaded template files and list their names in `page.OutOfBand`. Each template supplies its own `hx-swap-oob` markup:

```html
{{define "notice"}}<div id="notice" hx-swap-oob="true">{{range .Notifications}}{{.Message}}{{end}}</div>{{end}}
```

`page.OutOfBand = []string{"notice"}` appends that template only for HTMX requests. The main view and extra templates share the same data and notifications. All templates must render successfully before any body is sent.

Set `page.RedirectURL = "/saved"` for an ordinary 303 redirect or an HTMX 200 with `HX-Redirect`. Redirects take precedence over other response fields and skip templates and notification retrieval. Store any notification for the destination explicitly. `util.RedirectToURL` uses the same redirect behavior without constructing content.

For an HTMX response that must not swap content, set `page.HTMX.Response = &htmx.Response{NoContent: true, Trigger: "saved"}`. It sends 204 with the event header and skips templates and notification retrieval. Ordinary requests still render their page. Setting `ResponseStatusCode` to 204 skips the body for both request types. Use `RedirectURL` for redirects, not a 3xx `ResponseStatusCode`.

Notification loading can change cookies before rendering. A template failure leaves response content unwritten, but does not undo notification retrieval or application changes. A delivery error can occur after part of the body was sent. Direct `http.Handler` and `http.ResponseWriter` use remains available for streaming, downloads, and other specialized responses.

The [contact form](modules/site/templates/html/partials/contact-form.html) demonstrates ordinary `method="post" action="/contact"` submission alongside HTMX. Validation and storage failures redisplay escaped input. Successful writes clear the form and show an acknowledgment without a redirect. The [workflow tests](cmd/web/form_workflow_test.go) cover both modes, storage failure and retry, and registration/login redirects. Native browser submission is also tested without HTMX.

### Notifications

Message values in `pkg/components/messages` need no session service. Applications can supply `Content.Notifications` directly or set `ContentManagerService.LoadNotifications` before serving requests. A nil loader skips retrieval. Loader errors stop rendering.

For session-backed notifications, use [flash.GetNotifications](pkg/services/flash/notifications.go) as the loader. The reference site selects that loader when `app.renderNotifications` is enabled, unless the application already supplied one. See the [message examples](pkg/components/readme.md#messages) for storage helpers and direct message values.

### Rendered-response caching

`Content.Render` adds `HX-Request` and `HX-History-Restore-Request` to `Vary` without removing existing fields. An existing `Vary: *` already covers every request header. Rendered responses default to `Cache-Control: no-store`, including the reference site's personalized navigation and notifications. Static-file handlers are unchanged.

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

The listener supports TLS 1.2 or newer. Invalid or unreadable files stop startup. Relative paths use the config file's directory or the `app.workingDirectory` override. Restart to reload certificates. Certificate issuance, renewal, and HTTP-to-HTTPS redirects are not automatic.

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

The reference application sets `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, and disables camera, microphone, and geolocation access through `Permissions-Policy`. Its [Content Security Policy](cmd/web/security.go) allows local scripts and styles, plus Google Fonts. It blocks inline scripts, `eval`, framing, and cross-origin form submissions. Headers also cover fragments and error responses.

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

Forms embed `form.Form` and implement `Bind(*http.Request) error` and `Validate() form.FieldErrors`. Validation uses ordinary Go checks, not struct tags. `FieldErrors` maps field names to plain-text messages. `ParseAndValidate` records those errors, and callers must check `HasErrors()` before changing data or sending mail. See [contact validation](modules/site/contact.go) and [account validation](auth/modules/email/form_validation.go).

Account forms use the [password package's rules](auth/password/password.go). Required fields reject empty strings without trimming input. `form.ValidEmail` uses `net/mail` for address syntax and `golang.org/x/net/idna` for internationalized labels and encoded domain-length limits. Ordinary ASCII labels allow internal double hyphens, while internationalized labels retain IDNA's stricter rules. Quoted local parts, single-label domains, and IP literals are supported without changing the submitted address. Display names, comments outside quotes, surrounding whitespace, control characters, and malformed domains are rejected.

Account email fields use `type="text"` with `inputmode="email"` so browser email validation does not reject internationalized addresses before submission. The server validates email syntax. Stored comparison keys must match the current `EmailKey` rules. Existing keys for internationalized domains require explicit recomputation and a collision check before reuse. Startup does not rewrite them.

Email syntax checks do not establish deliverability.

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

These capacities apply per route, not across a module. Both default to 4,096 and must be positive. Override them with `HYPERSERVER_AUTH_RATELIMIT_MAXCLIENTS` and `HYPERSERVER_APP_SITERATELIMIT_MAXCLIENTS`. HTTP rate-limit capacities do not change these settings.

Startup warns when a route's allowance exceeds an enabled `http.sharedRateLimit` with the same window duration. The warning identifies the route and both per-IP allowances. Startup continues with both limits enforced. Different windows and explicit opt-outs do not trigger this warning.

#### Client capacity is a temporary admission cap

With `maxClients: 4096`, if all entries are unexpired, a new IP receives 429 with `Retry-After`—even on its first request. The limiter does not replace active entries or reset their counters.

Existing clients retain their remaining allowances. New IPs can enter as entries expire. A route limiter restricts only its route, while a shared application limiter can reject new IPs across the application. This is not a permanent user limit, but an undersized capacity can block legitimate newcomers.

Size capacity for distinct IPs within the window, including anonymous clients, not registered accounts. Evicting active entries would let clients bypass throttling. Expired entries are removed on later requests, not by a background worker.

#### Responses and limits

Each window starts with the client's first admitted request to that limiter. Throttled requests receive a plain-text 429 with `Retry-After`, including HTMX requests. With request logging configured, rejections identify the registered route or named shared budget, its settings, and whether its allowance or client capacity was exhausted. Submitted account values do not affect recovery throttling or appear in these rejection logs.

Clients behind a NAT or an unconfigured proxy share an allowance. Memory capacity applies per limiter, not across all routes. State resets on restart and is not shared across servers. Fixed windows allow bursts near window boundaries. These controls do not prevent distributed abuse or cap concurrent work.

### Expired-record cleanup

Run `go run ./cmd/cleanup -batch-size 100 -timeout 10s` with the same configuration as `cmd/web`. Each run removes at most 100 expired records from each selected server-side session store and, when authentication is enabled, account-token storage. It does not remove live records or accounts.

Schedule cleanup externally. The server does not run it automatically, and expiration checks reject expired sessions and tokens without waiting for cleanup. See [maintenance requirements and custom providers](docs/storage.md#expired-record-cleanup).

### Checks

```sh
go test ./...
go vet ./...
go test -race ./...
```

[CI](.github/workflows/ci.yml) runs Go and Firefox checks in independent jobs. Both must pass. Browser tests are opt-in locally.

See the [testing guide](docs/testing.md) for browser commands, startup diagnostics, and artifacts. Migration-test instructions are in the [storage guide](docs/storage.md#account-schema-migrations).

## Feedback

Feedback on the design, code, and developer experience is welcome. Email me at trentnix at gmail.com.

Send security reports privately to the same address. Do not include credentials, personal data, or exploit details in public issues.
