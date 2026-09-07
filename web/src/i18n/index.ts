import { createI18n } from 'vue-i18n'
import dayjs from 'dayjs'
import 'dayjs/locale/zh-cn'
import zh from './locales/zh'
import en from './locales/en'

export type AppLocale = 'zh-CN' | 'en-US'

// dayjs 默认只内置 en，zh-cn 需显式注册。Ant Design Vue 的 DatePicker 日历面板
// 星期/月份/一月起始（周一）文案取自 dayjs locale，必须随应用语言联动，
// 否则中文界面下日期面板仍是英文。
const DAYJS_LOCALES: Record<AppLocale, string> = { 'zh-CN': 'zh-cn', 'en-US': 'en' }

function syncDayjsLocale(locale: AppLocale) {
  dayjs.locale(DAYJS_LOCALES[locale])
}

// 默认中文；用户手动切换后记忆到 localStorage，下次进入保持。
const saved = (localStorage.getItem('mc_locale') as AppLocale) || 'zh-CN'
syncDayjsLocale(saved)

const i18n = createI18n({
  legacy: false,
  locale: saved,
  fallbackLocale: 'zh-CN',
  messages: {
    'zh-CN': zh,
    'en-US': en,
  },
})

// setLocale 切换语言并记忆偏好，同时同步 dayjs（日期组件）语言。
export function setLocale(locale: AppLocale) {
  i18n.global.locale.value = locale
  syncDayjsLocale(locale)
  localStorage.setItem('mc_locale', locale)
}

// currentLocale 读取当前语言，供 Ant Design Vue 组件 locale 联动。
export function currentLocale(): AppLocale {
  return i18n.global.locale.value as AppLocale
}

export default i18n