# Requirements

StewardMesh is requirements-driven. Every user-facing capability, API, schema, migration, security control, accessibility behavior, template, alert, notification, and test must reference a requirement or GitHub issue.

Requirement IDs use stable prefixes:

- `REQ-` product behavior
- `SEC-` security and privacy
- `A11Y-` accessibility
- `DOC-` documentation and feedback
- `OPS-` operations and delivery

New requirements must include acceptance criteria and links to related feature dictionary entries.

Traceability entries may declare `deliveryStatus` as `planned`, `foundation`, or `complete`. Planned entries require documentation; foundation entries require documentation, code, schema, and tests; complete entries require those artifacts plus transport API and UI evidence. Omitting the field means complete. This keeps early delivery honest without using placeholder API or UI paths.
