# HyperServer roadmap

HyperServer aims to provide common web infrastructure through Go handlers, HTML templates, and HTMX. It remains experimental and is not production-ready. This roadmap records direction and remaining work. Use the [README](readme.md) and [application guide](docs/application.md) for implemented behavior.

## Design direction

- Keep `net/http` and standard Go composition available.
- Keep the core usable without authentication, sessions, storage, or mail.
- Let applications select modules, providers, rendering policy, and resource ownership.
- Keep storage and business rules with their consumers. Add abstractions only for demonstrated needs.
- Support ordinary forms where an application needs them. Applications can depend on HTMX.
- Prefer safe defaults, accurate errors, and measured performance over more features.

## Deployment and recovery

Use the [local rehearsal](docs/deployment.md) for isolated tests. On the target host, validate HTTPS, secrets, persistent storage, service restarts, and startup after a reboot. Validate mail only when enabled features need it.

Schedule backups, choose retention and off-host storage, and rehearse an isolated restore. Verify recovered accounts, sessions, content, and uploads before switching traffic. Obtain an independent security review of authentication, authorization, sessions, recovery, uploads, and backups before relying on public account flows.

Measure mixed reads, writes, and search on the target host with cold and warm caches and content larger than cache capacity. See [performance checks](docs/performance.md). External scheduling is sufficient for backups and expired-record cleanup.

## Framework reuse

Build a persistent application outside this repository against a pinned framework revision. It must cover full-page and HTMX success and failure paths, configuration, and resource cleanup without copying blog setup internals. A stateless consumer alone does not establish this coverage.

Use the application to test provider and serializer replacement. Add another storage backend only when an application needs one. Run shared contract tests and document backend guarantees. Resolve coupling demonstrated by actual use before stabilizing public APIs or promoting the framework for external adoption.

## Blog ideas

- Publish from Notion.
- Evaluate Markdown editors.
- Explore hosting multiple blogs in one server.

New content components must reuse writing, revision, media-access, and save paths. Admin and Poster permissions remain blog rules, not framework policy.

## Scope limits

Defer a universal database API, broad ORM features, distributed caches, a job system, and scaffolding until requirements justify them. Import-based module discovery is not runtime plugin loading or isolation for untrusted code.

If mail must survive restarts, design durable storage and retries around application transactions. Do not promise exactly-once delivery.
