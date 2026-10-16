# 0001: Self-registration and application-owned runtime state

Status: Accepted.

## Decision

Package `init()` functions register immutable descriptors and factories, not live modules. Each application selects a catalog, creates its own instances, initializes dependencies before consumers, and binds routes before serving. Tests can supply local catalogs.

## Reason

A process-wide registry of live objects lets one application's initialization overwrite another application's configuration and services. Sharing descriptors preserves import-based discovery without sharing runtime state.

## Consequences

Factories must not perform I/O. Modules own resources acquired during initialization and release them during reverse-order cleanup. A failed initializer releases its partial resources. The initializer rolls back previously completed modules on failure.

Application-owned services use typed constructors. Module-supplied capabilities are checked at startup, not compile time. See the [setup guide](../application.md#module-dependencies) for dependency declarations and lifecycle rules.

Registration does not provide runtime plugin replacement or isolation from untrusted code. Those features require a separate design.
