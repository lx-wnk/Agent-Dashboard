/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_GITHUB_ORG?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
