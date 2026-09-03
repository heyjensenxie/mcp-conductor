import { createRouter, createWebHashHistory } from 'vue-router'
import AppLayout from '@/layout/AppLayout.vue'

// 前端使用 hash 路由，便于嵌入 Go 二进制后无需服务端回退配置。
const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: '/',
      component: AppLayout,
      children: [
        { path: '', redirect: '/dashboard' },
        { path: 'dashboard', name: 'dashboard', component: () => import('@/views/DashboardView.vue'), meta: { titleKey: 'dashboard' } },
        { path: 'servers', name: 'servers', component: () => import('@/views/ServersView.vue'), meta: { titleKey: 'servers' } },
        { path: 'servers/:id', name: 'server-detail', component: () => import('@/views/ServerDetailView.vue'), meta: { titleKey: 'serverDetail' } },
        { path: 'tools', name: 'tools', component: () => import('@/views/ToolsView.vue'), meta: { titleKey: 'tools' } },
        { path: 'routes', name: 'routes', component: () => import('@/views/RoutesView.vue'), meta: { titleKey: 'routes' } },
        { path: 'traffic', name: 'traffic', component: () => import('@/views/TrafficView.vue'), meta: { titleKey: 'traffic' } },
        { path: 'access', name: 'access', component: () => import('@/views/AccessControlView.vue'), meta: { titleKey: 'access' } },
        { path: 'testing', name: 'testing', component: () => import('@/views/TestingView.vue'), meta: { titleKey: 'testing' } },
        { path: 'observability', name: 'observability', component: () => import('@/views/ObservabilityView.vue'), meta: { titleKey: 'observability' } },
        { path: 'settings', name: 'settings', component: () => import('@/views/SettingsView.vue'), meta: { titleKey: 'settings' } },
      ],
    },
  ],
})

export default router