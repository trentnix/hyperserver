# Compatibility

HyperServer is in `v0` development. No public framework interface is frozen. Applications must pin a version or commit and test upgrades. A documented extension point is supported at that revision, not a promise that its shape will stay unchanged.

## What can change

Public APIs, configuration, HTTP behavior, and stored formats can change during `v0`, including between commits. A breaking change must document the affected contract and the required application changes in the same change. Published release notes must identify those changes and link to upgrade instructions.

If an old Go API is retained during a transition, its documentation must include a `Deprecated:` comment naming the replacement. There is no guaranteed deprecation period during `v0`. An API can be removed directly when maintaining both versions would add unnecessary complexity. Security fixes do not wait for a deprecation period.

Storage compatibility is separate from source compatibility. A format change must state whether to migrate, invalidate disposable data, or recreate example storage. It must not silently discard durable application data. Follow the provider's [migration procedures](storage.md) and [session format rules](../pkg/services/session/readme.md#replacing-serialization).

## Stabilization candidates

Module registration, lifecycle hooks, and `MailSender` are candidates for stabilization because the external application uses them without reference-site wiring. They remain experimental until further application use confirms their shape.

Keep account and session provider interfaces experimental while testing another backend. SQLite coverage alone does not establish that their contracts fit PostgreSQL or other storage platforms. The notes repository is application-owned and does not need to become a framework API.

A future stable release must identify its supported public surface and compatibility policy explicitly. Example internals and packages under `internal` are not reusable framework contracts.
