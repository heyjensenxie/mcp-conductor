<template>
  <a-layout class="layout">
    <!-- 深色工程侧栏 -->
    <a-layout-sider width="220" :theme="resolvedTheme" class="sider">
      <div class="brand">
        <img src="/logo.png" class="brand-logo" alt="MCP Conductor" />
        <span class="brand-title mono">{{ store.instanceName }}</span>
      </div>

      <a-menu :selected-keys="selectedKeys" mode="inline" :theme="resolvedTheme" class="menu" @click="onMenuClick">
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
        <a-menu-item key="security">
          <template #icon><SafetyOutlined /></template>{{ t('menu.security') }}
        </a-menu-item>
        <a-menu-item key="observability">
          <template #icon><BarChartOutlined /></template>{{ t('menu.observability') }}
        </a-menu-item>
        <a-menu-item key="evaluation">
          <template #icon><AuditOutlined /></template>{{ t('menu.evaluation') }}
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
          <span class="page-title">{{ pageTitle }}</span>
          <span class="header-crumb">{{ crumb }}</span>
        </div>
        <a-space :size="12">
          <a-select :value="locale" style="width: 116px" size="small" class="mono" @change="onLocaleChange">
            <a-select-option value="zh-CN">{{ t('lang.zh') }}</a-select-option>
            <a-select-option value="en-US">{{ t('lang.en') }}</a-select-option>
          </a-select>
          <a-dropdown placement="bottomRight" :trigger="['click']">
            <a-button type="text" size="small" class="theme-trigger" :title="t('theme.appearance')" :aria-label="t('theme.appearance')">
              <component :is="themeIcon" />
            </a-button>
            <template #overlay>
              <a-menu :selected-keys="[themeMode]" @click="onThemeMenuClick">
                <a-menu-item key="light">
                  <template #icon><BulbOutlined /></template>{{ t('theme.light') }}
                </a-menu-item>
                <a-menu-item key="dark">
                  <template #icon><BulbFilled /></template>{{ t('theme.dark') }}
                </a-menu-item>
                <a-menu-item key="system">
                  <template #icon><DesktopOutlined /></template>{{ t('theme.system') }}
                </a-menu-item>
              </a-menu>
            </template>
          </a-dropdown>
          <a-tag color="blue" class="mono">v1.0.0</a-tag>
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
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  AuditOutlined,
  BarChartOutlined,
  CloudServerOutlined,
  DashboardOutlined,
  FundOutlined,
  SafetyCertificateOutlined,
  SafetyOutlined,
  SettingOutlined,
  ShareAltOutlined,
  DesktopOutlined,
  BulbOutlined,
  BulbFilled,
  ToolOutlined,
} from '@ant-design/icons-vue'
import { setLocale, type AppLocale } from '@/i18n'
import { ensureAuthRequired, invalidateAuthState, isSessionToken } from '@/auth/session'
import { useAppStore } from '@/stores/app'
import { getThemeMode, resolveTheme, setThemeMode, THEME_CHANGE_EVENT, type ThemeMode, type ResolvedTheme } from '@/theme'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const store = useAppStore()

const authRequired = ref(false)
const themeMode = ref<ThemeMode>(getThemeMode())
const resolvedTheme = ref<ResolvedTheme>(resolveTheme(themeMode.value))
const themeIcon = computed(() => (themeMode.value === 'light' ? BulbOutlined : themeMode.value === 'dark' ? BulbFilled : DesktopOutlined))

function handleThemeChange(event: Event) {
  const detail = (event as CustomEvent<{ mode?: ThemeMode; resolved?: ResolvedTheme }>).detail
  themeMode.value = detail?.mode ?? getThemeMode()
  resolvedTheme.value = detail?.resolved ?? resolveTheme(themeMode.value)
}

