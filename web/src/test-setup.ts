import '@testing-library/jest-dom/vitest'

// Requirements: REQ-WORKSPACE-001, A11Y-001. Feature: experience.workspace.

// jsdom intentionally omits rendering APIs. axe-core probes canvas support
// during color checks, and DocumentationSite scrolls only as presentation;
// stable no-op shims keep the test signal focused on application behavior.
Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
  configurable: true,
  value: () => null,
})
Object.defineProperty(window, 'scrollTo', {
  configurable: true,
  value: () => undefined,
})
