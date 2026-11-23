/// <reference types="vite/client" />

// Fontsource packages are CSS-only side-effect imports.
declare module '@fontsource-variable/*'

interface ImportMetaEnv {
  readonly VITE_API_URL?: string
  readonly VITE_JAEGER_URL?: string
  readonly VITE_GRAFANA_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
