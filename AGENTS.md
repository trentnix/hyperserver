# HyperServer repository guidance

## Project intent

HyperServer is an experimental Go framework for server-rendered web applications with first-class HTMX support. High performance, low overhead, and readable code are design goals.

Use [roadmap.md](roadmap.md) for architectural direction and project priorities. The roadmap describes intended capabilities, not necessarily implemented behavior. Check the code before claiming a capability exists.

Treat public interfaces as unstable during the `v0` phase. Change an interface when real usage demonstrates a better boundary.

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
