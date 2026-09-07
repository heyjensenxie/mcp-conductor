/// <reference types="vite/client" />

// 构建期注入的常量（见 vite.config.ts 的 define）。
declare const __APP_COMMIT__: string
declare const __APP_BUILD_TIME__: string

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<{}, {}, any>
  export default component
}