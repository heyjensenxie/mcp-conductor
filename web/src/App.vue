<template>
  <a-config-provider :locale="antLocale" :theme="antTheme">
    <router-view />
  </a-config-provider>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import zhCN from 'ant-design-vue/es/locale/zh_CN'
import enUS from 'ant-design-vue/es/locale/en_US'
import { theme as antdTheme } from 'ant-design-vue'
import './styles/theme.css'
import { getThemeMode, resolveTheme, THEME_CHANGE_EVENT, type ResolvedTheme } from './theme'

// Ant Design Vue 组件级文案随应用语言联动。
const { locale } = useI18n()
const antLocale = computed(() => (locale.value === 'zh-CN' ? zhCN : enUS))

const resolvedTheme = ref<ResolvedTheme>(resolveTheme(getThemeMode()))
function onThemeChange(event: Event) {
  const detail = (event as CustomEvent<{ resolved?: ResolvedTheme }>).detail
  resolvedTheme.value = detail?.resolved ?? resolveTheme(getThemeMode())
}
onMounted(() => window.addEventListener(THEME_CHANGE_EVENT, onThemeChange))
onBeforeUnmount(() => window.removeEventListener(THEME_CHANGE_EVENT, onThemeChange))

// Operator Console 设计令牌：两套表面层级共享同一组组件形态与品牌语义。
const antThemes = {
  dark: {
    algorithm: antdTheme.darkAlgorithm,
    token: {
      colorPrimary: '#3b82f6', colorInfo: '#38bdf8', colorSuccess: '#22c55e', colorWarning: '#f59e0b', colorError: '#ef4444',
      colorBgBase: '#07111f', colorBgLayout: '#07111f', colorBgContainer: '#0d1b2a', colorBgElevated: '#102238',
      colorText: '#f1f5f9', colorTextSecondary: '#94a3b8', colorTextTertiary: '#64748b', colorTextQuaternary: '#475569',
      colorBorder: 'rgba(148, 163, 184, 0.12)', colorBorderSecondary: 'rgba(148, 163, 184, 0.08)',
      borderRadius: 8, borderRadiusLG: 12, borderRadiusSM: 6, fontSize: 13,
      fontFamily: "Inter, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif",
    },
    components: {
      Button: { fontWeight: 500 }, Card: { headerBg: 'transparent', bodyPadding: 20, headerPadding: 20 },
      Table: { headerBg: 'rgba(148, 163, 184, 0.035)', headerColor: '#94a3b8', rowHoverBg: 'rgba(59, 130, 246, 0.035)' },
      Tag: { borderRadiusSM: 6 }, Modal: { contentBg: '#102238', headerBg: 'transparent', footerBg: 'transparent' }, Drawer: { colorBgElevated: '#102238' },
      Menu: { darkItemBg: 'transparent', darkItemColor: '#94a3b8', darkItemHoverColor: '#e2e8f0', darkItemHoverBg: 'rgba(59, 130, 246, 0.08)', darkItemSelectedBg: 'rgba(37, 99, 235, 0.22)', darkItemSelectedColor: '#f8fafc' },
    },
  },
  light: {
    algorithm: antdTheme.defaultAlgorithm,
    token: {
      colorPrimary: '#2563eb', colorInfo: '#0284c7', colorSuccess: '#16a34a', colorWarning: '#d97706', colorError: '#dc2626',
      colorBgBase: '#f4f7fb', colorBgLayout: '#f4f7fb', colorBgContainer: '#ffffff', colorBgElevated: '#ffffff',
      colorText: '#0f172a', colorTextSecondary: '#475569', colorTextTertiary: '#64748b', colorTextQuaternary: '#94a3b8',
      colorBorder: '#e2e8f0', colorBorderSecondary: 'rgba(15, 23, 42, 0.07)',
      borderRadius: 8, borderRadiusLG: 12, borderRadiusSM: 6, fontSize: 13,
      fontFamily: "Inter, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif",
    },
    components: {
      Button: { fontWeight: 500 }, Card: { headerBg: 'transparent', bodyPadding: 20, headerPadding: 20 },
      Table: { headerBg: '#f8fafc', headerColor: '#64748b', rowHoverBg: '#f8fafc' },
      Tag: { borderRadiusSM: 6 }, Modal: { contentBg: '#ffffff', headerBg: 'transparent', footerBg: 'transparent' }, Drawer: { colorBgElevated: '#ffffff' },
      Menu: { itemBg: 'transparent', itemColor: '#64748b', itemHoverColor: '#334155', itemHoverBg: '#f5f8fc', itemSelectedBg: 'rgba(37, 99, 235, 0.07)', itemSelectedColor: '#1d4ed8' },
    },
  },
} as const
const antTheme = computed(() => antThemes[resolvedTheme.value])
</script>

<style>
html,
body,
#app {
  height: 100%;
  margin: 0;
}
body {
  background: #07111f;
  font-family: Inter, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif;
}
</style>
