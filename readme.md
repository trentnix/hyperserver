# HyperServer

HyperServer is an experimental Go framework for server-rendered applications with HTMX. It keeps ordinary Go handlers and HTML templates visible while handling service startup, middleware, rendering, and shutdown.

The core provides rendering and HTMX responses. Authentication, sessions, persistence, serialization, and mail use replaceable services. Public APIs can change during `v0.x`. HyperServer is **not production-ready**. See the [compatibility policy](docs/compatibility.md).

![Hyper Gopher](hypergo.png)

## Run an example

Use the Go version in [go.mod](go.mod). SQLite and race tests require CGO and a C compiler.

- [Blog](examples/blog/README.md): a complete application with posts, pages, images, search, and administrator-only publishing. Start here to see how an application uses the framework.
- [Reference site](cmd/web): the current default application, with contact forms and development routes. It is restricted to an explicit loopback IP and must not be publicly deployed, including through a tunnel or proxy.

To run the reference site, copy [config-template.yaml](config-template.yaml) to `config.yaml` if that file does not already exist. Configure the enabled services and replace signing-key placeholders, then run:

```sh
go run ./cmd/web
```

The blog has its own configuration and Go module. Follow its [setup instructions](examples/blog/README.md#run-locally). Do not load [modules/site](modules/site) in a production application.

## Build an application

Use `application.New` to open configured services and initialize the modules selected by `application.Options.Modules`. Configure shared rendering in that callback, before modules prepare their templates. Serve `app.Handler` through `app.Run` so shutdown drains requests before closing resources.

Imports make module factories available. They do not select or initialize modules. Factories must return fresh instances without I/O. Modules own their settings, routes, templates, and feature-specific storage.

Start with [application setup](docs/application.md) and the blog's [application.go](examples/blog/application.go). [AGENTS.md](AGENTS.md#building-an-application-with-hyperserver) gives coding agents the same starting points and constraints.

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
| [Local deployment](docs/deployment.md) | The disposable reference-site container rehearsal. |
| [Roadmap](roadmap.md) | Architectural direction and remaining work. |

## Configuration

The loader reads the first `config.yaml` in `.`, `config`, `../config`, or `../../config`. File values override defaults. `HYPERSERVER_` environment values override the file. Invalid or unknown settings stop startup.

Optional module settings live in `modules/<registered-name>/config.yaml` under the application's resource directory. Modules validate their own requirements. Runtime files stay outside source control. See [configuration](docs/configuration.md) and [module configuration](docs/application.md#optional-module-configuration).

## Account storage

`application.New` selects account storage through `auth.accountStorage`. SQLite is the default provider. Other providers must preserve uniqueness, concurrency, expiration, and atomic security operations, not just implement the methods.

Fresh stores initialize automatically. Existing framework account and session stores require supported schema versions and explicit migrations. See [storage setup and maintenance](docs/storage.md). The blog's content schema has a separate, experimental upgrade policy.

## Checks

Run the root and blog modules separately:

```sh
go test ./...
go vet ./...
(cd examples/blog && GOWORK=off go test ./... && GOWORK=off go vet ./...)
```

See [testing](docs/testing.md) for race, browser, and migration checks. [Performance measurements](docs/performance.md) describe reference-site workloads, not production capacity.

## Feedback and license

Email design feedback to [trentnix@gmail.com](mailto:trentnix@gmail.com). Report vulnerabilities through [SECURITY.md](SECURITY.md).

HyperServer uses the [MIT License](LICENSE), copyright Trent Nix. Third-party components retain their own licenses.
