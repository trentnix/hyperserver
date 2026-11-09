# HyperServer

HyperServer is an experimental Go framework for server-rendered applications with HTMX. It uses standard `net/http` handlers and `html/template`, with shared startup, middleware, rendering, and shutdown.

Inspired by [Pagoda](https://github.com/mikestefanello/pagoda), HyperServer is a reusable library rather than a starter repository. It requires no ORM. Authentication, sessions, storage, and mail are optional, replaceable services.

Public APIs can change during `v0.x`. HyperServer is **not production-ready**. See the [compatibility policy](docs/compatibility.md).

![Hyper Gopher](hypergo.png)

## Run the blog

Use the Go version in [go.mod](go.mod). SQLite and race tests require CGO and a C compiler.

The included [blog](docs/blog/readme.md) supports posts, pages, photo albums, search, and Admin and Poster roles. Follow its [setup instructions](docs/blog/readme.md#run-locally), then run from the repository root:

```sh
go run ./cmd/web
```

## Build an application

Use `application.New` to open configured services and initialize the modules selected by `application.Options.Modules`. Configure shared rendering in that callback. Serve `app.Handler` through `app.Run` to drain requests before closing resources. Imports register available module factories. The application selects which modules run.

Start with [application setup](docs/application.md) and the blog's [application.go](cmd/web/application.go). [AGENTS.md](AGENTS.md#building-an-application-with-hyperserver) gives coding agents the same starting points and constraints.

| Guide | Covers |
| --- | --- |
| [Application setup](docs/application.md) | Modules, service selection, dependency injection, and cleanup. |
| [Configuration](docs/configuration.md) | YAML, environment overrides, registration, and mail. |
| [Rendering and forms](pkg/components/readme.md) | Prepared views, full pages, fragments, validation, and notifications. |
| [HTTP behavior](docs/http.md) | Proxies, TLS, request limits, and rate limits. |
| [Storage](docs/storage.md) | Repository contracts, SQLite migrations, and expired-record cleanup. |
| [Sessions](pkg/services/session/readme.md) | Stores, rotation, revocation, and serialization. |
| [Image processing](pkg/imaging/example_test.go) | Upload validation, orientation, resizing, and metadata removal. |
| [Testing](docs/testing.md) | Framework and blog checks, browsers, and diagnostics. |
| [Local deployment](docs/deployment.md) | The disposable blog container rehearsal. |
| [Roadmap](roadmap.md) | Architectural direction and remaining work. |

## Checks

The framework, blog, and maintenance commands share one Go module. From the repository root:

```sh
go test ./...
go vet ./...
```

See [testing](docs/testing.md) for race, browser, and migration checks. [Performance checks](docs/performance.md) cover regression budgets, blog search, and profiling.

## Feedback and license

Email design feedback to [trentnix@gmail.com](mailto:trentnix@gmail.com). Report vulnerabilities through [SECURITY.md](SECURITY.md).

HyperServer uses the [MIT License](LICENSE), copyright Trent Nix. Third-party components retain their own licenses.
