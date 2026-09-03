<template>
  <a-layout class="layout">
    <a-layout-sider width="216" theme="light" class="sider">
      <div class="brand">
        <img src="/logo.png" class="brand-logo" alt="MCP Conductor" />
        <span class="brand-text">MCP Conductor</span>
      </div>
      <a-menu :selected-keys="selectedKeys" mode="inline" class="menu" @click="onMenuClick">
        <a-menu-item key="dashboard">
          <template #icon><DashboardOutlined /></template>{{ t('menu.dashboard') }}
        </a-menu-item>
        <a-menu-item key="servers">
          <template #icon><CloudServerOutlined /></template>{{ t('menu.servers') }}
        </a-menu-item>
        <a-menu-item key="tools">
          <template #icon><ToolOutlined /></template>{{ t('menu.tools') }}
        </a-menu-item>
        <a-menu-item key="routes">
          <template #icon><ShareAltOutlined /></template>{{ t('menu.routes') }}
        </a-menu-item>
        <a-menu-item key="traffic">
          <template #icon><FundOutlined /></template>{{ t('menu.traffic') }}
        </a-menu-item>
        <a-menu-item key="access">
          <template #icon><SafetyCertificateOutlined /></template>{{ t('menu.access') }}
        </a-menu-item>
        <a-menu-item key="testing">
          <template #icon><ExperimentOutlined /></template>{{ t('menu.testing') }}
        </a-menu-item>
        <a-menu-item key="observability">
          <template #icon><BarChartOutlined /></template>{{ t('menu.observability') }}
        </a-menu-item>
        <a-menu-item key="settings">
          <template #icon><SettingOutlined /></template>{{ t('menu.settings') }}
        </a-menu-item>
      </a-menu>
    </a-layout-sider>

    <a-layout>
      <a-layout-header class="header">
        <span class="page-title">{{ pageTitle }}</span>
        <a-space>
          <a-tag color="blue">v0.1.0</a-tag>
          <a-select :value="locale" style="width: 120px" size="small" @change="onLocaleChange">
            <a-select-option value="zh-CN">{{ t('lang.zh') }}</a-select-option>
            <a-select-option value="en-US">{{ t('lang.en') }}</a-select-option>
          </a-select>
        </a-space>
      </a-layout-header>
      <a-layout-content class="content">
        <router-view />
      </a-layout-content>
    </a-layout>
  </a-layout>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  BarChartOutlined,
  CloudServerOutlined,
  DashboardOutlined,
  ExperimentOutlined,
  FundOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
  ShareAltOutlined,
  ToolOutlined,
} from '@ant-design/icons-vue'
import { setLocale, type AppLocale } from '@/i18n'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()

// 菜单高亮：按路径首段推导，使 `/servers/:id` 等子页保持所属菜单选中。
const selectedKeys = computed(() => {
  const path = route.path
  for (const key of ['dashboard', 'servers', 'tools', 'routes', 'traffic', 'access', 'testing', 'observability', 'settings']) {
    if (path === `/${key}` || path.startsWith(`/${key}/`)) return [key]
  }
  return ['dashboard']
})

const pageTitle = computed(() => t(`page.${route.meta.titleKey ?? 'dashboard'}`))

function onMenuClick({ key }: { key: string }) {
  router.push(`/${key}`)
}

function onLocaleChange(locale: AppLocale) {
  setLocale(locale)
}
</script>

<style scoped>
.layout {
  height: 100vh;
}
.sider {
  border-right: 1px solid #f0f0f0;
  display: flex;
  flex-direction: column;
}
.brand {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 56px;
  padding: 0 18px;
  font-weight: 600;
  color: rgba(0, 0, 0, 0.88);
  border-bottom: 1px solid #f0f0f0;
}
.brand-logo {
  height: 32px;
  width: auto;
  border-radius: 6px;
}
.brand-text {
  font-size: 15px;
  letter-spacing: 0.2px;
  white-space: nowrap;
}
.menu {
  flex: 1;
  border-inline-end: none !important;
}
.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: #fff;
  border-bottom: 1px solid #f0f0f0;
  padding: 0 24px;
  height: 56px;
  line-height: 56px;
}
.page-title {
  font-size: 16px;
  font-weight: 600;
}
.content {
  padding: 24px;
  overflow: auto;
}
</style>