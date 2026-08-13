# Guide — Help, walkthroughs, accessibility, and issue reporting

- **Canonical ID:** `experience.help`
- **Requirements:** `REQ-WORKSPACE-001`, `A11Y-001`, `DOC-001`, `DOC-002`
- **Roadmap issue:** [#15](https://github.com/WSCMAX/StewardMesh/issues/15)

## Purpose

Guide makes StewardMesh behavior discoverable without interrupting work. It combines module-level help, examples, same-host product documentation, role- and permission-aware walkthroughs, public-branding accessibility checks, and a privacy-minimized issue-reporting handoff in one keyboard-operable panel.

Every product name remains paired with a plain-language descriptor. The application header exposes Guide before and after authentication, module cards and implemented workspaces open their own contextual topic, and Guard setup and recovery views retain direct help and reporting access.

## Contextual help

Guide contains topics for Workspace, Atlas, Horizon, Ledger, Threads, Vault, People, Guard, and Guide itself. Each topic provides:

- a plain-language summary and concrete example;
- a direct same-host link to the matching local product documentation;
- an in-page destination when the workspace is implemented and available;
- an accessible control name such as **Open Atlas help**.

Help text and topic selection are public product guidance, not an authorization boundary. The walkthrough omits permission-protected workspaces that the current session cannot read. Guard continues to enforce all permissions on the server.

## Local documentation surface

The web application includes a public, first-party documentation surface at fixed `#docs/{topic}` hashes for Overview, Workspace, Atlas, Horizon, Ledger, Threads, Vault, People, Guard, and Guide. Hash routing keeps documentation on the current host and avoids deployment-specific server rewrite requirements. Guide, Guard setup, and feature-owned fallback help actions link to these pages instead of GitHub.

The documentation layout adapts the repository's licensed Tailwind documentation patterns to the StewardMesh design system: a searchable topic rail, focused article column, desktop table of contents, related-guide cards, previous/next navigation, and responsive single-column behavior. Links back to the live product use fixed `#workspace-{area}` hashes. Documentation is available before authentication and does not request protected APIs.

Runtime product guidance lives in `web/src/documentation.ts` and is intentionally independent from repository Markdown. The Markdown feature, requirement, architecture, and operations documents remain engineering references and traceability artifacts; they are not fetched from GitHub or rendered into the deployed web application. Local documentation contains no GitHub links. The separately configured issue-reporting destination remains the only intentional external handoff and still requires user review.

## Walkthrough behavior

The first authenticated workspace view offers a role-aware tour without opening a modal or blocking other content. The user can start it, skip it, close Guide with the Close button or Escape, finish it, or replay it from the beginning. Completing or skipping stores only `new`, `completed`, or `skipped` in local storage; no identity, role, permission, record, or session data is persisted.

Walkthrough steps are derived from current permission hints and include the signed-in role only in visible explanatory copy. The role is never added to issue context. Close returns keyboard focus to the control that opened Guide.

## Branding accessibility gate

Build-time branding values are optional six-digit hexadecimal colors:

- `VITE_STEWARD_DARK_CANVAS`
- `VITE_STEWARD_DARK_SURFACE`
- `VITE_STEWARD_LIGHT_CANVAS`
- `VITE_STEWARD_TEXT_ON_DARK`
- `VITE_STEWARD_TEXT_ON_LIGHT`
- `VITE_STEWARD_PRIMARY`
- `VITE_STEWARD_SUCCESS`
- `VITE_STEWARD_WARNING`
- `VITE_STEWARD_DANGER`

Before applying them, Guide validates WCAG 2.2 AA contrast for dark and light copy, primary-action copy, the focus indicator, and tinted success, warning, and danger messages. Invalid colors or a critical contrast failure block the requested theme and activate the verified StewardMesh defaults as one atomic fallback. The Accessibility view identifies each pass or blocked result instead of relying on color alone.

Guide also simulates common protanopia, deuteranopia, and tritanopia distinctions between semantic colors and warns when pairs may be difficult to distinguish. These warnings reinforce the interface rule that status, validation, charts, and relationships require text, icons, patterns, or shapes in addition to color.

Brand values are compiled into the public web bundle. They are not secret configuration and must never contain credentials or private organization data.

## Sanitized issue reporting

`VITE_ISSUES_URL` configures the issue destination and defaults to the public StewardMesh repository. A GitHub issues-list URL is converted to its new-issue form. Guide prefills an editable report with only:

- URL pathname without query string or fragment;
- selected StewardMesh component;
- bounded public application version from `VITE_APP_VERSION`;
- coarse browser name and major version;
- bounded viewport dimensions;
- coarse operating-system family;
- the latest valid `X-Correlation-ID`, or `Unavailable`.

The shared same-origin API client captures only correlation IDs that match the bounded allow-list. Guide excludes display names, usernames, email addresses, roles, permissions, record values, search terms, full user-agent strings, request bodies, cookies, CSRF values, tokens, files, and private URLs. The issue opens in a new tab for explicit user review and submission; StewardMesh does not submit it automatically.

Workspace and People use the same report boundary. The active Workspace hash, selected directory records, controlled related-record drafts, and location/person values never enter `collectIssueContext`. The final URL serializer sanitizes every supplied field again, so even a future caller cannot place line breaks or an email address directly into the generated report context. Correlation IDs remain useful for server-side audit lookup without exposing the audit event's private record metadata.

## Interfaces and provider boundary

Guide is a frontend feature and adds no database migration, REST endpoint, or gRPC method. `web/src/guide.ts` owns pure help, contrast, preference, browser classification, and sanitization contracts. `web/src/documentation.ts` owns public documentation routes and product content, `web/src/DocumentationSite.tsx` renders the responsive documentation experience, and `web/src/Root.tsx` selects documentation without mounting authenticated application behavior. `web/src/GuideExperience.tsx` renders contextual help, while `web/src/api.ts` supplies the sanitized response correlation boundary.

## Validation

- pure contrast, fallback, color guidance, sanitization, browser/system, and preference tests;
- component tests for accessibility, focus restoration, permission-aware steps, skip/replay/completion, unsafe-brand warnings, and issue reports;
- authenticated application tests for contextual module help and reporting;
- local-route, search, public-access, same-host link, and documentation accessibility tests;
- automated axe checks and TypeScript compilation;
- keyboard, desktop, and 320-pixel browser validation with clean console output;
- repository race tests, traceability checks, dependency/security scans, production build, and container build gates.