onMounted(async () => {
  authRequired.value = await ensureAuthRequired()
  // 实例名持久化后刷新页面时同步标签标题。
  document.title = store.instanceName
  window.addEventListener(THEME_CHANGE_EVENT, handleThemeChange)
})
onBeforeUnmount(() => window.removeEventListener(THEME_CHANGE_EVENT, handleThemeChange))
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
  for (const key of ['dashboard', 'servers', 'tools', 'routes', 'traffic', 'access', 'security', 'observability', 'evaluation', 'settings']) {
    if (path === `/${key}` || path.startsWith(`/${key}/`)) return [key]
  }
  return ['dashboard']
})

const pageTitle = computed(() => t(`page.${route.meta.titleKey ?? 'dashboard'}`))
const crumb = computed(() => route.name === 'server-detail' ? t('page.servers') : route.name === 'tool-detail' ? t('page.tools') : route.name === 'access-key-detail' ? t('page.access') : '')

function onMenuClick({ key }: { key: string }) {
  router.push(`/${key}`)
}

function onLocaleChange(l: AppLocale) {
  setLocale(l)
}

function onThemeMenuClick({ key }: { key: string }) {
  if (key === 'light' || key === 'dark' || key === 'system') setThemeMode(key)
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
  border-right: 1px solid var(--mc-sidebar-line);
  display: flex;
  flex-direction: column;
}
.brand {
  display: flex;
  align-items: center;
  gap: 9px;
  height: 64px;
  padding: 0 18px;
  border-bottom: 1px solid var(--mc-sidebar-line);
}
.brand-logo {
  height: 32px;
  width: auto;
  border-radius: 6px;
}
.brand-title {
  font-size: 13.5px;
  font-weight: 600;
  letter-spacing: 0.06em;
  color: var(--mc-ink);
  white-space: nowrap;
}
.menu {
  flex: 1;
  padding-top: 12px;
  background: transparent !important;
  border-inline-end: none !important;
}
:deep(.ant-menu-dark .ant-menu-item),
:deep(.ant-menu-light .ant-menu-item) {
  height: 44px;
  line-height: 44px;
  margin: 2px 8px;
  border-radius: var(--mc-radius-md);
  color: var(--mc-sidebar-text);
  transition: color 180ms ease, background 180ms ease;
}
:deep(.ant-menu-dark .ant-menu-item:hover),
:deep(.ant-menu-light .ant-menu-item:hover) {
  background: var(--mc-menu-hover) !important;
  color: var(--mc-ink) !important;
}
:deep(.ant-menu-dark .ant-menu-item-selected),
:deep(.ant-menu-light .ant-menu-item-selected) {
  background: var(--mc-sidebar-active) !important;
  color: var(--mc-ink-strong) !important;
  border-left: 2px solid var(--mc-accent);
  padding-left: 22px;
}
:deep(.ant-menu-dark .ant-menu-item-selected .ant-menu-item-icon),
:deep(.ant-menu-light .ant-menu-item-selected .ant-menu-item-icon) {
  color: var(--mc-accent-light);
}
:deep(.ant-menu-dark .ant-menu-item-selected::after),
:deep(.ant-menu-light .ant-menu-item-selected::after) {
  display: none;
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
  background: var(--mc-header-bg);
  backdrop-filter: blur(10px);
  border-bottom: 1px solid var(--mc-sidebar-line);
  padding: 0 24px;
  height: 60px;
  line-height: 60px;
}
.header-left {
  display: flex;
  align-items: baseline;
  gap: 12px;
}
.page-title {
  font-size: 20px;
  font-weight: 600;
  letter-spacing: -0.01em;
  color: var(--mc-ink-strong);
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

:deep(.header .ant-select-selector) {
  background: rgba(148, 163, 184, 0.06) !important;
  border-color: var(--mc-line) !important;
}
:deep(.user-tag) {
  background: rgba(148, 163, 184, 0.08) !important;
  border-color: var(--mc-line) !important;
  color: var(--mc-ink-2) !important;
}
:deep(.theme-trigger) {
  width: 32px;
  padding: 0;
  color: var(--mc-ink-2) !important;
}
:deep(.theme-trigger:hover) { color: var(--mc-ink) !important; }

@media (max-width: 760px) {
  .header { padding: 0 16px; }
  .header-crumb { display: none; }
  .page-title { font-size: 17px; }
}
</style>
