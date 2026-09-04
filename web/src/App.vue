<template>
  <a-config-provider :locale="antLocale" :theme="antTheme">
    <router-view />
  </a-config-provider>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import zhCN from 'ant-design-vue/es/locale/zh_CN'
import enUS from 'ant-design-vue/es/locale/en_US'
import './styles/theme.css'

// Ant Design Vue 组件级文案随应用语言联动。
const { locale } = useI18n()
const antLocale = computed(() => (locale.value === 'zh-CN' ? zhCN : enUS))

// Operator Console 设计令牌：单一信号蓝 + 细圆角 + 等宽数字。
const antTheme = {
  token: {
    colorPrimary: '#1f6feb',
    colorInfo: '#1f6feb',
    colorSuccess: '#12a150',
    colorWarning: '#d9822b',
    colorError: '#e0453c',
    colorBgLayout: '#eef1f6',
    colorText: '#0d1520',
    colorTextSecondary: '#55606e',
    colorBorder: '#dbe1ea',
    colorBorderSecondary: '#e8ecf2',
    borderRadius: 6,
    fontSize: 13,
    fontFamily:
      "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', 'PingFang SC', 'Microsoft YaHei', sans-serif",
  },
  components: {
    Button: { fontWeight: 500 },
    Table: { headerBg: '#f7f9fc', headerColor: '#55606e' },
    Card: { headerBg: 'transparent' },
    Tag: { borderRadiusSM: 4 },
    Menu: {
      darkItemBg: 'transparent',
      darkItemColor: '#9aa7ba',
      darkItemHoverColor: '#d6e0ef',
      darkItemSelectedBg: 'rgba(31,111,235,0.22)',
      darkItemSelectedColor: '#eaf1ff',
    },
  },
}
</script>

<style>
html,
body,
#app {
  height: 100%;
  margin: 0;
}
body {
  background: #eef1f6;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial,
    'PingFang SC', 'Microsoft YaHei', sans-serif;
}
</style>