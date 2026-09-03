<template>
  <a-config-provider :locale="antLocale">
    <router-view />
  </a-config-provider>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import zhCN from 'ant-design-vue/es/locale/zh_CN'
import enUS from 'ant-design-vue/es/locale/en_US'

// Ant Design Vue 组件级文案随应用语言联动。
const { locale } = useI18n()
const antLocale = computed(() => (locale.value === 'zh-CN' ? zhCN : enUS))
</script>

<style>
html,
body,
#app {
  height: 100%;
  margin: 0;
}
body {
  background: #f0f2f5;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial,
    'PingFang SC', 'Microsoft YaHei', sans-serif;
}
</style>