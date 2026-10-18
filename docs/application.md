# Application setup and modules

Start with the [stateless application](../examples/stateless/main.go). The [external application](../examples/external/readme.md) adds persistent notes, accounts, sessions, and mail using public packages. Application setup selects services and owns shared resources. Modules own their internal resources and routes.

## Adding or replacing a feature

Use the [notes module](../examples/external/notes/module.go) as a starting point. Its `Module(repository, protect)` constructor returns a descriptor whose factory creates a fresh module. The package registers an unconfigured descriptor during import. [Application setup](../examples/external/main.go) supplies storage and write-access middleware with `handlers.Replace` before initializing modules. Importing the module alone does not supply its dependencies.

The module's `Init` checks dependencies and prepares embedded templates. `Routes` binds ordinary Go handlers and returns registration errors. Handlers use the supplied repository, not a database client or configuration lookup. To replace notes storage, change [provider selection](../examples/external/storage.go), not the handlers. The [repository contract](../examples/external/notes/notes.go) belongs to this feature, not the framework.

| Replace | Implement and supply |
| --- | --- |
| Account storage | [`user.AccountRepository`](../pkg/services/user/account_repository.go), initialized in application setup and passed to auth and its middleware. Preserve email uniqueness, expiry checks, session revocation, and atomic token redemption. |
| Session storage | [`session.SessionStore`](../pkg/services/session/store.go) through a `session.StoreFactory`. Authentication also requires revocable rotation. See [ownership and extension](../pkg/services/session/readme.md#ownership-and-extension). |
| Session encoding | [`session.Serializer`](../pkg/services/session/serializer.go), selected when constructing a store. Define how existing data is read or invalidated. See [serialization](../pkg/services/session/readme.md#replacing-serialization). |
| Mail delivery | [`messaging.MailSender`](../pkg/services/messaging/mail.go) through `NewMailClientWithSender`. Honor cancellation, validate headers, and report unavailable delivery as an error. The client does not close the sender. |
| Authentication workflow | [`auth.AuthService`](../auth/auth_service.go) plus the supported workflow interfaces. Supply its descriptor to `auth.Module`. Changing account storage alone does not require a new auth provider. |

Selected services handle concurrent requests. Keep mutable request data separate and give each resource one cleanup owner. The contracts linked above define failure and concurrency guarantees. Matching method signatures is not enough. The external application's [storage tests](../examples/external/notes/repository_test.go) and [HTTP tests](../examples/external/verification_test.go) demonstrate provider-independent behavior and injected failures.

These extension points remain experimental. See the [compatibility policy](compatibility.md) before publishing an adapter.

## Application startup

### Signing keys

Generate independent values for `auth.jwtKey` and `http.session.jwtKey`, for example with `openssl rand -hex 32` separately for each key. Configuration uses the output's bytes unchanged, without decoding. Constructors reject keys shorter than 32 bytes and known placeholders. Length alone cannot establish randomness.

### Services and serving

`server.NewApplicationServer(cfg)` validates and copies configuration and creates the router and content manager. It does not load configuration files or create optional services. The core can run with `config.Config{}`.

Prepare shared services, select rendering and middleware, then initialize modules. Register owned resources with `app.OnShutdown` before their consumers. Supplying a service does not transfer ownership unless the receiving API explicitly says so.

Pass initialized dependencies through module factories. For example, this catalog selects email authentication and a site module with application-owned contact storage:

```go
catalog := []handlers.Descriptor{
    auth.Module(accounts, sessions, []auth.Descriptor{
        email.Provider(accounts, sessions, mailClient),
    }),
    site.Module(
        func(context.Context) (site.ContactRepository, error) { return contacts, nil },
        mailClient,
    ),
}
if err := site.ConfigureRendering(app.ContentManager, app.Config.Auth.Enabled); err != nil {
    return err
}
if err := handlers.Setup(ctx, app, nil, catalog, nil); err != nil {
    return err
}
```

This example deliberately selects only these modules and email authentication. [Reference module setup](../cmd/web/modules.go) instead replaces configured descriptors while preserving other discovered modules and auth providers. See the [storage](storage.md) and [session](../pkg/services/session/readme.md#ownership-and-extension) guides for provider construction.

`smtp.NewMailClient(cfg.Mail)` returns a mail client and a connectivity-check function using the same copied settings. Construction performs no network I/O. The application decides whether to use the check for readiness. It only opens and closes a TCP connection, bounded by the caller's deadline and mail timeout. It does not verify TLS, authentication, or delivery. Other senders use `messaging.NewMailClientWithSender`.

Consumers that require delivery must call the client's `ValidateConfig`. Internationalized domains are converted to IDNA ASCII form for delivery. Non-ASCII mailbox names require the SMTP server's `SMTPUTF8` and `8BITMIME` extensions. Successful sending means server acceptance, not inbox delivery.

For built-in email/password authentication, [`auth/web.Email`](../auth/web/email.go) connects accounts, sessions, mail, and flash notifications. Configure layouts first, then supply services once:

```go
accountWeb := authweb.Email{
    Accounts: accounts, Sessions: sessions, Mail: mailClient,
    LoginURL: "/auth/login/email", HomeURL: "/notes",
}
protect := accountWeb.RequireAuthentication(app.ContentManager)
catalog, err := handlers.Replace(handlers.Registered(), notes.Module(repository, protect))
if err != nil {
    return err
}
handler, err := accountWeb.Setup(ctx, app, catalog)
if err != nil {
    return err
}
// Serve handler through the application's outer security middleware.
```

Import `authweb "github.com/trentnix/hyperserver/auth/web"`. `Setup` configures the selected auth descriptor, initializes the entire catalog, and returns its handler with session and identity loading attached. Do not also call `handlers.Setup`. The catalog must include auth, as `handlers.Registered()` does here. Authentication and the email provider must be enabled in configuration.

The application still chooses protected routes, layout data, and security middleware and closes its services after requests drain. If setup fails, discard the application and close its services. Use the lower-level `auth.Module`, `handlers.Setup`, and `authmiddleware.LoadSessionManagement` APIs for other auth providers or capability selections. For sessions without accounts, install `session.WithSessionManager(sessions)`.

[`middleware.FromConfig`](../pkg/services/middleware/configuration.go) applies proxy verification, HSTS, and the optional shared rate limit in that request order. Keep origin checks and session loading inside it. Keep logging and security headers outside so they also cover rejected requests:

```go
handler = http.NewCrossOriginProtection().Handler(handler)
handler, err = middleware.FromConfig(handler, app.Config.HTTP)
if err != nil {
    return err
}
handler = middleware.SecurityHeaders(contentSecurityPolicy)(handler)
handler = middleware.LoggerMiddleware(log)(handler)
```

`authweb.Email.Setup` applies `http.defaultRateLimit`. When using `handlers.Setup` directly, pass routes created by `routing.NewConfiguredRoutes(app.Web, app.Config.HTTP, log)` to apply that setting. The shared budget is separate and requires `middleware.FromConfig`. See the [reference composition](../cmd/web/http.go).

Logging uses `*slog.Logger` and application-selected handlers. `LoggerMiddleware` attaches request fields to the logger returned by `logger.Get(r.Context())`. Use context-aware logging methods to pass cancellation and tracing values to the handler. Check for nil when no request logger is installed.

[`server.NewHTTPServer(app.Config.HTTP, handler)`](../pkg/server/http.go) creates a standard `*http.Server` with the configured address, timeouts, and optional TLS certificate. Resolve configuration paths first. It does not wrap the handler or open a listener. Timeout values pass through unchanged, including zero. Omitted read, write, and idle timeouts remain zero. The [configuration template](../config-template.yaml) supplies example values. Applications can still construct `http.Server` directly.

`app.Run(ctx, prepare)` coordinates startup, serving, and shutdown. The preparation function returns the HTTP server, a `net.Listener` opened on its `Addr`, and an error. It must not start serving. Run cleans up startup failures and drains requests before closing services. Applications managing HTTP themselves can use `server.ServeHTTP` and call `Shutdown` after requests drain.

## Module catalogs and authentication

Imports register descriptors, not live modules. Use `handlers.Registered()` for a catalog copy or supply a local catalog. Factories must return fresh instances without I/O. `handlers.Replace` replaces the entire descriptor, including its dependency declarations. To replace only its factory, copy the original descriptor and change `New`.

`handlers.Setup` resolves dependencies, initializes modules, binds routes, and registers cleanup. Pass configured `*routing.Routes` for route defaults, or nil for defaults. A nil catalog selects no modules. If setup fails, discard the application because its mux may still contain routes. Use `handlers.Initialize` only when managing route binding and cleanup yourself.

Auth has a separate descriptor catalog. Only configured, enabled providers initialize. An explicit empty catalog does not fall back to imports. Providers implement `AuthService` and the workflow interfaces they support, such as `LoginHandler` or `ResetHandler`. Unsupported routes return 404. Registration without a supporting provider is reported as unavailable.

Auth providers configure request handling, not resource lifecycles. Their `Init` can validate settings and prepare templates. It must not open connections, query storage, send mail, or start background work. Application setup supplies those services and owns cleanup. The auth module does not call provider close hooks. Provider `Init` has no context, so custom template filesystems must return promptly.

## Module dependencies

Give each dependency one source. Use typed constructor parameters for application-owned services. Use declared capabilities only when one module supplies a service to another.

Descriptors declare `Provides: []string{"post-storage"}` and `Requires: []handlers.Requirement{{Capability: "post-storage"}}`. Names are case-sensitive. Resolution rejects missing requirements, ambiguity, duplicate names, and cycles before invoking factories. Providers initialize before consumers. `handlers.Resolve` checks metadata without creating instances.

If several modules provide a capability, the selections argument to `Setup` or `Initialize` maps that capability to a module name:

```go
selections := map[string]string{"post-storage": "postgres-posts"}
```

`Optional: true` permits no provider. `Multiple: true` accepts all matching providers. Explicit selection narrows either form to one provider. Unselected alternatives still initialize if they remain in the catalog. Remove their descriptors to exclude them.

Providers return exactly their declared services from `Capabilities() map[string]any` after initialization. Consumers receive only declared dependencies through `BindDependencies`, before their own initialization:

```go
func (m *PostsModule) BindDependencies(d handlers.Dependencies) error {
    var err error
    m.repository, err = handlers.GetDependency[PostRepository](d, "post-storage")
    return err
}
```

Use `GetDependencies` for multiple providers. Missing optional services return a zero value or empty slice. Consumers must handle that absence. Wrong types, nil services, missing hooks, and provider failures stop startup. Capability and binding hooks must not perform I/O. See the [executable example](../pkg/handlers/example_test.go).

## Module cleanup

Resource-owning modules can implement `Shutdown(context.Context) error`. `Initialize` rolls back completed modules on failure and returns no instances. A failing `Init` must release its own partial resources. Modules must not close application-owned services.

`Setup` registers reverse-order cleanup automatically. If using `Initialize` directly, register `handlers.Close` yourself. Repeated cleanup does not rerun module hooks.

| Budget | Control | Scope |
| --- | --- | --- |
| Module initialization | Caller context. Reference command: `app.initializationTimeout`, default `10s`. | All selected modules together. An earlier parent deadline wins. |
| HTTP draining | `http.shutdownTimeout`, default `10s`. | Requests already in progress, not module cleanup. |
| Resource cleanup | `Shutdown()` allows `10s`. `ShutdownContext(ctx)` accepts another deadline. | Reverse-order shutdown hooks. |

Without a caller deadline, `Initialize` uses ten seconds. The reference command's database, account, and session setup have separate ten-second limits. Context deadlines cannot interrupt code that ignores cancellation. Initialization contexts must not be retained for request I/O.

If HTTP draining fails, Run closes HTTP connections and returns `server.ErrHTTPDrainIncomplete` without closing services under active handlers. The command must exit or wait for those handlers before cleanup. The reference command exits with an error, and a second signal forces exit. Applications must manage hijacked connections separately.
