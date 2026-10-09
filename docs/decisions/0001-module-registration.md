# 0001: Self-registration and application-owned runtime state

Status: Accepted. Factory catalogs, per-application instances, and capability ordering are implemented. Runtime dependency delivery and module cleanup remain pending.

## Context

Importing a module must make it available to HyperServer. Storing live objects in a process-wide registry makes every server instance share those objects.

When server A initializes a module, the module stores A's database, configuration, and other dependencies. If server B initializes the same module object, B can overwrite those dependencies while A's handlers still use the object. Separate processes do not share these registries.

The [handler catalog](../../pkg/handlers/handlers.go) and [auth catalog](../../auth/auth_service.go) therefore store descriptions and factories. Each server creates and initializes its own module objects.

## Decision

Package `init()` functions must register immutable module descriptors, not runtime instances. Registration must not load configuration, perform I/O, bind routes, migrate storage, or start background work.

Each application snapshots the process-wide module catalog and owns its enabled instances. Startup explicitly resolves dependencies, initializes modules, and binds routes before accepting traffic. Shutdown drains requests before closing dependent services in reverse order. Tests can supply local catalogs.

## Consequences

Importing a module still makes it available to every application in the process. Applications share descriptions of available modules, not the module objects themselves. Each application creates, initializes, and cleans up its own objects.

Descriptions contain a name, a factory that creates an instance without I/O, and required and provided capabilities. Applications can copy the import catalog or supply a local catalog. Metadata slices are copied so applications cannot change each other's descriptions. Auth registries instantiate only configured, enabled providers.

`handlers.Resolve` validates module metadata and provider selections before factories run. A required capability needs one provider unless the consumer permits several. Optional requirements permit no provider but do not hide ambiguous selections. Explicit selections narrow matching providers to one. The resolver follows catalog and requirement order, orders dependencies first, and rejects cycles. Every module in the supplied catalog stays selected. Provider selection changes dependency edges, not which modules run.

The resolver returns ordered descriptions, not runtime service objects. Dependency delivery remains an [unresolved contract](readme.md#unresolved-contracts). Modules must eventually receive only the dependencies they declare.

Future plugin loaders can register module descriptions and factories through the same registration API that imports use. Registration must therefore be callable without relying on `init()`. We have not chosen how to load plugin code or check whether a plugin works with a particular HyperServer version.

For now, each application selects its modules at startup. Adding or replacing a plugin while the server is running needs more design work. We must decide how to change routes and dependencies, finish requests already using the old plugin, and release its resources.

Giving each application its own module objects prevents accidental sharing. It does not make untrusted plugin code safe to run inside the server process.
