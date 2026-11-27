/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_ORIGIN: string;
  readonly VITE_APP_VERSION: string;
  readonly VITE_PUSH_VAPID_PUBLIC_KEY: string;
}
interface ImportMeta { readonly env: ImportMetaEnv }
