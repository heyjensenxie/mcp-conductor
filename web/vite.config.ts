import { execSync } from 'node:child_process'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import { AntDesignVueResolver } from 'unplugin-vue-components/resolvers'

// 构建信息：把构建时的 Git 短提交与时间注入为编译期常量，供「系统信息」只读展示。
// 无 .git / git 不可用时兜底为 "dev"，避免在无仓库环境下构建失败。
function appCommit(): string {
  try {
    const out = execSync('git rev-parse --short HEAD', { stdio: ['ignore', 'pipe', 'ignore'] }).toString().trim()
    return out || 'dev'
  } catch {
    return 'dev'
  }
}
function appBuildTime(): string {
  const d = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// 开发环境把 /api 与 /mcp 代理到本地后端，前端源码不含后端地址。
export default defineConfig({
  define: {
    __APP_COMMIT__: JSON.stringify(appCommit()),
    __APP_BUILD_TIME__: JSON.stringify(appBuildTime()),
  },
  plugins: [
    vue(),
    // Ant Design Vue 按需引入。v4 为 css-in-js，组件运行时自动注入样式，
    // 因此关闭 resolver 的静态样式导入（importStyle:false），只做组件按需。
    Components({ resolvers: [AntDesignVueResolver({ importStyle: false })] }),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:18110', changeOrigin: true },
      '/mcp': { target: 'http://localhost:18110', changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    chunkSizeWarningLimit: 900,
    rollupOptions: {
      output: {
        // 基础框架单独打包，利于浏览器长期缓存（不把 echarts 全量打入）。
        manualChunks: {
          'vendor-vue': ['vue', 'vue-router', 'pinia', 'vue-i18n'],
        },
      },
    },
  },
})