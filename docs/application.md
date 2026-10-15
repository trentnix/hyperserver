# Application setup and modules

Use the [stateless application](../examples/stateless/readme.md) for a minimal setup and [cmd/web](../cmd/web) for accounts, sessions, and mail. The [README](../readme.md#module-registration) shows basic module registration and wiring.

## Application startup

### Signing keys

Configuration and built-in session constructors reject signing keys shorter than 32 bytes, as required for [HS256](https://www.rfc-editor.org/rfc/rfc7518#section-3.2). They also reject empty values and known placeholders without including secrets in errors. Length cannot establish randomness. Generate independent values for `auth.jwtKey` and `http.session.jwtKey` with a cryptographically secure generator, for example by running `openssl rand -hex 32` separately for each key. Paste each output unchanged: configuration uses the string's bytes without hex or base64 decoding.

### Services and serving

`server.NewApplicationServer(cfg)` returns `(*ApplicationServer, error)`. It validates shared settings, copies configuration including nested maps and pointers, and creates the router and content manager. It does not load configuration files or create optional services. A stateless application can use `config.Config{}` and serve `app.Web` through `net/http`.

Prepare shared services before initializing modules. The reference application owns its SQL pool in [application.go](../cmd/web/application.go), checks connectivity with a ten-second deadline, and registers pool cleanup before consumers. The core has no SQL pool. Applications or providers must import their SQL drivers.

Pass initialized accounts and sessions to `auth.Module(accounts, sessions, providers)`, `email.Provider(accounts, sessions, mailClient)`, and authentication middleware. See [session factories](../pkg/services/session/readme.md#ownership-and-extension) and [reference session setup](../cmd/internal/setup/sessions.go).

For mail, pass `smtp.NewSender(cfg.Mail)` to `messaging.NewMailClientWithSender`, then supply the client to modules. [Mail setup](../cmd/web/mail.go) and [module wiring](../cmd/web/site_modules.go) show the calls. Construction does not contact SMTP. Consumers requiring delivery must validate configuration.

Register owned services with `app.OnShutdown` before initializing consumers. Supplied services keep their existing owners. [Reference session setup](../cmd/web/sessions.go) registers cleanup for managers it creates or when `ownSessions` transfers ownership.

`app.Run(ctx, prepare)` coordinates serving and cleanup. The preparation function receives a context and returns a configured `*http.Server`, a `net.Listener`, and an error. It must not start serving. Run closes resources on startup failure and drains requests before cleanup on cancellation. See [shutdown behavior](#module-cleanup) and the [stateless runner](../examples/stateless/main.go).

Applications managing HTTP themselves can use `server.ServeHTTP` and call `Shutdown()` after requests drain. Signal handling belongs in the command.

For session-only middleware, use `session.WithSessionManager(sessions)`. For authenticated identity loading, use `authmiddleware.LoadSessionManagement(accounts, sessions)` from `auth/middleware`, alongside `RequireAuthentication` and `RequireAnonymous`. The reference application selects loading behavior through `auth.enabled`. General logging, security headers, composition, and application-defined authorization live in `pkg/services/middleware`.

## Module catalogs and authentication

Use `handlers.Setup` for dependency resolution, initialization, route binding, and cleanup registration. A nil catalog selects no modules. For route defaults, pass a configured `*routing.Routes` instead of nil. If setup fails, discard the application because its mux can still contain registered routes. Use `handlers.Initialize` only when managing route binding and cleanup yourself.

`handlers.Replace` replaces the whole descriptor, including `Provides` and `Requires`, without changing other catalogs. To replace only a factory, copy the original descriptor and change `New`. Module-owned constructors supply declarations that match their configuration.

Modules own registration, internal setup, requirements, and routes. Applications select modules and supply shared services. Modules must not initialize or close those supplied services. The [README example](../readme.md#module-registration) wires contact storage directly without a provider module.

Startup logs show module names and advertised capabilities in initialization order, explicit selections, and auth providers in catalog order. They also show the account-repository type, session-provider names, and mail-client choice. Logs omit configuration values and connection details. They describe selection, not service health. Unchosen provider modules still appear if they remain in the catalog.

Auth providers use `auth.Descriptor` and `auth.Register`. The auth module creates its manager and enabled providers, validates verification settings during `Init`, and binds routes during `Routes`. Supply initialized accounts and sessions and configure provider dependencies before activation. An explicit empty provider catalog does not fall back to imports. Registration does not alter running applications.

The [README example](../readme.md#module-registration) selects only email authentication. [Reference module setup](../cmd/web/site_modules.go) instead replaces the email descriptor while preserving other discovered providers.

`auth.AuthService` requires `AuthType`, `Init`, and `IsLoaded`. Add `LoginHandler`, `RegistrationHandler`, `ResetHandler`, or `ChangeHandler` for supported workflows, and `ProviderRoutes` for extra routes. The manager displays supported choices and returns 404 for unsupported workflows. If no provider supports registration, the registration page reports it unavailable. Verification configuration is required only for registration-capable providers when registration and required verification are enabled.

`AuthService.Init` receives no context, and the auth module does not call provider shutdown hooks. Initialize resource-owning dependencies in application setup, pass them to provider factories, and close them through application cleanup.

## Module dependencies

Give each dependency one source. Pass application-owned services through typed constructors. For module-supplied services, declare `Requires` and read them through `BindDependencies`. If a module supports both sources, its descriptor constructor must select one without silently overwriting a supplied service.

Descriptors can declare `Provides: []string{"post-storage"}` and `Requires: []handlers.Requirement{{Capability: "post-storage"}}`. Capability and module names are case-sensitive. `handlers.Initialize` validates the catalog before invoking factories, then binds dependencies and initializes providers before consumers. Missing requirements, ambiguous providers, duplicate names, invalid metadata, and dependency cycles return errors. Use `handlers.Resolve` to check metadata without creating modules.

If several modules provide a required capability, select one by module name:

```go
modules, err := handlers.Initialize(ctx, app, handlers.Registered(), map[string]string{
    "post-storage": "postgres-posts",
})
```

`Optional: true` permits a missing provider. `Multiple: true` accepts all matching providers. Optional requirements still reject ambiguity unless they accept multiple providers. An explicit selection narrows either form to one provider, and invalid selections always fail. All modules in the supplied catalog still initialize, including unchosen alternatives. Remove a module from the local catalog to exclude it from the application.

Providers implement `Capabilities() map[string]any`, returning exactly their declared services after `Init` succeeds. Consumers implement `BindDependencies(handlers.Dependencies) error`, called before their `Init`. Both hooks must avoid I/O. Consumers request their own interfaces:

```go
func (m *PostsModule) BindDependencies(d handlers.Dependencies) error {
    var err error
    m.repository, err = handlers.GetDependency[PostRepository](d, "post-storage")
    return err
}
```

Only declared requirements are available. An absent optional capability returns the requested type's zero value, so an interface field is `nil`. The consumer must handle that absence. Use `GetDependencies[PostRepository]` for several providers. It returns a new slice in catalog order, or an empty slice when an optional capability is absent. Wrong types, nil services, missing hooks, and initialization failures stop startup. An optional requirement does not hide a broken provider.

`Initialize` uses the caller's deadline, or ten seconds if none is set, for all module initialization. Pass `context.WithTimeout` to change the budget. Modules must open and validate storage during `Init`, before consumers receive it or routes bind. Use the initialization context for setup I/O, not later requests. Deadlines cannot interrupt operations that ignore context, including some filesystem calls.

The reference application sets that deadline from `app.initializationTimeout`, which defaults to `10s` and must be positive. For longer module setup:

```yaml
app:
  initializationTimeout: 2m
```

`HYPERSERVER_APP_INITIALIZATIONTIMEOUT` overrides the file. The budget covers all module initialization, not each module separately, and an earlier parent deadline wins. Database, account, and session setup retain their separate ten-second limits. The initializer itself does not read configuration.

## Module cleanup

A module that owns resources can implement `Shutdown(context.Context) error`. Modules without owned resources need no shutdown hook. `handlers.Initialize` rolls back completed modules when startup fails. A failing `Init` must release its own partially acquired resources. Factories and dependency-binding hooks must not acquire resources.

`handlers.Setup` registers module cleanup with `app.OnShutdown`. If you use `handlers.Initialize` directly, register `handlers.Close` yourself. Call `app.Shutdown()` on later startup errors and after requests drain. Cleanup runs consumers before providers, leaves borrowed services open, and reports all errors. Repeated cleanup calls do not rerun module hooks.

`Shutdown()` gives cleanup ten seconds. `ShutdownContext(ctx)` accepts a different deadline. Modules must honor cancellation and stop their background work before returning. Cleanup cannot interrupt code that ignores context or context-free storage close functions.

The reference application handles `SIGINT` and `SIGTERM`, stops accepting requests, and gives active requests `http.shutdownTimeout` to finish. The setting defaults to `10s` and must be positive. To allow longer requests to drain:

```yaml
http:
  shutdownTimeout: 30s
```

`HYPERSERVER_HTTP_SHUTDOWNTIMEOUT` overrides the file. This budget controls HTTP draining only, not request duration or module cleanup. After draining, the application closes modules, account providers, session providers, and the shared database.

If draining fails, Run closes HTTP connections and returns `server.ErrHTTPDrainIncomplete` without closing services under active handlers. The command must exit or wait for those handlers before cleanup. The reference command exits with an error. A second signal forces exit. Applications must manage hijacked connections separately.

See the [storage-provider example](../pkg/handlers/example_test.go). `Initialize` does not bind routes and returns no instances on failure.
