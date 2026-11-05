# HyperServer repository guidance

## Project intent

HyperServer is an experimental Go framework for server-rendered applications with first-class HTMX support. High performance, low overhead, and readable code are design goals. Public interfaces are unstable during `v0`. Change an interface when real usage demonstrates a better boundary.

Use [roadmap.md](roadmap.md) for direction, not proof of implemented behavior. Check the code before claiming a capability exists.

## Building an application with HyperServer

Follow [application setup](docs/application.md) and the [blog application](cmd/web/application.go). Inspect `pkg/application.Options` and relevant service contracts before writing setup code. Prefer `application.New` over assembling services and middleware by hand. Change framework code only when the task requires it.

- Keep configuration application-owned. Ignore runtime configuration, secrets, databases, and uploads. Prefer file settings over new flags. Keep display content out of operational configuration.
- Load configuration with `config.GetConfig()`. Select descriptors through `Options.Modules`. Imports register available factories, not active modules. A nil module callback selects nothing.
- Configure layouts and `content.ConfigureResponses` in the callback before modules prepare templates. Use [prepared views and named partials](pkg/components/readme.md) for full-page and HTMX responses.
- Implement `handlers.Handler`. Factories return fresh instances without I/O. Modules validate settings, acquire resources, and prepare templates in `Init`. Optional module configuration comes from the application's resource directory, not an installed Go package.
- Register method-qualified routes and return `routes.Err()`. Site modules own root paths, including `/login` and `/register`. Use the auth manager's `PageHandlers` and its protected `Submissions` scope as shown in the application guide.
- Serve `app.Handler` with `server.NewHTTPServer` and `app.Run`. Do not install standard middleware twice or repeat `handlers.Setup`. Register callback-owned resources with `app.OnShutdown`. Modules close resources they acquire through `Shutdown(context.Context) error`.

Authentication requires selecting `auth.Module` and enabling it in configuration. Configuration selects providers. Use `Options.AuthPolicy` for admission and login behavior, and `Options.AuthProviders` for an explicit local provider catalog. Business permissions remain application rules.

The framework and blog share one Go module. Framework HTTP examples are test-only fixtures under [internal/test/web](internal/test/web). Do not expose fixture routes in an application. Applications in other repositories must pin the framework revision and own their configuration and tests.

## Architecture and ownership

Keep the core small and usable without database, authentication, session, or mail services. Activate optional services only when selected modules or application configuration need them. Preserve `net/http` types and standard Go composition.

Package `init()` functions must register immutable metadata only. Keep the process-wide catalog separate from per-application instances. Tests must be able to supply a local catalog. Do not add package-global mutable runtime state.

Keep dependency resolution, initialization, route binding, and shutdown deterministic. Reject missing or ambiguous providers, dependency cycles, and route conflicts before accepting traffic.

Keep HTTP handling, application operations, persistence, serialization, and rendering responsibilities distinct. These boundaries define ownership, not a required package structure. Define narrow repositories near consumers. Do not introduce a universal database API or a service layer without a concrete need.

Let the application operation define transaction boundaries. Providers must bind participating operations to the same transaction and preserve their consumers' guarantees. Keep encoded storage independent of serialization so serializers can be replaced.

Let cache consumers choose keys, freshness, invalidation, and failure behavior. Cache implementations must enforce expiration and resource bounds and handle concurrent access.

Application-supplied resources keep their owner. Return initialization errors, not startup panics. Clean up partial acquisitions and close initialized modules in reverse dependency order. Drain HTTP requests before closing services.

Support ordinary navigation and form submissions when required. Applications can depend on HTMX. Preserve ordinary-form and HTMX coverage in framework fixtures.

## Implementation

Prefer the standard library when it meets requirements. Before adding a dependency, assess maintenance, transitive dependencies, and replacement cost. Use maintained implementations for cryptography and password hashing. Add abstractions only for demonstrated needs.

Use blank lines between logical phases, not every statement or completed `if` block. Keep an operation and its error check together. Keep short expressions on one line. Split long call chains when that improves readability.

Propagate `context.Context` through I/O. Set explicit upload body limits. Validate requests and authorization before changing data. Preserve submitted non-secret values on validation errors. Report failures accurately, including required operations that were skipped.

Use `html/template` and escape values by default. Restrict `template.HTML` to reviewed trust boundaries. Reuse parsed templates rather than parsing on each request.

Keep diagnostics and sample routes in fixtures or development modules that production applications do not load. Do not introduce an application-wide development flag to control those routes.

## Verification

Format changed Go files with `gofmt`, then run:

- `go test ./...`
- `go vet ./...`

For concurrent code, also run `go test -race ./path/to/package` for affected packages and relevant integration flows.

Add regression tests for corrected defects. Test fresh installation, full-page and HTMX success and failure paths, authorization, and shutdown. Cover service failures and startup cleanup. Use shared provider contracts and migration tests for supported stored formats.

Do not claim performance improvements without a repeatable benchmark or profile. Record the workload and environment. See [testing](docs/testing.md) and [performance checks](docs/performance.md).

For documentation-only changes, check edited content and links. Go tests are not required. Report checks that passed, failed, or were skipped.
