<template>
  <a-layout class="layout">
    <!-- 深色工程侧栏 -->
    <a-layout-sider width="224" theme="dark" class="sider">
      <div class="brand">
        <img src="/logo.png" class="brand-logo" alt="MCP Conductor" />
        <span class="brand-title mono">MCP Conductor</span>
      </div>

      <a-menu :selected-keys="selectedKeys" mode="inline" theme="dark" class="menu" @click="onMenuClick">
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

      <div class="sider-footer">
        <span class="mc-dot mc-dot--ok"></span>
        <span class="mono footer-text">gateway · healthy</span>
      </div>
    </a-layout-sider>

    <a-layout>
      <a-layout-header class="header">
        <div class="header-left">
          <span class="page-title mono">{{ pageTitle }}</span>
          <span class="header-crumb">{{ crumb }}</span>
        </div>
        <a-space :size="12">
          <a-select :value="locale" style="width: 116px" size="small" class="mono" @change="onLocaleChange">
            <a-select-option value="zh-CN">{{ t('lang.zh') }}</a-select-option>
            <a-select-option value="en-US">{{ t('lang.en') }}</a-select-option>
          </a-select>
          <a-tag color="#1f6feb" class="mono">v0.1.0</a-tag>
          <template v-if="authRequired && store.token">
            <a-tag class="mono user-tag">{{ userTag }}</a-tag>
            <a-popconfirm :title="t('auth.confirmLogout')" :ok-text="t('auth.logout')" :cancel-text="t('common.cancel')" @confirm="onLogout">
              <a-button size="small">{{ t('auth.logout') }}</a-button>
            </a-popconfirm>
          </template>
        </a-space>
      </a-layout-header>

      <a-layout-content class="content mc-grid">
        <router-view />
      </a-layout-content>
    </a-layout>
  </a-layout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
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
import { ensureAuthRequired, invalidateAuthState, isSessionToken } from '@/auth/session'
import { useAppStore } from '@/stores/app'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const store = useAppStore()

const authRequired = ref(false)
onMounted(async () => {
  authRequired.value = await ensureAuthRequired()
})
// 登录态标识：会话令牌显示 session，operator_token 直填显示 operator。
const userTag = computed(() => (isSessionToken(store.token) ? 'session' : 'operator'))

function onLogout() {
  store.clearToken()
  invalidateAuthState()
  router.push('/login')
}

// 菜单高亮：按路径首段推导，使 `/servers/:id` 等子页保持所属菜单选中。
const selectedKeys = computed(() => {
  const path = route.path
  for (const key of ['dashboard', 'servers', 'tools', 'routes', 'traffic', 'access', 'testing', 'observability', 'settings']) {
    if (path === `/${key}` || path.startsWith(`/${key}/`)) return [key]
  }
  return ['dashboard']
})

const pageTitle = computed(() => t(`page.${route.meta.titleKey ?? 'dashboard'}`))
const crumb = computed(() => route.name === 'server-detail' ? t('page.servers') : route.name === 'access-key-detail' ? t('page.access') : '')

function onMenuClick({ key }: { key: string }) {
  router.push(`/${key}`)
}

function onLocaleChange(l: AppLocale) {
  setLocale(l)
}
</script>

<style scoped>
.layout {
  height: 100vh;
  background: var(--mc-bg);
}

/* ---- 侧栏 ---- */
.sider {
  background: var(--mc-sidebar) !important;
  border-right: 1px solid rgba(255, 255, 255, 0.06);
  display: flex;
  flex-direction: column;
}
.brand {
  display: flex;
  align-items: center;
  gap: 9px;
  height: 58px;
  padding: 0 18px;
  border-bottom: 1px solid var(--mc-sidebar-line);
}
.brand-logo {
  height: 30px;
  width: auto;
  border-radius: 5px;
}
.brand-title {
  font-size: 13.5px;
  font-weight: 600;
  letter-spacing: 0.06em;
  color: #eef3fb;
  white-space: nowrap;
}
.menu {
  flex: 1;
  padding-top: 8px;
  background: transparent !important;
  border-inline-end: none !important;
}
:deep(.ant-menu-dark .ant-menu-item) {
  margin: 2px 8px;
  border-radius: 6px;
}
:deep(.ant-menu-dark .ant-menu-item-selected) {
  color: #eaf1ff;
}
.sider-footer {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 14px 20px;
  border-top: 1px solid var(--mc-sidebar-line);
}
.footer-text {
  font-size: 11px;
  color: var(--mc-sidebar-text);
  letter-spacing: 0.04em;
}

/* ---- 顶栏 ---- */
.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: rgba(255, 255, 255, 0.86);
  backdrop-filter: saturate(1.2) blur(6px);
  border-bottom: 1px solid var(--mc-line);
  padding: 0 24px;
  height: 58px;
  line-height: 58px;
}
.header-left {
  display: flex;
  align-items: baseline;
  gap: 12px;
}
.page-title {
  font-size: 15px;
  font-weight: 600;
  letter-spacing: 0.03em;
  color: var(--mc-ink);
}
.header-crumb {
  font-family: var(--mc-mono);
  font-size: 12px;
  color: var(--mc-ink-3);
}

/* ---- 内容 ---- */
.content {
  padding: 24px;
  overflow: auto;
}
</style>
