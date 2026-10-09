# Architecture decisions

The [roadmap](../../roadmap.md) defines project direction and priorities. These records explain concrete choices and their tradeoffs. An accepted decision does not mean its implementation is complete.

## Accepted decisions

- [0001: Self-registration and application-owned runtime state](0001-module-registration.md) — factory catalogs, per-application instances, capability ordering, and declared dependency delivery implemented. Automatic module cleanup pending.

## Unresolved contracts

| Area | Decisions still needed | When to resolve |
| --- | --- | --- |
| Modules | Lifecycle error handling and cleanup. | During Phase 2, extending the independent-application tests. |
| Services | Move remaining shared runtime services into application-owned wiring. Derive individual service methods from their consumers. | During Phase 2, or earlier when a security fix needs a contract change. |
| Rendering | Response-result representation, view selection and naming, and how dispatch maps validation errors and HTMX instructions to HTTP responses. | During Phase 3, using the reference form workflow. |
| Storage and serialization | Repository methods and error semantics, how repositories join an application-owned transaction, serializer interfaces, and stored-format versioning. | Establish atomic recovery operations during Phase 1. Complete provider and format contracts during Phase 4. |

## Recording a decision

When implementation requires one of these choices, add a numbered Markdown record with its status, context, decision, and consequences. Record the choice actually made, not an untested API design. Include implementation status and link to relevant code when available.

Update the unresolved list when a choice is made. If a later decision replaces an accepted one, mark the earlier record superseded and link to its replacement. Keep roadmap priorities in the roadmap.
