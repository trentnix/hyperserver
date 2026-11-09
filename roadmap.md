# HyperServer roadmap

HyperServer aims to provide common web infrastructure through Go handlers, HTML templates, and HTMX. It remains experimental and is not production-ready. This document describes direction, not a list of implemented capabilities. Use the [README](readme.md) and [application guide](docs/application.md) for current behavior.

## Design direction

- Keep `net/http` and standard Go composition available.
- Keep the core usable without authentication, sessions, databases, or mail.
- Let applications select modules, providers, rendering policy, and resource ownership explicitly.
- Keep storage and business rules with their consumers. Add shared abstractions only for demonstrated needs.
- Support ordinary forms where an application needs them. Applications can depend on HTMX.
- Prefer safe defaults, accurate errors, and measured performance over a larger feature set.

## Priorities

### Validate deployment and recovery

Use the [local rehearsal](docs/deployment.md) for isolated deployment tests. On the target host, validate HTTPS, secrets, persistent storage, restart behavior, and recovery. Validate mail only when enabled features need it. Measure mixed read, write, and search workloads using the [performance checks](docs/performance.md), including cold caches and content that exceeds cache capacity.

Choose backup retention and off-host storage. Verify recovered accounts, sessions, content, and images before switching traffic. Obtain an independent security review before relying on public account and recovery flows. External scheduling is sufficient for backups and expired-record cleanup.

### Prove framework reuse

Build a small application outside this repository against a pinned framework revision. It must demonstrate persistent content, full-page and HTMX success and failure paths, configuration, and resource cleanup without copying the blog's setup internals.

Use that application to test provider and serializer replacement. Add another storage backend only when an application needs it. Run shared contract tests and document backend-specific guarantees. Fix coupling demonstrated by real usage before stabilizing public APIs.

### Extend the blog deliberately

The blog supports Admin and Poster roles, invitations, post ownership, and account-aware login choices. Keep those business rules in the application. Before adding new content sources or components, reuse the writing, revision, and image-access rules instead of creating separate save paths.

## Scope limits

Defer a universal database API, broad ORM features, distributed caches, a job system, and scaffolding until real requirements justify them.

Import-based module discovery is not runtime plugin loading or a boundary for untrusted code. Runtime replacement would require a separate lifecycle and isolation design.

If mail delivery must survive restarts, design durable storage and retries around the application's transaction boundaries. Do not promise exactly-once delivery.
