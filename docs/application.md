# Application setup and modules

Start with the [standalone quickstart](quickstart.md). Use `application.New` to assemble services, modules, middleware, and cleanup. Applications choose their own rendering and business rules. The [blog startup](../cmd/web/application.go) and [HTTP fixtures](../internal/test/web) show larger applications.

## Services and serving

`application.New` copies and validates `config.Config`, resolves paths, opens selected services, initializes modules, and binds routes. It does not load the application configuration file or open a listener. Call `config.GetConfig()` first when using file configuration.

`Options.AccountProviders` and `Options.SessionProviders` are complete storage catalogs. Nil catalogs include no providers. Configuration must select available names, including an explicit `auth.accountStorage.provider` when opening accounts. For the built-in SQLite account store and SQLite/cookie session stores, import `pkg/services/configured`:

```go
app, err := application.New(ctx, cfg, application.Options{
    AccountProviders: configured.AccountProviders(cfg),
    SessionProviders: configured.SessionProviders(cfg),
    Modules: func(ctx context.Context, app *application.Application) ([]handlers.Descriptor, error) {
        return handlers.Registered(), nil
    },
})
if err != nil {
    return err
}
```

Catalog construction performs no I/O. Modify the returned maps to replace factories, or supply only your own providers. Stateless applications can omit both catalogs and build without CGO. Built-in SQLite providers require CGO and a C compiler.

`Options.Services` supplies initialized accounts, sessions, or mail and bypasses their factories. Supplied services keep their existing owner. The application closes services it creates, including after startup failure. See [storage providers](storage.md) and [sessions](../pkg/services/session/readme.md) for contracts and replacement tests.

`Options.Modules` runs after shared services are ready. Configure layouts and response handlers there, before modules prepare templates. Return the selected descriptors. Imports only register available factories. A nil callback selects no modules. Do not repeat `handlers.Setup`.

Serve `app.Handler` through `app.Run`, as shown in the quickstart. `Handler` includes standard web middleware, session loading, and identity loading when authentication is active. Do not serve the bare `app.Web` or install the standard stack twice. Modules must still protect their own routes.

Set `Options.ContentSecurityPolicy` for the application's assets. Empty omits CSP. Attach a `*slog.Logger` with `logger.Set(ctx, log)` before construction to enable startup and request logging. Requests receive IDs even without a logger. See [HTTP behavior](http.md) for middleware, proxies, body limits, and rate limits.

