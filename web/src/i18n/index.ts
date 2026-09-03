import { createI18n } from 'vue-i18n'
import zh from './locales/zh'
import en from './locales/en'

export type AppLocale = 'zh-CN' | 'en-US'

// 默认中文；用户手动切换后记忆到 localStorage，下次进入保持。
const saved = (localStorage.getItem('mc_locale') as AppLocale) || 'zh-CN'

const i18n = createI18n({
  legacy: false,
  locale: saved,
  fallbackLocale: 'zh-CN',
  messages: {
    'zh-CN': zh,
    'en-US': en,
  },
})

// setLocale 切换语言并记忆偏好。
export function setLocale(locale: AppLocale) {
  i18n.global.locale.value = locale
  localStorage.setItem('mc_locale', locale)
}

// currentLocale 读取当前语言，供 Ant Design Vue 组件 locale 联动。
export function currentLocale(): AppLocale {
  return i18n.global.locale.value as AppLocale
}

export default i18n