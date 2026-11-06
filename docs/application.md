# Application setup and modules

Use `application.New` to configure services, modules, middleware, and cleanup. The [blog application](../cmd/web/application.go) demonstrates the setup. [HTTP fixtures](../internal/test/web) test provider replacement and ordinary forms.

Create one [`blog.Site`](../modules/blog/site.go) per application startup. `Configure(app)` sets shared rendering and returns the blog descriptor. `AuthenticationPolicy()` supplies account admission and onboarding behavior through `Options.AuthPolicy`. Select the blog and `auth.Module` in `Options.Modules`. Provider settings and credentials belong in `config.yaml`.

## Adding or replacing a feature

A module descriptor's factory creates a fresh instance with its dependencies. Select descriptors in `application.Options.Modules`. Use `handlers.Replace` when replacing a descriptor in an existing catalog. Importing a module alone does not supply its dependencies or select it.

The module's `Init` validates settings and dependencies, opens module-owned resources, and prepares embedded templates. `Routes` binds ordinary Go handlers and returns registration errors. Keep storage selection out of request handlers. Use feature-owned repositories where they help, or native clients when direct backend access is appropriate.

| Replace | Implement and supply |
| --- | --- |
| Account storage | [`user.AccountRepository`](../pkg/services/user/account_repository.go), selected through `Options.AccountProviders` or supplied initialized through `Options.Services.Accounts`. Preserve email uniqueness, expiry checks, session revocation, and atomic token redemption. |
| Session storage | [`session.SessionStore`](../pkg/services/session/store.go) through a `session.StoreFactory`. Authentication also requires revocable rotation. See [ownership and extension](../pkg/services/session/readme.md#ownership-and-extension). |
| Session encoding | [`session.Serializer`](../pkg/services/session/serializer.go), selected when constructing a store. Define how existing data is read or invalidated. See [serialization](../pkg/services/session/readme.md#replacing-serialization). |
| Mail delivery | [`messaging.MailSender`](../pkg/services/messaging/mail.go) through `NewMailClientWithSender`. Honor cancellation, validate headers, and report unavailable delivery as an error. The client does not close the sender. |
| Authentication workflow | [`auth.AuthService`](../auth/auth_service.go) plus the supported workflow interfaces. Select descriptors through `Options.AuthProviders`. Changing account storage alone does not require a new auth provider. |

Selected services handle concurrent requests. Keep mutable request data separate and give each resource one cleanup owner. The contracts linked above define failure and concurrency guarantees. Matching method signatures is not enough.

These extension points remain experimental. See the [compatibility policy](compatibility.md) before publishing an adapter.

## Application startup

### Signing keys

SQLite sessions and account recovery/verification tokens use opaque random tokens and need no signing keys.

If you enable cookie-backed sessions, generate `http.session.jwtKey` with `openssl rand -hex 32`. Configuration uses the output's bytes unchanged, without decoding. The cookie provider rejects keys shorter than 32 bytes and known placeholders. Cookie-backed sessions remain signed, not encrypted.

### Services and serving

[`application.New(ctx, cfg, application.Options{})`](../pkg/application/application.go) creates the core and opens configuration-selected accounts, sessions, and mail. It resolves paths on a configuration copy and closes acquired resources if startup fails. `Options.Services` accepts initialized services without taking ownership. `AccountProviders` and `SessionProviders` override built-in factories.

Select modules through `Options.Modules`, which runs after shared services are ready. Return descriptors that supply feature dependencies through their factories. Return `handlers.Registered()` to select the import catalog explicitly. A nil callback loads no modules. `Options.ProviderSelections` resolves ambiguous capabilities. The constructor uses `handlers.Setup` to load optional module configuration, initialize modules, and bind routes. Do not initialize the same modules again.

```go
app, err := application.New(ctx, cfg, application.Options{
    Modules: func(ctx context.Context, app *application.Application) ([]handlers.Descriptor, error) {
        return handlers.Registered(), nil
    },
})
if err != nil {
    return err
}
```

Services, module selection, and module initialization share one `app.initializationTimeout`, defaulting to ten seconds. Set shared rendering overrides in the callback before module setup prepares templates. Register any resources opened by the callback with `app.OnShutdown`. Modules close before those resources and shared services, including on startup failure.

### Authentication providers

If the selected catalog includes `auth` and `auth.enabled` is true, the constructor replaces that descriptor with one wired from the application's services, `Options.AuthPolicy`, and `Options.AuthProviders`. Select `auth.Module(auth.Dependencies{}, nil)` in the module callback. Arguments supplied to that descriptor are replaced, not merged. When using `handlers.Setup` directly, supply initialized dependencies and the provider catalog to `auth.Module` yourself.

The default provider catalog comes from `auth.Registered()`. The framework imports email/password and Google. Enable each with `auth.services.<provider>.enabled`. Disabled providers do not initialize. Imports alone do not select the auth module. See [Google authentication](google-authentication.md) for credentials, account linking, and setup.

To add an authentication provider, register an `auth.Descriptor` in the provider package's `init()`. Its `New(auth.Dependencies)` factory receives the application's accounts, sessions, mail, and policy. Import that package and enable its name in configuration. Package initialization must register metadata only. The factory must return a fresh instance without I/O.

External providers can use `auth.PrepareProviderView` and `auth.ProviderPage` for a shared page around their branded controls. Applications can override `auth.provider.introduction` without copying provider templates. Implement `LoginAvailability` to check whether an account has credentials for the provider. Availability checks affect presentation only. A failed check must not prevent another provider from authenticating.

After validating the protocol response and consuming one-use browser-bound state, call `auth.ExternalAccounts.Resolve` with the validated identity and the intent captured at flow start. The helper applies local account admission, registration availability, explicit linking, and account-version checks. It never links by matching email. Return the account to the manager to establish the session, including after registration when the provider has already authenticated the new account. For registration, supply `VerifiedEmail` only when current ownership is established. Linked identities can authenticate without an email claim. OIDC issuer/subject pairs and other providers' stable identity namespaces use the same storage contract.

Configure external form redirect destinations with `http.formActionOrigins`, for example `[https://accounts.google.com]`. This explicit allowlist extends the application's CSP without provider-specific blog edits. Values must be exact HTTP or HTTPS origins, not wildcards or CSP directives. The setting requires a CSP and cannot override `form-action 'none'`. It does not permit incoming cross-origin requests.

Use `Options.AuthPolicy` for shared account admission, registration availability, and successful-login behavior. Providers must call `Policy.Admit` before creating accounts or linking identities and `Policy.RegistrationAvailable` before creating an account. External providers must recheck those rules on their callback. The manager also checks the local account before creating a login session and calls `AfterLogin` only after that session is saved. Business permissions remain application rules.

The authentication module owns `/auth/account`, which requires login and any configured verification. Providers can implement `AccountProvider` to report a method's name, connection status, and typed links to its controls. The page shows enabled providers only. Applications can use `AuthManager.AccountMethods` to offer Account navigation only when actions are available. Provider handlers must enforce authorization independently of link visibility.

External providers can implement `ExternalIdentityProvider` to let the auth manager disconnect their local link. The manager requires an alternative enabled `PasswordLoginProvider` and confirms the password before removal. Account storage must implement `user.ExternalIdentityUnlinker` to remove the link against the same account version. Providers do not inspect each other's configuration. Password enrollment uses optional `user.PasswordSetupRepository` support and a separate, recipient-bound token purpose. Setup tokens cannot authorize password recovery.

For an explicit local catalog, set `Options.AuthProviders` to a callback returning `([]auth.Descriptor, error)`. It runs after module selection, before initialization, with shared services ready. It shares the startup deadline and runs only when auth is selected and enabled. Register callback-owned resources with `app.OnShutdown`. An empty result selects no providers, not the defaults. A custom catalog does not replace `Options.AuthPolicy`.

With `auth.enabled: false`, the constructor removes auth from the selected catalog before dependency resolution. It does not initialize auth, prepare its templates, or bind its routes. Sessions remain available independently.

### Login and registration routes

Site modules own `/login` and `/register`. Declare `Requires: []handlers.Requirement{{Capability: auth.ManagerCapability}}` in the module descriptor. In `BindDependencies`, receive the initialized manager with `handlers.GetDependency[*auth.AuthManager](dependencies, auth.ManagerCapability)`. In the module's `Routes` method, register its prepared handlers:

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

Prepare these handlers once during startup. They preserve anonymous-user checks and no-store headers. Register POST handlers through `Submissions` to retain the auth rate limits and startup diagnostics. `routing.Routes` supplies body limits and rejects route conflicts. Registration handlers return an unavailable response when registration is disabled.

A site that can run without auth can declare an optional dependency and skip registration when the manager is absent or disabled. The blog requires authentication. The [HTTP fixture](../internal/test/web/module_fixture_test.go) demonstrates optional authentication. Provider forms and links still use `/login` and `/register`. Custom paths are not supported by this API. Auth registers only its `/auth/...` operations and legacy redirects.

### HTTP serving and middleware

Serve `app.Handler`, not `app.Web`. The handler includes the standard web middleware, attaches selected sessions, and, when authentication is active, loads the current identity. Modules still choose protected routes with `auth/middleware.RequireAuthentication(app.Accounts, app.ContentManager)`. The constructor defaults the login destination to `/login`. Set presentation overrides on `app.ContentManager` in the module-selection callback. Login and home destinations must be local absolute paths.

`app.renderNotifications` enables session-backed page notifications, independently of authentication. The constructor validates the required session store before module initialization. A custom `ContentManager.LoadNotifications` supplied in the callback takes precedence over the flag and owns its dependency requirements. Authentication forms still use their own flash messages when page notifications are disabled.

The constructor installs `middleware.Web` once, outside session and identity loading. It provides security headers and cross-origin protection and applies the configured trusted proxies, HSTS, and shared rate limit. Set `Options.ContentSecurityPolicy` to match the application's assets. An empty value omits CSP. Do not wrap `app.Handler` with `middleware.Web` again. For a different stack, use the lower-level `server` and middleware APIs.

Attach a `*slog.Logger` with `logger.Set(ctx, log)` before calling `application.New` to enable startup and request logging. Without a logger, requests still receive IDs.

Use the returned application's `Run` method to drain requests and close owned resources, or call `Shutdown` after consumers stop. SMTP construction makes no network requests. Consumers that require delivery must validate the mail client. With no mail settings, `Mail` is nil.

### Health endpoints

`app.HealthHandler(runContext, timeout, checks...)` returns a handler with `GET /health/live` and `GET /health/ready` outside sessions, rate limits, and module routes. Readiness checks the run context, account repositories and session stores that implement `CheckHealth(context.Context) error`, then the supplied checks. Pass the application's run context, not a startup deadline. Checks share the timeout and must honor cancellation.

The blog command adds SMTP connectivity when registration requires email verification. Add `health.Check{Name: "mail", Run: app.Mail.CheckHealth}` when mail must affect readiness. Mail clients delegate to senders that implement the same optional method. Providers without that method are not probed. Custom probes must be read-only, safe for concurrent calls, and honor cancellation.

### Explicit setup

Use the lower-level APIs below when an application needs a different service lifecycle or HTTP stack.

`server.NewApplicationServer(cfg)` validates and copies configuration and creates the router and content manager. It does not load configuration files or create optional services. The core can run with `config.Config{}`.

Prepare shared services, select rendering and middleware, then initialize modules. Register owned resources with `app.OnShutdown` before their consumers. Supplying a service does not transfer ownership unless the receiving API explicitly says so.

Pass initialized services through module factories. Use `handlers.Setup(ctx, app, nil, catalog, selections)` to initialize modules with configured route defaults. See the [storage](storage.md) and [session](../pkg/services/session/readme.md#ownership-and-extension) guides for provider construction.

`smtp.NewMailClient(cfg.Mail)` returns a mail client using copied settings. Construction performs no network I/O. Its `CheckHealth` opens and closes a TCP connection, bounded by the caller's deadline and mail timeout. It does not verify TLS, authentication, or delivery. Other senders use `messaging.NewMailClientWithSender`.

Consumers that require delivery must call the client's `ValidateConfig`. Internationalized domains are converted to IDNA ASCII form for delivery. Non-ASCII mailbox names require the SMTP server's `SMTPUTF8` and `8BITMIME` extensions. Successful sending means server acceptance, not inbox delivery.

For authentication, supply [`auth.Module`](../auth/module.go) with `auth.Dependencies{Accounts: accounts, Sessions: sessions, Mail: mailClient}` and provider descriptors, such as [`email.Provider()`](../auth/modules/email/email_auth.go). Each provider factory uses the dependencies supplied to the module. Set `ContentManager.AuthURL` and `HomeURL`, then initialize the catalog through `handlers.Setup`. Wrap the mux with `authmiddleware.LoadSessionManagement(accounts, sessions)` and protect routes with `authmiddleware.RequireAuthentication(accounts, contentManager)`. For sessions without accounts, use `session.WithSessionManager(sessions)`. Install `flash.LoadNotifications` separately if pages need session-backed notifications. The application owns validation and cleanup in this lower-level path.

[`middleware.Web`](../pkg/services/middleware/web.go) installs logging, security headers, proxy verification, HSTS, the shared rate limit, and cross-origin protection in that order. Supply the application's CSP and logger. Pass `app.Web` for a stateless application or the session-aware handler returned by auth setup. Rejected requests receive security headers and are logged. For a different cross-origin policy or middleware order, compose the individual middleware explicitly.

`handlers.Setup` applies `http.defaultRateLimit`. Explicit routes passed to `handlers.Setup` keep their own defaults. `middleware.Web` applies the separate shared budget. Neither policy silently replaces the other.

Logging uses `*slog.Logger` and application-selected handlers. `LoggerMiddleware` attaches request fields to the logger returned by `logger.Get(r.Context())`. Use context-aware logging methods to pass cancellation and tracing values to the handler. Check for nil when no request logger is installed.

[`server.NewHTTPServer(app.Config.HTTP, handler)`](../pkg/server/http.go) creates a standard `*http.Server` with the configured address, timeouts, and optional TLS certificate. Resolve configuration paths first. It does not wrap the handler or open a listener. Timeout values pass through unchanged, including zero. Omitted read, write, and idle timeouts remain zero. The [configuration template](../config-template.yaml) supplies example values. Applications can still construct `http.Server` directly.

The HTTP write timeout includes handler work. For synchronous mail, allow time for `mail.timeout`, storage, and rendering. For example, allow a 40-second HTTP response budget for a 30-second SMTP timeout. The write timeout does not cancel handler work or replace the password-reset minimum response time.

`app.Run(ctx, prepare)` coordinates startup, serving, and shutdown. The preparation function returns the HTTP server, a `net.Listener` opened on its `Addr`, and an error. It must not start serving. Run cleans up startup failures and drains requests before closing services. Applications managing HTTP themselves can use `server.ServeHTTP` and call `Shutdown` after requests drain.

## Module catalogs and authentication

### Optional module configuration

Modules can implement `handlers.Configurable` to load settings before dependency binding and `Init`:

```go
type Settings struct {
    Title string `yaml:"title"`
}

type Module struct {
    settings Settings
}

func New() *Module {
    return &Module{settings: Settings{Title: "My application"}}
}

func (m *Module) Configuration() any {
    return &m.settings
}
```

The module also implements the usual `Init` and `Routes` methods. Its factory must create fresh settings. `Init` validates the loaded values and can apply explicit constructor overrides before validation.

For a module registered as `news`, the framework reads `modules/news/config.yaml` under `app.workingDirectory`. If the application uses `cmd/example/config.yaml` and leaves `app.workingDirectory` unset, the module file is `cmd/example/modules/news/config.yaml`:

```yaml
title: My news
```

Applications own runtime files. Module packages own defaults, validation, and configuration templates. Installed Go package paths do not affect file lookup. Configurable module names must be single local directory names, not `.` or `..`.

Missing or empty files keep defaults. Invalid or unreadable files stop startup. Required settings still must pass module validation. Modules without `Configurable` do not read configuration files. Loading happens once during initialization, with no directory search or module-specific environment overrides. Keep runtime `config.yaml` files out of source control.

For custom initialization outside `handlers.Setup` or `handlers.Initialize`, use [`config.LoadModuleFile`](../config/module.go) directly.

### Catalog selection

Imports register descriptors, not live modules. Use `handlers.Registered()` for a catalog copy or supply a local catalog. Factories must return fresh instances without I/O. `handlers.Replace` replaces the entire descriptor, including its dependency declarations. To replace only its factory, copy the original descriptor and change `New`.

`handlers.Setup` resolves dependencies, initializes modules, binds routes, and registers cleanup. Pass nil routes to use `http.defaultRateLimit`, or supply routes with explicit defaults. A nil catalog selects no modules. If setup fails, discard the application because its mux may still contain routes. Use `handlers.Initialize` only when managing route binding and cleanup yourself.

Auth has a separate descriptor catalog. Only configured, enabled providers initialize. An explicit empty catalog does not fall back to imports. Providers implement `AuthService` (`AuthType` and `Init`) and their supported workflow interfaces, such as `LoginHandler` or `ResetHandler`. If `Init` fails, startup stops before routes are bound.

The auth manager composes the login page registered by the site module. Providers supply controls with `GetLoginButton` or an inline form with `LoginFormHandler.LoginForm`. Login forms must submit a hidden `provider` field containing their `AuthType` to `/login`. Inline providers must link their alternative control to `/login?provider=<AuthType>` so users can switch forms. Prepare templates during initialization and return escaped markup without a page layout. Use `content.View.RenderPartial` to preserve application template overrides and request-local layout data. The shared page displays the inline form and other available provider controls, including after validation failures.

`Login` returns a `LoginResult` with either an authenticated `Account` or a rendered `Form` containing errors and non-secret submitted values. It must not write an HTTP response when returning either field. A zero result means the provider wrote its own response, such as an OAuth redirect. External providers implement `LoginCallbackHandler` to return a validated local account through the same session-completion path. The provider must validate its own browser-bound callback state.

Use `ProviderRoutes` for additional provider-specific endpoints, with explicit access checks. Its `Routes(routes, mutations)` method receives a general scope and a scope with the authentication mutation rate limit. Register state-changing endpoints on `mutations`.

Registration uses `GET /register` and `POST /register`. Providers implement `RegistrationHandler` and can supply inline controls through `RegistrationFormHandler.RegistrationForm`. Alternative inline controls link to `/register?provider=<AuthType>`. Forms post to `/register` with a hidden `provider` field. Before account creation, return `RegistrationResult{Form: markup}` for validation failures.

After account creation or an OAuth redirect, write the response and return an empty result. Providers retain ownership of verification delivery and post-creation recovery guidance. The shared page preserves alternative registration methods.

An application can set `Policy.LoginAccount` to tailor login choices to a known account, or return nil to show all configured methods. Providers can implement `LoginAvailability` to report whether that account has credentials. Providers without this capability remain visible. The blog uses this policy for its sole user with site access. Pending invitations keep all configured choices available. This policy controls presentation, not authorization or route availability.

Unsupported routes return 404. Registration without a supporting provider is reported as unavailable.

Auth providers configure request handling, not resource lifecycles. Their `Init` can validate settings and prepare templates. It must not open connections, query storage, send mail, or start background work. Application setup supplies those services and owns cleanup. The auth module does not call provider close hooks. Provider `Init` has no context, so custom template filesystems must return promptly.

## Module dependencies

Give each dependency one source. Use typed constructor parameters for application-owned services. Use declared capabilities only when one module supplies a service to another.

| Dependency source | Wiring | Cleanup owner |
| --- | --- | --- |
| `Options.Services` | Pass the initialized service into the module factory. | The caller retains ownership. HyperServer never closes supplied services. |
| Configuration-selected shared services | `application.New` opens services before `Options.Modules` runs. Pass them into module factories. | The application closes owned account and session storage after modules stop. The SMTP client holds no persistent connection. |
| A module capability | Declare `Requires` and receive the service in `BindDependencies`. | The providing module owns its resources. Consumers borrow them. |
| A module's internal resources | Open resources in `Init`. | The module releases partial resources on failure and implements `Shutdown` for successful startup. |

The blog borrows account storage through `Site.Configure` and the auth manager through `auth.ManagerCapability`. It owns content storage, uploads, and local asset handles. The auth module borrows accounts, sessions, and mail. Those shared services are not module capabilities.

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

`Setup` registers reverse-order cleanup automatically. During normal shutdown, requests drain first, then consuming modules close before their providers, followed by callback-owned resources and application-owned services. If using `Initialize` directly, register `handlers.Close` yourself. Repeated cleanup does not rerun module hooks.

| Budget | Control | Scope |
| --- | --- | --- |
| Application startup | `app.initializationTimeout`, default `10s`. | Service setup, module selection, and module initialization share one deadline. An earlier parent deadline wins. |
| HTTP draining | `http.shutdownTimeout`, default `10s`. | Requests already in progress, not module cleanup. |
| Resource cleanup | `Shutdown()` allows `10s`. `ShutdownContext(ctx)` accepts another deadline. | Reverse-order shutdown hooks. |

When called directly, `Initialize` uses the caller's deadline or defaults to ten seconds. Context deadlines cannot interrupt code that ignores cancellation. Initialization contexts must not be retained for request I/O.

If HTTP draining fails, Run closes HTTP connections and returns `server.ErrHTTPDrainIncomplete` without closing services under active handlers. The command must exit or wait for those handlers before cleanup. The blog command exits with an error. Applications must manage hijacked connections separately.