`app.renderNotifications` enables session-backed page notifications independently of authentication. A custom `ContentManager.LoadNotifications` supplied in the callback takes precedence and owns its dependency requirements. Authentication forms retain their own flash messages. See [messages](../pkg/components/readme.md#messages).

## Authentication providers

Select `auth.Module(auth.Dependencies{}, nil)` in `Options.Modules` and set `auth.enabled: true`. The constructor supplies shared services, `Options.AuthPolicy`, and `Options.AuthProviders` instead of the selected descriptor's factory arguments. With authentication disabled, it removes the auth module before resolving dependencies. Sessions remain independently available.

The default auth catalog is `auth.Registered()`, including email/password and Google. Configuration enables providers through `auth.services.<provider>.enabled`. Alternatively, `Options.AuthProviders` returns an explicit catalog after module selection, under the startup deadline. An empty result selects none. Register callback-owned resources with `app.OnShutdown`. See [Google setup](google-authentication.md) and [registration and email](configuration.md#registration-and-email).

`Options.AuthPolicy` controls admission, registration availability, and successful-login behavior. Providers must call `Policy.Admit` before creating accounts or linking identities and `Policy.RegistrationAvailable` before registration. External callbacks must recheck both rules. The manager checks the local account before creating a session and calls `AfterLogin` after saving it. Business permissions remain application rules.

Site modules own `/login` and `/register`. Declare a dependency on `auth.ManagerCapability` and receive the manager through `handlers.GetDependency[*auth.AuthManager]`. During route registration:

```go
pages, err := m.auth.PageHandlers(routes)
if err != nil {
    return err
}
routes.Handle("GET /login", pages.Login)
pages.Submissions.Handle("POST /login", pages.LoginSubmit)
routes.Handle("GET /register", pages.Registration)
pages.Submissions.Handle("POST /register", pages.RegistrationSubmit)
return routes.Err()
```

POST handlers must use `Submissions` for auth limits and diagnostics. These handlers enforce anonymous-user checks and no-store responses. Disabled registration returns an unavailable response. A module that supports running without auth can declare an optional dependency and skip these routes. The blog requires auth. Custom login and registration paths are unsupported.

Protect other routes with `auth/middleware.RequireAuthentication(app.Accounts, app.ContentManager)`. The default login destination is `/login`. Presentation overrides belong on `app.ContentManager` in the module callback. Login and home destinations must be local absolute paths.

### Custom auth providers

Implement [`auth.AuthService`](../auth/auth_service.go) and register an `auth.Descriptor`, or return the descriptor from `Options.AuthProviders`. Factories create fresh instances without I/O. Provider `Init` validates settings and prepares templates. It must not open connections, query storage, send mail, or start background work. Setup supplies services and owns cleanup. Auth calls no provider close hooks.

Inline login providers implement `LoginFormHandler`. Their forms post to `/login` with a hidden `provider` field. Redirecting providers implement `RedirectLoginHandler`, whose `BeginLogin` receives that POST.

Registration uses `RegistrationHandler` and optional `RegistrationFormHandler`, with forms posting to `/register`. Return form markup for validation failures. After creating an account or writing a redirect, return an empty result. See the [email provider](../auth/modules/email/email_auth.go) and [provider contracts](../auth/auth_service.go).

`auth.PrepareProviderView` and `auth.ProviderPage` supply shared provider controls. `Policy.LoginAccount` and `LoginAvailability` can tailor visible methods to a known account. Availability affects presentation only. A failed availability check must not disable authentication through that provider.

After validating an external protocol and consuming browser-bound, single-use state, call `auth.ExternalAccounts.Resolve` with the identity and original flow intent. It applies admission, registration, linking, and account-version rules. Matching email never links accounts. Supply `VerifiedEmail` for registration only when current ownership is established.

`AccountProvider` supplies controls at `/auth/account`. `ExternalIdentityProvider` supports disconnection, which requires a working alternative password method, password confirmation, and `user.ExternalIdentityUnlinker`. Optional password enrollment uses `user.PasswordSetupRepository` and a separate token purpose. See [Google account management](google-authentication.md#existing-accounts) for the user flow. Each handler must enforce authorization independently of its links.

## Module dependencies

Each module implements `handlers.Handler`. Its factory creates fresh state without I/O. `Init` validates settings, acquires module-owned resources, and prepares templates. `Routes` registers method-qualified handlers and returns `routes.Err()`. Use `handlers.Replace` to replace a descriptor in a catalog.

Pass application-owned services through typed constructors. Use declared capabilities for services supplied by another module. Each dependency must have one source and one cleanup owner:

| Source | Wiring | Cleanup |
| --- | --- | --- |
| `Options.Services` | Pass the supplied service into module factories. | Its original owner. |
| Configuration-selected services | Read shared services in `Options.Modules`. | The application, after modules stop. |
| Module capability | Declare `Requires` and receive it in `BindDependencies`. | The providing module. Consumers borrow it. |
| Internal module resource | Open it in `Init`. | The module, including partial acquisitions on failure. |

Descriptors declare `Provides: []string{"post-storage"}` and `Requires: []handlers.Requirement{{Capability: "post-storage"}}`. Resolution rejects missing requirements, ambiguity, duplicate names, and cycles before creating instances. Providers initialize first. `handlers.Resolve` validates metadata without invoking factories.

Use `Options.ProviderSelections`, such as `map[string]string{"post-storage": "postgres-posts"}`, to resolve ambiguous capabilities. `Optional: true` permits no provider. `Multiple: true` accepts all matches. Explicit selection narrows either form to one provider. Remove unused descriptors from the catalog to prevent their initialization.

After initialization, `Capabilities() map[string]any` must return exactly the declared services. Consumers bind them before their own `Init`:

```go
func (m *PostsModule) BindDependencies(d handlers.Dependencies) error {
    var err error
    m.repository, err = handlers.GetDependency[PostRepository](d, "post-storage")
    return err
}
```

Use `GetDependencies` for multiple providers. Consumers must handle absent optional dependencies. Wrong types, nil services, missing hooks, and provider failures stop startup. Capability and binding hooks must not perform I/O. See the [dependency example](../pkg/handlers/example_test.go).

Modules can contribute components instead of distinct pages. A composing module collects loaded providers' templates and chooses components, data, order, and layouts for each request. See [module-driven composition](../pkg/components/readme.md#compose-pages-from-loaded-modules).

## Optional module configuration

Implement `handlers.Configurable` to load module settings before dependency binding and `Init`:

```go
type Settings struct {
    PageSize int `yaml:"pageSize"`
}

func (m *Module) Configuration() any { return &m.settings }
```

Initialize fresh settings with defaults in the factory. For a module named `news`, the framework reads `modules/news/config.yaml` under `app.workingDirectory`. Installed package paths do not affect lookup. Names must be single local directory names, not `.` or `..`.

Missing or empty files preserve defaults. Invalid or unreadable files stop startup. `Init` applies explicit constructor overrides and validates the result. Modules without `Configurable` load no file. Loading occurs once, without directory search or module-specific environment overrides.

Applications own runtime files and keep them out of source control. For direct setup, `config.LoadModuleFile` provides the same strict decoding.

## Shutdown and health

Resource-owning modules implement `Shutdown(context.Context) error`. A failing `Init` must release its own partial acquisitions. Setup closes initialized modules in reverse dependency order on failure or shutdown. Modules must not close borrowed services. Repeated cleanup does not rerun hooks.

`app.Run` drains HTTP requests, then closes modules, callback-owned resources, and application-owned services. Without Run, stop consumers before calling `Shutdown`. If draining fails, Run closes HTTP connections and returns `server.ErrHTTPDrainIncomplete` without closing services under active handlers. The command must exit or wait for those handlers before cleanup. Applications must manage hijacked connections separately.

| Budget | Control |
| --- | --- |
| Shared startup deadline | `app.initializationTimeout`, default `10s`. An earlier parent deadline wins. |
| HTTP draining | `http.shutdownTimeout`, default `10s`. |
| Resource cleanup | `Shutdown()` allows `10s`. `ShutdownContext(ctx)` accepts a deadline. |

Startup contexts must not be retained for request I/O. Cancellation cannot interrupt code that ignores its context. Direct `handlers.Initialize` uses the caller's deadline or a ten-second default.

`app.HealthHandler(runContext, timeout, checks...)` exposes `GET /health/live` and `GET /health/ready` outside sessions and rate limits. Use the application's run context, not its startup deadline. Readiness probes accounts and session stores implementing `CheckHealth(context.Context) error`, then custom checks. Probes share the timeout and must be read-only, concurrent-safe, and cancelable. Providers without this capability are not probed.

SMTP construction performs no I/O. Consumers requiring mail must call `ValidateConfig`. Add `health.Check{Name: "mail", Run: app.Mail.CheckHealth}` if SMTP connectivity must affect readiness. This probe checks a TCP connection, not TLS, authentication, or delivery.

Internationalized domains use IDNA. Non-ASCII mailboxes require SMTPUTF8 and 8BITMIME. Successful sending means server acceptance, not inbox delivery.

## Explicit setup

Use lower-level APIs only when the application needs a different lifecycle or HTTP stack:

1. Call `server.NewApplicationServer(cfg)` for a validated core router and content manager. It loads no files or optional services. Resolve paths before opening resources.
2. Open services and register their cleanup with `app.OnShutdown` before initializing consumers. Supply services through module factories or `auth.Dependencies`.
3. Configure rendering, then call `handlers.Setup(ctx, app, nil, catalog, selections)`. Direct `Initialize` callers must register `handlers.Close` themselves.
4. Wrap the mux with `authmiddleware.LoadSessionManagement(accounts, sessions)` or `session.WithSessionManager(sessions)`, then `middleware.Web`. Install `flash.LoadNotifications` if needed. Explicit composition owns middleware order and cross-origin policy.
5. Use `server.NewHTTPServer` and `app.Run`, or manage a standard `http.Server` and call `Shutdown` after requests drain. The preparation callback for Run must return an HTTP server and listener without starting serving.

`server.NewHTTPServer` applies configured addresses, timeouts, and optional TLS. It does not open a listener or add middleware. Omitted read, write, and idle timeouts remain zero. A write timeout includes handler work but does not cancel it. Allow enough time for synchronous mail, storage, and rendering. See [HTTP behavior](http.md).

## Blog setup

Create one [`blog.Site`](../modules/blog/site.go) per startup. `Configure(app)` sets rendering and returns the blog descriptor. Supply its `AuthenticationPolicy()` through `Options.AuthPolicy`, select the blog and auth modules, and provide the built-in storage catalogs. See the [blog application](../cmd/web/application.go).
