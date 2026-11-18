# Compatibility

HyperServer is experimental. Public APIs, configuration, HTTP behavior, and stored formats can change during `v0`, even between commits. Applications must pin a revision and test upgrades. Packages under `internal` and blog internals are not reusable framework contracts.

Breaking changes must document the affected contract and application changes. Release notes must link to upgrade instructions. If a breaking change retains an old Go API, its documentation must include a `Deprecated:` comment naming the replacement. No deprecation period is guaranteed during `v0`.

Storage changes must specify migration, invalidation of disposable data, or recreation of development storage. Never silently discard durable data. Follow [schema migrations](storage.md#account-schema-migrations), [session format rules](../pkg/services/session/readme.md#replacing-serialization), and [backup compatibility](blog/backup.md#compatibility).

Run [provider contracts](storage.md#testing-a-replacement-provider) and application tests when replacing services. SQLite coverage does not prove another backend's guarantees. A stable release must define its public surface and validate a separately pinned application.

## Account capabilities

Password changes, tokens, resets, and verification use optional interfaces rather than `AccountRepository`. SQLite retains all capabilities without a schema change. See [adapter upgrade instructions](storage.md#account-capabilities-and-adapter-upgrades).

## Rendering API upgrade

Request-time file registration and parsing are removed. Update applications that use these `Content` fields or methods:

| Removed API | Replacement |
| --- | --- |
| `Layouts`, `AddLayout` | Load layouts during preparation. Use `WithLayouts` to select their named roots. |
| `Contents`, `AddContent` | Pass `TemplatePath` values to `PrepareView` or `PrepareTemplates` during initialization. |
| `Components`, `AddComponent` | Collect module contributions or configure manager components before preparation. Select and order named components in request data. |
| `LogMessages`, `AddLogMessage`, `AddLogMessages` | Use the request logger for diagnostics. These values never emitted output. |

Create responses with `view.Content(r, data)`. Direct construction with `NewContent` or `NewManagedContent` must assign a prepared set to `Templates` before rendering a body. Redirects and 204 responses do not require templates. The manager's template registration methods remain available for startup configuration. No stored-data migration is needed.

`Refresh: true` still renders a body and requires prepared templates. To request an HTMX refresh without rendering, also set `NoContent: true`. Ordinary and history-restoration requests ignore both HTMX settings and still require templates.
