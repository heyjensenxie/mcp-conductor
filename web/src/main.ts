import { createApp } from 'vue'
import { createPinia } from 'pinia'
import 'ant-design-vue/dist/reset.css'

import App from './App.vue'
import router from './router'
import i18n from './i18n'
import { initializeTheme } from './theme'

initializeTheme()

// 组件由 unplugin-vue-components 按需引入；ant-design-vue v4 通过 css-in-js
// 运行时注入样式（无需按组件静态引入 css）。
const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(i18n)
app.mount('#app')
