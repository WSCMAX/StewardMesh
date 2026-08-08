# Initial Requirements

| ID | Requirement | Acceptance criteria |
|---|---|---|
| REQ-ATLAS-001 | Users can register and retrieve assets. | Valid asset records can be created and listed through REST and repository interfaces. |
| REQ-PEOPLE-001 | Assets can be organized by users and departments. | The domain model supports user and department references without requiring them for every asset. |
| REQ-THREADS-001 | Strategic tags and goals can be attached to records. | Tags and goals have stable IDs and support parent-child relationships. |
| REQ-STORAGE-001 | File storage is abstracted from the application. | Local storage is available now and S3-compatible storage can implement the same interface. |
| REQ-API-001 | REST and gRPC contracts are documented. | OpenAPI and protobuf definitions are checked into the repository. |
| SEC-HTTP-001 | The HTTP service uses secure defaults. | Timeouts, body limits, security headers, and input validation are covered by tests or configuration. |
| A11Y-001 | The interface meets WCAG 2.2 AA at minimum. | Keyboard focus, semantic headings, reduced motion, and accessible status messaging are implemented and tested. |
| DOC-001 | Feature behavior is discoverable. | Branded names always appear with plain-language descriptors and documentation links. |
| DOC-002 | Users can report issues from the application. | The issue destination is configurable and defaults to the public StewardMesh issues page. |
