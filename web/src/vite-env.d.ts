/// <reference types="vite/client" />

// Requirements: A11Y-001, DOC-001, DOC-002. Feature: experience.help.
interface ImportMetaEnv {
  readonly VITE_APP_VERSION?: string
  readonly VITE_ISSUES_URL?: string
  readonly VITE_STEWARD_DARK_CANVAS?: string
  readonly VITE_STEWARD_DARK_SURFACE?: string
  readonly VITE_STEWARD_LIGHT_CANVAS?: string
  readonly VITE_STEWARD_TEXT_ON_DARK?: string
  readonly VITE_STEWARD_TEXT_ON_LIGHT?: string
  readonly VITE_STEWARD_PRIMARY?: string
  readonly VITE_STEWARD_SUCCESS?: string
  readonly VITE_STEWARD_WARNING?: string
  readonly VITE_STEWARD_DANGER?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
