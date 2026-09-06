import { createRouter, createWebHashHistory } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { ensureAuthRequired } from '@/auth/session'
import AppLayout from '@/layout/AppLayout.vue'

// 前端使用 hash 路由，便于嵌入 Go 二进制后无需服务端回退配置。
const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    // 登录页：独立于 AppLayout，后台默认开启鉴权时用于换取会话令牌。
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue'),
      meta: { titleKey: 'login' },
    },
    {
      path: '/',
      component: AppLayout,
      children: [
        { path: '', redirect: '/dashboard' },
        { path: 'dashboard', name: 'dashboard', component: () => import('@/views/DashboardView.vue'), meta: { titleKey: 'dashboard' } },
        { path: 'servers', name: 'servers', component: () => import('@/views/ServersView.vue'), meta: { titleKey: 'servers' } },
        { path: 'servers/:id', name: 'server-detail', component: () => import('@/views/ServerDetailView.vue'), meta: { titleKey: 'serverDetail' } },
        { path: 'tools', name: 'tools', component: () => import('@/views/ToolsView.vue'), meta: { titleKey: 'tools' } },
        { path: 'tools/:id', name: 'tool-detail', component: () => import('@/views/ToolDetailView.vue'), meta: { titleKey: 'toolDetail' } },
        { path: 'routes', name: 'routes', component: () => import('@/views/RoutesView.vue'), meta: { titleKey: 'routes' } },
        { path: 'traffic', name: 'traffic', component: () => import('@/views/TrafficView.vue'), meta: { titleKey: 'traffic' } },
        { path: 'access', name: 'access', component: () => import('@/views/AccessControlView.vue'), meta: { titleKey: 'access' } },
        { path: 'access/keys/:id', name: 'access-key-detail', component: () => import('@/views/AccessKeyDetailView.vue'), meta: { titleKey: 'accessKeyDetail' } },
        { path: 'observability', name: 'observability', component: () => import('@/views/ObservabilityView.vue'), meta: { titleKey: 'observability' } },
        { path: 'evaluation', name: 'evaluation', component: () => import('@/views/EvaluationView.vue'), meta: { titleKey: 'evaluation' } },
        { path: 'settings', name: 'settings', component: () => import('@/views/SettingsView.vue'), meta: { titleKey: 'settings' } },
      ],
    },
  ],
})

// 全局鉴权守卫：后台要求登录且本地无凭据时，引导到登录页。
router.beforeEach(async (to) => {
  if (to.path === '/login') return true
  const authRequired = await ensureAuthRequired()
  if (!authRequired) return true
  const store = useAppStore()
  if (store.token) return true
  return { path: '/login', query: { redirect: to.fullPath } }
})

export default router
