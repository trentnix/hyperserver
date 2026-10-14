# HyperServer

HyperServer is an experimental Go framework for server-rendered web applications with first-class HTMX support. It aims to handle recurring web application infrastructure so developers can focus on application features using familiar Go handlers and HTML templates.

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

Authentication requires SQLite sessions. Login and account verification rotate the session ID. Password reset or change signs out all devices. Cookie-only sessions are available for non-authentication data.

Applications define access rules through [route-level authorization policies](auth/middleware/authorization_example_test.go), separate from authentication. Policies can inspect the loaded identity and route values. Denial returns 403, and policy failures return 500 without exposing the error, for both ordinary and HTMX requests.

Session data uses JSON, limited to 64 KiB before base64 encoding. Cookies are limited to 4 KiB including their name and attributes. Cookie-store data is signed, not encrypted, and must not contain secrets. See the [session guide](pkg/services/session/readme.md) for supported values and typed reads.

Applications supply optional services through typed constructors. Modules can declare dependencies on other modules for ordered initialization. Application setup owns shared services and their cleanup.

## Direction

Provide a working default setup for common application needs, including authentication, sessions, mail, forms, and response handling. Components must be replaceable without requiring every application to assemble its infrastructure from scratch. High performance, low overhead, and readable code are design goals.

Keep the core focused on rendering and HTMX, with replaceable services and self-registering modules. Preserve Go's `net/http` model, prefer the standard library, and keep dependencies small. Use maintained security implementations rather than custom cryptography. Storage and serialization must be independently replaceable. Applications can use ordinary forms or depend on HTMX.

The [roadmap](roadmap.md) and [architecture decisions](docs/decisions/readme.md) describe intended capabilities and priorities. Security and correctness fixes come first.

## Exploring the code

Use the Go version declared in [go.mod](go.mod), which also controls CI. SQLite tests and the race detector require CGO and a C compiler.

Start with [cmd/web](cmd/web) for application composition, [modules/site](modules/site) for the reference application, and [pkg](pkg) for framework components and services.

For a smaller starting point, run `go run ./examples/stateless` and open http://127.0.0.1:8082. The [stateless greeting application](examples/stateless/readme.md) uses its own module and embedded templates without a configuration file, database, sessions, authentication, or mail.

The development [site module owns the diagnostic and sample routes](modules/site/router.go). Production applications must not import it. Omitting it does not resolve the framework's outstanding security issues.

`POST /test-email` allows logged-in users who have completed any required account verification. Other users receive 403, and no mail is sent.

### Configuration

Use [config-template.yaml](config-template.yaml) as the starting point for a local `config.yaml`, not a production configuration.

The [configuration loader](config/load.go) uses the first `config.yaml` in `.`, `config`, `../config`, or `../../config`. Precedence is defaults, then the file, then `HYPERSERVER_` environment values. For example, `HYPERSERVER_MAIL_PASSWORD` overrides `mail.password`, including an empty value. Unknown settings and invalid values stop startup. Durations require units such as `5s` or `2h`, except `0`. Use a YAML list or comma-separated environment value for `http.trustedProxies`.

The web and cleanup commands each load configuration once. Services validate the supplied settings without rereading files or environment variables. Later file or environment changes do not reconfigure a running application. Applications supplying `config.Config` directly bypass file loading, environment overrides, and loader defaults. Constructors still apply their documented zero-value defaults, such as SMTP's `30s` timeout.

Provider selection uses the same resolved configuration:

