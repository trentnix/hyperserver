# HyperServer repository guidance

## Project intent

HyperServer is an experimental Go framework for server-rendered web applications with first-class HTMX support. High performance, low overhead, and readable code are design goals.

Use [roadmap.md](roadmap.md) for architectural direction and project priorities. The roadmap describes intended capabilities, not necessarily implemented behavior. Check the code before claiming a capability exists.

Treat public interfaces as unstable during the `v0` phase. Change an interface when real usage demonstrates a better boundary.

## Building an application with HyperServer

Use [docs/application.md](docs/application.md) for the supported setup path. The default [blog application](cmd/web/application.go) demonstrates it with real content and storage. The [reference site](cmd/reference) demonstrates contact forms, health checks, and development routes. Do not copy its diagnostic routes into another application.

Before writing setup code, inspect `pkg/application.Options` and the relevant service contracts. Prefer the configured constructor over assembling accounts, sessions, mail, and middleware by hand. Change framework code only when the requested task requires it.

1. Create an application-owned `config-template.yaml` and ignore runtime `config.yaml`, secrets, databases, and uploads. Prefer file configuration over new command-line flags. Configure only the services the application needs.
2. Load configuration with `config.GetConfig()`. Call `application.New(ctx, cfg, options)` and select descriptors in `Options.Modules`. Imports register available factories, not active modules. A nil module callback selects nothing.
3. Configure shared layouts and `content.ConfigureResponses` in the module callback before templates are prepared. Keep display content out of operational configuration. The application decides which modules load. Modules validate their own required settings.
4. Implement `handlers.Handler`: `Init(context.Context, *server.ApplicationServer) error` and `Routes(*routing.Routes) error`. Use a fresh instance per descriptor factory. Use `init()` only to register immutable metadata. Open resources in `Init`, not in factories or package initialization.
5. If a module needs settings, implement `Configuration() any` with a pointer to its settings. Supply defaults in its factory and validate in `Init`. The framework reads optional `modules/<registered-name>/config.yaml` under the application resource directory. Do not read configuration from an installed Go package directory.
6. Prepare embedded templates once with `content.PrepareView` or `content.PrepareTemplates`. Create request-local content through `view.Content(r, data)`. Put request titles and data in handlers. Use named partials for full-page and HTMX rendering. See [rendering and forms](pkg/components/readme.md).
7. Register method-qualified routes and return `routes.Err()`. Set explicit upload body limits. Validate requests and authorization before changing data. Preserve submitted non-secret values on validation errors. Use ordinary form actions where operation without JavaScript is required.
8. Keep storage rules with the feature. Use narrow repositories where substitution or testing benefits the application. Do not create a capability provider, generic repository, or separate service layer without a concrete need. Application-supplied resources keep their owner. Modules close only resources they acquire.
9. Serve `app.Handler` with `server.NewHTTPServer` and `app.Run`. Do not install the standard middleware twice or call `handlers.Setup` after `application.New`. Use `app.OnShutdown` for callback-owned resources and module `Shutdown(context.Context) error` for module-owned resources.
10. Test a fresh installation, full-page and HTMX success/failure paths, authorization, and shutdown. Run checks from the repository root. The framework, blog, and reference site share one Go module.

Authentication requires selecting `auth.Module` and enabling authentication in configuration. The constructor supplies configured services and the default email provider. Use `Options.AuthProviders` only when the application needs a different authentication policy. Business permissions remain application or module rules, not core framework policy.

For an application in its own repository, pin the framework revision and give that application its own configuration and tests.

## Architecture

Keep the core small. Rendering and HTMX response handling belong in the core. Authentication, sessions, persistence, caching, and mail use replaceable service contracts.

Activate a service only when a loaded module or application configuration requires it. The core must run without database, authentication, session, or mail services.

Preserve self-registering modules. Package `init()` functions must register immutable metadata only. Keep the process-wide module catalog separate from per-application module instances. Tests must be able to supply a local catalog.

Keep dependency resolution, initialization, route binding, and shutdown explicit and deterministic. Reject unresolved dependencies, ambiguous providers, dependency cycles, and route conflicts before accepting traffic.

Keep HTTP handling, application operations, persistence, serialization, and rendering responsibilities distinct. These boundaries define ownership, not a required package structure for applications.

Define narrow repository interfaces near their consumers. Do not introduce a universal database or query API. Providers must satisfy the guarantees their consumers require.

Let the application operation decide which changes must succeed together. Storage providers must bind participating repository operations to the same transaction scope.

Keep storage independent from serialization. Services that store encoded values must allow the serializer to be replaced.

Let cache consumers choose keys, freshness, invalidation, and failure behavior. Cache implementations must enforce expiration and resource bounds and handle concurrent access.

Keep `net/http` types and standard Go composition available. Do not hide them behind a mandatory proprietary request model.

Support ordinary navigation and form submissions when an application requires them. Applications can depend on HTMX. Keep the reference application's progressive-enhancement example working.

## Dependencies

Prefer the standard library when it meets requirements. A smaller feature set is acceptable if it keeps the code easier to read and maintain.

Before adding a dependency, assess maintenance, transitive dependencies, and replacement cost. Use established, maintained implementations for cryptography and password hashing.

Add abstractions and infrastructure for demonstrated needs. Follow the roadmap's scope limits rather than introducing speculative adapters or services.

## Implementation

Use blank lines to separate logical phases within a function, not every statement or call. Keep an operation and its error check together. Group closely related operations, such as creating a token and building its URL. Add space when the responsibility changes, such as moving from rendering to persistence and delivery.

Keep short expressions on one line. Split long call chains across lines when the steps become easier to scan. Do not add blank lines merely because an `if` block ends.

Do not add package-global mutable runtime state. Limit shared registration state to the module catalog.

Return errors from initialization. Do not introduce new startup panics. Clean up partially acquired resources and close initialized modules in reverse dependency order. During shutdown, drain HTTP requests before closing services.

Use context-aware operations for I/O and propagate `context.Context`.

Use method-qualified route patterns for state-changing handlers.

Use `html/template` and escape rendered values by default. Restrict `template.HTML` to reviewed trust boundaries. Reuse parsed templates rather than parsing them on each request.

Keep diagnostic and sample routes in development modules. Production applications must not load those modules. Do not add an application-wide development flag to control their routes.

Report service failures accurately. Do not return success when a required operation was skipped or failed.

## Verification

Format changed Go files with `gofmt`, then run:

- `go test ./...`
- `go vet ./...`

If a change affects concurrent code, run race tests for the affected packages and relevant integration flows:

- `go test -race ./path/to/package`

Add regression tests for every corrected defect.

Test failure paths for changed services, startup, and shutdown. Use HTTP integration tests for authentication and response behavior, shared contract tests for storage providers, and migration tests for supported stored formats.

Do not claim a performance improvement without a repeatable benchmark or profile. Record the workload and environment.

For documentation-only changes, check the edited content and links. Go tests are not required.

Report which checks ran, their results, and any checks that were skipped or blocked.
