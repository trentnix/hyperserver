# Compatibility

HyperServer is experimental. Public APIs, configuration, HTTP behavior, and stored formats can change during `v0`, including between commits. Applications must pin a version or commit and test upgrades.

A breaking change must document the affected contract and required application changes. Published release notes must link to upgrade instructions. If an old Go API is retained during a transition, its documentation must include a `Deprecated:` comment naming the replacement. There is no guaranteed deprecation period during `v0`.

Storage compatibility is separate from source compatibility. Format changes must specify migration, invalidation of disposable data, or recreation of development storage. They must not silently discard durable data. Follow the [storage migration procedures](storage.md), [session format rules](../pkg/services/session/readme.md#replacing-serialization), and [backup compatibility requirements](blog/backup.md#compatibility).

Documented extension points are supported at the pinned revision, not frozen interfaces. Run the shared provider contracts and application tests when replacing a service. SQLite coverage alone does not establish compatibility with another backend.

Account adapters must follow the [account capability upgrade instructions](storage.md#account-capabilities-and-adapter-upgrades). Password changes, tokens, password resets, and email verification are separate from `AccountRepository`. SQLite retains every capability and needs no migration for this interface change.

Rendering requires templates prepared during initialization. The request-time file fields, registration methods, and unused `LogMessages` helpers on `Content` are removed. Follow the [rendering API upgrade instructions](../pkg/components/readme.md#rendering-api-upgrade). Modules can still contribute components for request-time composition. `Templates.WithLayouts` selects prepared layouts without rereading files. Manager-level template configuration remains available, and no database change is required.

Packages under `internal` and blog implementation details are not reusable framework contracts. A future stable release must identify its supported public interfaces and compatibility guarantees. Validate a separately pinned application before promising external framework stability.