| Service | Default and replacement |
| --- | --- |
| Accounts | Empty `auth.accountStorage.provider` selects SQLite. Select a registered provider or supply an initialized repository. See [account storage](#account-storage). |
| Sessions | `http.session.types` maps session names to stores, with `default` as the fallback. There is no implicit store when that mapping is absent. Unknown or disabled selections fail. [Custom stores](pkg/services/session/readme.md#ownership-and-extension) currently require code wiring. |
| Authentication | Only configured, enabled providers in the selected auth catalog are instantiated when `auth.enabled` is true. Importing a provider alone does not enable it. |
| Mail | The reference application creates an SMTP sender from `mail` settings unless its `Mail` field already holds a client. Use `messaging.NewMailClientWithSender` for another sender. Incomplete SMTP configuration leaves delivery unavailable, not replaced by another sender. |

To exercise two storage platforms together, run `go run ./cmd/web -contact-directory ./tmp/contacts`. Accounts stay in SQLite, while the site saves contacts as individual JSON files in a private directory. The file provider requires hard-link support. Without this flag, contacts also use SQLite. This is reference-application wiring, not a framework-wide storage setting.

SQLite contact startup checks writable columns, supported types, required email values (`NOT NULL`), and a non-partial unique submission-ID key with binary comparison. Incompatible tables stop site initialization. Setup does not repair existing tables or validate stored records.

### Application startup

`server.NewApplicationServer(cfg)` accepts a `config.Config` and returns `(*ApplicationServer, error)`. It validates shared settings, copies the configuration including nested maps and pointers, and creates the router and content manager. Changes to the source configuration do not change the application. Configure services before serving requests. Construction does not read configuration files or initialize database, session, authentication, or mail services. A stateless application can use `config.Config{}` and serve `app.Web` through `net/http`.

Prepare storage in application setup, before initializing its consumers. The reference application owns its SQL pool in [application.go](cmd/web/application.go) and registers pool cleanup with `OnShutdown` before account and contact setup. The core has no SQL-pool field. The reference application checks SQL connectivity with a ten-second deadline. SQL drivers must be imported by the application or its storage providers.

Prepare accounts and sessions in application setup and pass them to `auth.Module(accounts, sessions, providers)`, `email.Provider(accounts, sessions, mailClient)`, and authentication middleware. The core holds neither service. Supply built-in or custom session providers through factories as shown in [Sessions](pkg/services/session/readme.md#ownership-and-extension). The reference application's [setup](cmd/internal/setup/sessions.go) selects its providers from configuration.

For SMTP, construct a sender with `smtp.NewSender(cfg.Mail)` from [`pkg/services/messaging/smtp`](pkg/services/messaging/smtp/smtp.go), then pass it to `messaging.NewMailClientWithSender`. The core has no mail-client field. Pass the client to `site.Module(newContacts, mailClient)` and `email.Provider(accounts, sessions, mailClient)` from [`auth/modules/email`](auth/modules/email/email_auth.go). [Reference mail setup](cmd/web/mail.go) and [module wiring](cmd/web/site_modules.go) show the wiring. Construction does not contact SMTP, and consumers that require delivery must validate configuration. After preparing services, use `handlers.Setup` for module initialization, route binding, and cleanup registration.

If application setup owns the session manager, register its `Close()` with `app.OnShutdown` before initializing modules. A supplied manager keeps its existing owner. [Reference session setup](cmd/web/sessions.go) registers cleanup for managers it creates, or when `ownSessions` explicitly transfers ownership.

`app.Run(ctx, prepare)` owns serving and application cleanup. The preparation function receives the context and returns a configured `*http.Server`, a `net.Listener`, and an error. It selects services and modules but must not start serving. Run closes resources on startup failure and drains requests before cleanup on cancellation. `http.shutdownTimeout` controls draining, with a `10s` default.

If draining fails, Run closes connections and returns `server.ErrHTTPDrainIncomplete` without closing services under active handlers. The command must exit or wait for those handlers before cleanup. Signal handling stays in the command. See the [stateless example](examples/stateless/main.go) for a complete runner. Applications managing HTTP themselves can use `server.ServeHTTP` and call `Shutdown()` after requests drain.

`pkg/services/middleware` provides logging, security headers, handler composition, and application-defined authorization without importing account or session storage. Use `session.WithSessionManager(sessions)` to attach sessions without loading an identity. Import `auth/middleware` as `authmiddleware` for `LoadSessionManagement`, `RequireAuthentication`, and `RequireAnonymous`. `LoadSessionManagement(accounts, sessions)` attaches the supplied manager and loads the authenticated user. The reference application chooses session-only or authenticated loading using `auth.enabled`.

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

`handlers.Registered()` returns a catalog copy. Applications can instead supply a local `[]handlers.Descriptor` to select modules without changing the import catalog. Each factory must return a fresh instance without I/O. Resolve dependencies, create the instances, then initialize them and bind their routes before accepting requests.

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

`handlers.Setup` resolves dependencies, initializes modules, checks route registrations, and registers reverse-order cleanup with the application. Pass a configured `*routing.Routes` instead of nil to supply route defaults. Failed setup closes initialized modules, but any registered routes remain in the mux. Discard the failed application. A nil catalog selects no modules. The lower-level `handlers.Initialize` remains available when callers need to manage routes and module cleanup themselves.

After successful module setup, the reference application logs module names and advertised capability providers in initialization order, plus explicit provider selections. Authentication reports its initialized providers in catalog order. Application setup reports the actual account-repository type, selected session-provider names, and whether mail uses SMTP or an application-supplied client. These diagnostics identify selections, not service health. They exclude configuration values and connection details. Unchosen provider modules still appear if they remain in the selected catalog.

`contactStore` implements `site.ContactRepository`. The application prepares the store and closes it after requests drain. This direct wiring does not require a separate provider module.

`Replace` replaces the entire descriptor, including `Provides` and `Requires`. A module-owned constructor supplies the appropriate declarations for its configuration. To replace only a factory, copy the original descriptor and change `New` before passing it to `Replace`. Other applications keep their original catalog.

Modules own their registration, internal setup, requirement checks, and routes. Applications select modules, supply shared services, and coordinate startup and shutdown. Modules must not initialize or close borrowed services. A configurable module can expose a descriptor constructor: the site's `site.Module(newContacts, mailClient)` requires a contact-storage factory and accepts an optional mail client. The reference application selects SQLite or file storage in [site_modules.go](cmd/web/site_modules.go).

Auth providers use `auth.Descriptor` and `auth.Register`. The registered auth module creates its own manager and enabled providers, validates verification configuration during `Init`, and binds manager and provider routes during `Routes`. Initialize its required services first, as shown in [the reference startup](cmd/web/main.go). For an explicit provider catalog, replace the default module with `auth.Module(accounts, sessions, providers)` using `handlers.Replace`. An empty explicit catalog never falls back to imports. Adding a descriptor does not activate it in applications already running.

The example uses the same initialized `accounts` repository and `sessions` manager for the auth manager and email provider. Its explicit provider list selects only email authentication. Import discovery still works, but applications must supply accounts and sessions to the auth module and configure each provider's dependencies before activation. [Reference module setup](cmd/web/site_modules.go) replaces the email descriptor in a snapshot while preserving other discovered providers.

`auth.AuthService` requires only `AuthType`, `Init`, and `IsLoaded`. Add `LoginHandler`, `RegistrationHandler`, `ResetHandler`, or `ChangeHandler` for the workflows a provider supports. A login-only provider needs no registration or password-reset stubs. The manager shows only supported login and registration choices and rejects unsupported provider workflows with 404. If no provider supports registration, the registration page reports that registration is unavailable. Verification configuration is required only for registration-capable providers when registration and required verification are enabled. Implement `ProviderRoutes` only when a provider needs extra routes.

### Module dependencies

Give each dependency one source. Pass application-owned services through typed constructors, as in `auth.Module(accounts, sessions, providers)`. If another module supplies a service, declare it in `Requires` and read it through `BindDependencies`. Do not also declare a requirement for a service already supplied by a constructor. A module that supports both sources must select one in its descriptor constructor, not silently overwrite a supplied service during binding.

`ApplicationServer` supplies the core configuration, router, and renderer, not optional services. Request middleware attaches the application's selected session manager and authenticated identity to requests. Handlers use that request state without selecting or opening another provider.

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

`Initialize` uses the caller's deadline, or ten seconds if none is set, for the entire initialization phase. Pass `context.WithTimeout` to choose a different budget. Modules must open and validate their storage during `Init`, before consumers receive it or routes bind. Use the initialization context for setup I/O, not later requests. Deadlines cannot interrupt operations that ignore context, including some filesystem calls. Borrowed pools remain open until their application owner closes them after consumers stop.

The reference application sets that deadline from `app.initializationTimeout`, which defaults to `10s` and must be positive. For longer module setup:

```yaml
app:
  initializationTimeout: 2m
```

`HYPERSERVER_APP_INITIALIZATIONTIMEOUT` overrides the file. The budget covers all module initialization, not each module separately, and an earlier parent deadline wins. Database, account, and session setup retain their separate ten-second limits. The initializer itself does not read configuration.

### Module cleanup

A module that owns resources can implement `Shutdown(context.Context) error`. Modules without owned resources need no shutdown hook. `handlers.Initialize` rolls back completed modules when startup fails. A failing `Init` must release its own partially acquired resources. Factories and dependency-binding hooks must not acquire resources.

`handlers.Setup` registers module cleanup with `app.OnShutdown`. If you use `handlers.Initialize` directly, register `handlers.Close` yourself. Call `app.Shutdown()` on later startup errors and after requests drain. Cleanup runs consumers before providers, leaves borrowed services open, and reports all errors. Repeated cleanup calls do not rerun module hooks.

`Shutdown()` gives cleanup ten seconds. `ShutdownContext(ctx)` accepts a different deadline. Modules must honor cancellation and stop their background work before returning. Cleanup cannot interrupt code that ignores context or context-free storage close functions.

The reference application handles `SIGINT` and `SIGTERM`, stops accepting requests, and gives active requests `http.shutdownTimeout` to finish. The setting defaults to `10s` and must be positive. To allow longer requests to drain:

```yaml
http:
  shutdownTimeout: 30s
```

`HYPERSERVER_HTTP_SHUTDOWNTIMEOUT` overrides the file. The setting controls HTTP draining only, not normal request duration or the separate module-cleanup deadline. After requests drain, the application closes modules, account providers, session providers, and the shared database. If draining times out, it closes HTTP connections and exits with an error without closing services underneath running handlers. A second signal forces exit. Applications that use hijacked connections must manage those connections separately.

See the [complete storage-provider example](pkg/handlers/example_test.go). Modules with only constructor-supplied dependencies need no binding hook. `Initialize` does not bind routes. On failure, it closes completed modules and returns no instances.

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

Rendering helpers use `ContentManagerService.ApplyDefaults(owner, configure)` to select one shared policy. Selecting a second policy, including selecting the same one twice, returns an error before changing settings. Ordinary field assignments and template registration remain available for application overrides. The guard prevents competing helper selections, not arbitrary writes by module code. Modules must leave application-wide settings to setup.

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

Child scopes share the first error. Later registrations through those scopes do nothing. Earlier routes remain registered, so the application must not serve a mux after registration fails. The reference application checks after every module and stops startup before listening. Checks run during route binding, after module initialization. They do not roll back module resources.

Direct `http.ServeMux.Handle` calls retain Go's panic behavior. Panics in module code, diagnostic callbacks, or request handlers are not converted into registration errors.

### Account storage

In the reference application, `auth.accountStorage.provider` selects account persistence, separate from the login providers under `auth.services`. Empty or omitted selects `sqlite`, which uses the application-owned `database.connection`. SQLite accepts no `auth.accountStorage.options`. Unknown provider names stop startup when authentication is enabled. The web and cleanup commands share this selection code in [reference account setup](cmd/internal/setup/accounts.go), not in the core server.

Account models, token operations, and storage contracts live in [`user`](pkg/services/user). The SQLite implementation lives in [`user/sqlite`](pkg/services/user/sqlite). Import that provider when constructing `sqlite.NewAccountRepository(pool)` directly. The repository borrows the pool and must be initialized before use.

SQLite account initialization creates schema version 1 only when no account storage objects exist. Existing storage must have version 1 recorded in `hyperserver_account_version` with `dirty = false`. Missing, malformed, dirty, and unsupported versions stop startup without changes. Initialization also checks writable columns, supported types, required `NOT NULL` declarations, and unique keys for account IDs, normalized email keys, and account/token pairs. The keys must cover every row and use binary comparison. Tokens must reference `user(id)` through `user_id` with `ON DELETE CASCADE`. Initialization does not migrate existing tables or validate every stored value.

Applications supply an initialized `user.AccountRepository` to their auth factories and middleware. For a borrowed SQLite pool:

```go
accounts := sqlite.NewAccountRepository(pool)
if err := accounts.Initialize(ctx); err != nil {
    return err
}
```

Pass `accounts` to the factories shown in [Module registration](#module-registration). If a repository owns resources, register its cleanup with `app.OnShutdown` before initializing its consumers. Borrowed pools keep their existing owner. Other providers implement the same account contract without changing the core server.

#### Storage guarantees

Replacement providers must preserve the [account](pkg/services/user/account_repository.go), [session](pkg/services/session/store.go), and [contact](modules/site/site.go) contracts, not just their method signatures. Account email matching and uniqueness use [EmailKey](pkg/services/user/email.go): Unicode case-insensitive local parts and equivalent Unicode/Punycode domains share a key. The original address is preserved for display and delivery. Dots, tags, local-part quoting, and local-part Unicode normalization remain significant. Domain comparison follows non-transitional IDNA rules, so `faß.example` and `fass.example` remain distinct. Account security changes use version checks and atomic writes. Contact creation inserts a new submission on each call. Repeating a request can create another submission. Session stores support [serializer replacement](pkg/services/session/readme.md#replacing-serialization) independently of storage, with JSON as the default.

SQLite writes complete before reporting success. File-backed SQLite data survives normal close and reopen. Crash durability depends on SQLite settings and the filesystem. In-memory databases are temporary. The example file contact provider publishes complete files without overwriting existing records, but does not sync them to disk or promise power-loss durability. Its filesystem calls cannot be interrupted by context cancellation. Applications requiring stronger guarantees must select another provider.

Factories must reject unsupported settings and release partially acquired resources. Authentication rejects session providers without revocable rotation, and maintenance rejects missing cleanup capabilities. Interface checks cannot prove a custom provider's behavior. Its tests must establish the required guarantees. See [session behavior](pkg/services/session/readme.md#storage-guarantees) for concurrent saves and expiration.

Accounts, contacts, and each configured SQLite session table have independent schema versions. Default session payloads have a separate format version and support reading unversioned JSON objects. See [session serialization](pkg/services/session/readme.md#replacing-serialization) for compatibility and size limits. Schema declaration checks do not validate every existing record. The [roadmap](roadmap.md#storage-and-serialization) defines migration ownership and the upgrade procedure.

#### Account schema migrations

Accounts use the separately installed [golang-migrate v4.20.1 CLI](https://github.com/golang-migrate/migrate/releases/tag/v4.20.1). Migrations are plain SQL files: [up](pkg/services/user/sqlite/migrations/000001_accounts.up.sql) creates version 1 and [down](pkg/services/user/sqlite/migrations/000001_accounts.down.sql) removes its tables. On first startup, the application creates the SQLite database file if needed. Account initialization executes the embedded up file and records version 1 in one transaction. No migration executable or manual setup is required for a fresh database.

The account-specific `hyperserver_account_version` table records the current version and whether a migration is incomplete (`dirty`). Later startups check this metadata without changing it. Unversioned account databases have no supported upgrade path. Recreate disposable development databases rather than adding version metadata by hand. Startup never deletes existing storage or automatically runs later migrations.

Install the pinned CLI with its SQLite driver, outside the application's dependency graph. The executable goes in `GOBIN`, or `$(go env GOPATH)/bin` if `GOBIN` is unset. With that directory on `PATH`, run from the repository root against a database with no account tables:

```sh
go install -tags sqlite3 github.com/golang-migrate/migrate/v4/cmd/migrate@v4.20.1
migrate -path pkg/services/user/sqlite/migrations \
  -database 'sqlite3://file:///absolute/path/database.db?x-migrations-table=hyperserver_account_version&_foreign_keys=on' up
```

Stop affected writers, including cleanup jobs, and back up existing data before deployment migrations. Run only one migration process per database at a time. The SQLite driver does not coordinate locks across migration processes. Keep the account-specific table setting in every command. Use `down 1` to reverse version 1. After down, startup rejects the empty version table until an explicit `up` recreates the schema. If a migration fails, inspect and repair the database before using the tool's `force` command to correct its version marker. `force` does not run or undo SQL.

Neither application code nor Go tests import golang-migrate. The CLI has its own dependencies to maintain, but adds none to this module. Only the SQLite provider knows its version-table format. Replacing the tool would require converting that metadata, not account records or SQL annotations.

CI installs the pinned CLI and tests bootstrap compatibility, up/down, independent version tables, failed-migration rollback, and explicit repair. To run those tests locally:

```sh
HS_TEST_MIGRATE=migrate go test ./pkg/services/user/sqlite -run '^TestAccountMigration' -count=1
```

The command fails if `migrate` is not on `PATH`. You can also set `HS_TEST_MIGRATE` to an absolute executable path. Without `HS_TEST_MIGRATE`, CLI integration tests skip. Schema and version-validation tests still run.

#### Contact and session migrations

Contact storage follows the same first-start and version-check policy as accounts, using [contact migrations](modules/site/database/migrations/000001_contacts.up.sql) and `hyperserver_contact_version`. To run its migrations explicitly:

```sh
migrate -path modules/site/database/migrations \
  -database 'sqlite3://file:///absolute/path/database.db?x-migrations-table=hyperserver_contact_version&_foreign_keys=on' up
```

See [session migrations](pkg/services/session/readme.md#sqlite-schema-migrations) for per-table versions and custom table names. These providers can share a database without sharing version markers. Run each provider's migrations separately.

#### SQLite connections

The reference application and built-in session and maintenance providers use the [SQLite pool opener](pkg/database/sqlite/pool.go). Each pool allows one open connection and retains it while idle. Every new or replacement connection enables foreign keys and waits up to 5000 milliseconds for locks. Set `_timeout` or `_busy_timeout` in the connection string to change that wait. Zero disables waiting. Disabling foreign keys is rejected.

Cancellation stops a wait for a free pool connection. During a SQLite lock wait, cancellation can be delayed until the lock timeout and return a lock error instead of a context error. Caller-supplied pools retain their owner's settings.

#### Direct backend access

Keep the native client in application setup and pass it to modules that need it. Repositories do not take that access away. For example, the same `pool` used above can serve a reporting query:

```go
var accountCount int
if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM user").Scan(&accountCount); err != nil {
    return err
}
```

Use a typed constructor parameter such as `*sql.DB` for application-supplied clients. If another module supplies the client, publish it as a capability, declare the requirement, and retrieve it with `handlers.GetDependency[*sql.DB](d, "sqlite-pool")`. The [shared-pool test](pkg/handlers/storage_initialization_test.go) covers native queries alongside account and contact repositories. Other backends can use their own client types without adding a framework interface.

Application setup owns the clients it supplies and closes them after modules stop. Modules must not close supplied clients. A module that creates its own client owns its cleanup. Modules must still close query results and any dedicated `*sql.Conn` they acquire. Closing a dedicated connection returns it to the pool without closing the pool.

Direct access couples that code to the backend. Direct writes must preserve the affected service's rules, including validation, token invalidation, and session revocation. Sharing a pool does not make separate operations one transaction.

#### Provider selection

The reference application's [shared setup](cmd/internal/setup) is private to packages under `cmd`. It supports named factories. In the web command, register replacements before initialization:

```go
accounts := setup.NewAccountStorage(app.Config)
if err := accounts.RegisterProvider("company", setup.AccountProvider{
    Open: openCompanyAccounts,
    OpenMaintenance: openCompanyTokenMaintenance, // Optional. Opens existing storage only.
}); err != nil {
    return err
}
if err := initializeAccounts(ctx, app, accounts); err != nil {
    return err
}
```

This `cmd/internal/setup` helper uses configuration, not a server instance. Provider factories receive the startup context and a copy of the configured options. `accounts.Open(ctx, pool)` returns an initialized repository and its optional cleanup function. Failed opens release partial acquisitions. The web command's [initializeAccounts](cmd/web/accounts.go) supplies the repository and registers its cleanup. Cleanup runs after consumers stop and before the shared pool closes.

Select the registered name and its options in configuration:

```yaml
auth:
  accountStorage:
    provider: company
    options:
      connection: your-provider-connection
```

Reference provider registrations belong to one setup instance. Names are case-sensitive, and `sqlite` is reserved. Supplying `AccountRepository` on the reference application's [setup struct](cmd/web/application.go) bypasses selection and initialization. The reference application ships only SQLite. Its shared database and session setup remain separate from account-provider selection.

`OpenMaintenance` returns the separate [TokenMaintenance](pkg/services/user/account_repository.go) capability, an optional close function, and an error. It must not create or migrate tables. The cleanup command calls `setup.CleanupAccountTokens(ctx, cfg, limit)` without constructing a server or `AccountStorage`. Provider selection is shared with web startup. If setup registers custom providers, its `CleanupTokens` method uses those registrations through the same maintenance implementation. Each batch closes acquired resources, even when opening or deletion fails. A missing capability returns an error rather than silently skipping cleanup. Applications that already hold a repository implementing `TokenMaintenance` can call `DeleteExpiredTokens` directly.

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

Run `go run ./cmd/cleanup -batch-size 100 -timeout 10s` with the same configuration as `cmd/web`. Both commands resolve storage paths from that configuration. Each run deletes at most 100 expired rows from each selected server-side session store and, when authentication is enabled, account-token storage. Storage providers own maintenance setup and pool handling. SQLite database files and tables must already exist. Cleanup does not create missing files or support in-memory databases. Live records and accounts are unchanged.

The supplied command includes the built-in providers. A custom account provider must also be registered in the application's maintenance executable. Unknown providers and missing maintenance capabilities produce an error without falling back to SQLite. Independent session cleanup still runs.

Schedule the command externally for regular cleanup. The server does not run cleanup automatically. The command reports deleted counts and exits with an error if either cleanup fails. Expiration checks still reject expired sessions and tokens before cleanup runs.

### Checks

Tests live beside the code they exercise. Shared storage contract helpers live in [internal/test](internal/test) and can be used by tests throughout the repository.

From the repository root, run the existing checks with:

```sh
go test ./...
go vet ./...
go test -race ./...
```

The [CI workflow](.github/workflows/ci.yml) runs these checks on pushes and pull requests. See [AGENTS.md](AGENTS.md) for working agreements and regression-test guidance.

CI also tests error responses, native forms, HTMX redirects, retargeting, reswapping, and out-of-band notifications in headless Firefox. Run `HS_TEST_FIREFOX=firefox go test -count=1 -timeout=5m ./cmd/web -run '^TestBrowser(SiteErrors|NativeCredentialForms|ResponseDispatch|ErrorReporting)$'` locally. HTMX tests download the version pinned in the site layout and check its integrity hash. Ordinary Go test runs skip browser tests.

CI saves browser diagnostics for seven days, including readiness timing, [Firefox process-launch logs](https://firefox-source-docs.mozilla.org/xpcom/logging.html), and available crash reports. On failure, it captures live process state and probes the test server before stopping Firefox. Whole browser profiles are not uploaded. Reports can contain synthetic test data. Set `HS_TEST_BROWSER_ARTIFACTS` to a directory to retain diagnostics locally.

To investigate intermittent startup failures, manually run the CI workflow with `browser_diagnostics` enabled. It repeats the standalone `TestBrowserErrorReporting` test ten times with fresh profiles. Any failed attempt keeps the run failed. Startup and check deadlines remain 30 and 15 seconds.

## Feedback

Feedback on the design, code, and developer experience is welcome. Email me at trentnix at gmail.com.

Send security reports privately to the same address. Do not include credentials, personal data, or exploit details in public issues.
