# Architecture decisions

The [roadmap](../../roadmap.md) defines project direction and priorities. These records explain concrete choices and their tradeoffs. An accepted decision does not mean its implementation is complete.

## Accepted decisions

- [0001: Self-registration and application-owned runtime state](0001-module-registration.md) — module discovery, dependency resolution, and runtime ownership.

## Upcoming decisions

Use the roadmap's [remaining priorities](../../roadmap.md#remaining-priorities) to identify decisions as the work reaches them. Storage guarantees, serializer compatibility, and stored-format migration are the next focus. Keep implementation status and task ordering out of this index.

## Recording a decision

When implementation requires one of these choices, add a numbered Markdown record with its status, context, decision, and consequences. Record the choice actually made, not an untested API design. Include implementation status and link to relevant code when available.

Add accepted records to this index. If a later decision replaces an accepted one, mark the earlier record superseded and link to its replacement. Keep roadmap priorities in the roadmap.
