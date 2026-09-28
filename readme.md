# HyperServer

HyperServer is an experimental Go framework for server-rendered web applications with first-class HTMX support. The goal is high performance and low overhead, with code that stays straightforward to read, extend, and maintain.

## Project status

HyperServer is in early development and is not production-ready. The repository is intended for exploration, local experiments, and discussion as the framework takes shape. Public APIs and configuration can change without backward compatibility during the `v0` phase.

Known security and correctness issues remain in authentication, account recovery, sessions, and request handling. Startup validation, shutdown, and deployment safeguards are incomplete. Do not expose the example application to the public internet or use it with real accounts or sensitive data.

High performance is a design goal, not an established benchmark result. Current tests focus on sessions and do not establish application-wide correctness or security.

![Hyper Gopher](hypergo.png)

## What is here today

- A reference application with server-rendered pages, HTMX fragments, forms, and notifications.
- Import-based module registration with route and initialization hooks.
- Experimental email/password authentication, account verification and recovery, cookie and SQLite session stores, SMTP mail, and request logging.
- SQLite-backed persistence used by the reference application and framework services.

These implementations are a starting point. Services are not yet consistently optional or independently replaceable, and module instances still share process-wide state.

## Direction

- Keep Go's `net/http` model and standard middleware available.
- Prefer the standard library and a small dependency set, even when that means fewer features. Use maintained security implementations rather than custom cryptography.
- Keep rendering and HTMX response handling in the core. Make authentication, sessions, persistence, caching, and mail replaceable services that activate only when needed.
- Preserve self-registering modules while giving each application explicit initialization, dependency resolution, and shutdown.
- Make storage and serialization independently replaceable through contracts based on consumer needs, not a universal database API.
- Support ordinary navigation and form submissions where applications need them. Applications can also depend on HTMX.

The [roadmap](roadmap.md) describes the intended architecture and implementation order. Security and correctness fixes come first, followed by lifecycle work, shared response handling, storage contracts, measured performance work, and deployment validation.

## Exploring the code

Start with [cmd/web](cmd/web) for application composition, [modules/site](modules/site) for the reference application, and [pkg](pkg) for framework components and services. The [configuration template](config/config-template.yaml) describes the current settings but is not a production configuration.

From the repository root, run the existing checks with:

```sh
go test ./...
go vet ./...
go test -race ./pkg/services/session
```

See [AGENTS.md](AGENTS.md) for repository working agreements and verification guidance.

## Feedback

Feedback on the design, code, and developer experience is welcome, especially concrete examples of applications HyperServer should support. Email me at trentnix at gmail.com.

Send security reports privately to the same address. Do not include credentials, personal data, or exploit details in public issues.
