/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_ADMIN_BASE_URL?: string
  readonly VITE_ENV_LABEL?: string
  readonly VITE_DATA_MODE?: 'live' | 'mock'
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
