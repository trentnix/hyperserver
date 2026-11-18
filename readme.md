# HyperServer

HyperServer is an experimental Go framework for server-rendered applications with HTMX. It uses standard `net/http` handlers and `html/template`, with shared startup, middleware, rendering, and shutdown.

Inspired by [Pagoda](https://github.com/mikestefanello/pagoda), HyperServer is a reusable library rather than a starter repository. It requires no ORM. Applications choose optional authentication, session, storage, and mail services.

Public APIs can change during `v0.x`. HyperServer is **not production-ready**. Pin a revision and test upgrades. See [compatibility](docs/compatibility.md) and [security reporting](SECURITY.md).

![Hyper Gopher](hypergo.png)

## Get started

Use the Go version in [go.mod](go.mod). Stateless applications can build without CGO. SQLite and race tests require CGO and a C compiler. Browser integration targets **HTMX 4**.

- Build an application with the [standalone quickstart](docs/quickstart.md), then read [application setup](docs/application.md) for modules, services, and cleanup.
- Run the included Markdown blog using its [setup guide](docs/blog/readme.md). It supports posts, nested pages, media, search, RSS, and Admin and Poster roles.

## Guides

| Topic | Guide |
| --- | --- |
| Configuration | [Files, environment overrides, signing keys, and email](docs/configuration.md) |
| Rendering | [Prepared views, composition, HTMX, and forms](pkg/components/readme.md) |
| HTTP | [TLS, proxies, middleware, body limits, and rate limits](docs/http.md) |
| Storage | [Providers, contracts, SQLite migrations, and cleanup](docs/storage.md) |
| Sessions | [Reading, saving, rotation, and serialization](pkg/services/session/readme.md) |
| Google login | [Configuration and account linking](docs/google-authentication.md) |
| Images | [Processing example](pkg/imaging/example_test.go) |
| Verification | [Tests](docs/testing.md) and [performance checks](docs/performance.md) |
| Deployment | [Local container rehearsal and recovery](docs/deployment.md) |
| Direction | [Roadmap and remaining work](roadmap.md) |

The framework, blog, and maintenance commands share one Go module. Run `go test ./...` and `go vet ./...` from the repository root. See [testing](docs/testing.md) for race, browser, and migration checks. Coding agents must follow [AGENTS.md](AGENTS.md).

## Feedback and license

Email design feedback to [trentnix@gmail.com](mailto:trentnix@gmail.com). HyperServer uses the [MIT License](LICENSE), copyright Trent Nix. Third-party components retain their own licenses.
