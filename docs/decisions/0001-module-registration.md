# 0001: Self-registration and application-owned runtime state

Status: Accepted. The runtime refactor is not implemented.

## Context

Importing a module must make it available to HyperServer. Today, the [handler registry](../../pkg/handlers/handlers.go) and [auth registry](../../auth/auth_service.go) store live module objects that every server instance in the same Go process shares.

When server A initializes a module, the module stores A's database, configuration, and other dependencies. If server B initializes the same module object, B can overwrite those dependencies while A's handlers still use the object. Separate processes do not share these registries.

Imports must instead register descriptions and factories. Each server then creates and initializes its own module objects.

## Decision

Package `init()` functions must register immutable module descriptors, not runtime instances. Registration must not load configuration, perform I/O, bind routes, migrate storage, or start background work.

Each application snapshots the process-wide module catalog and owns its enabled instances. Startup explicitly resolves dependencies, initializes modules, and binds routes before accepting traffic. Shutdown drains requests before closing dependent services in reverse order. Tests can supply local catalogs.

## Consequences

Importing a module still makes it available to every application in the process. Applications share descriptions of available modules, not the module objects themselves. Each application creates, initializes, and cleans up its own objects.

We still need to decide what a module description contains and how an application passes dependencies to a module. Those choices are listed under [unresolved contracts](readme.md#unresolved-contracts). Modules must only receive the dependencies they declare. Applications do not need a separate Go package for each responsibility.

Future plugin loaders can register module descriptions and factories through the same registration API that imports use. Registration must therefore be callable without relying on `init()`. We have not chosen how to load plugin code or check whether a plugin works with a particular HyperServer version.

For now, each application selects its modules at startup. Adding or replacing a plugin while the server is running needs more design work. We must decide how to change routes and dependencies, finish requests already using the old plugin, and release its resources.

Giving each application its own module objects prevents accidental sharing. It does not make untrusted plugin code safe to run inside the server process.
