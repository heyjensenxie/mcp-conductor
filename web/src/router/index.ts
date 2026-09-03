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
        { path: 'dashboard', name: 'dashboard', component: () => import('@/views/DashboardView.vue'), meta: { title: 'Dashboard' } },
        { path: 'servers', name: 'servers', component: () => import('@/views/ServersView.vue'), meta: { title: 'MCP Servers' } },
        { path: 'servers/:id', name: 'server-detail', component: () => import('@/views/ServerDetailView.vue'), meta: { title: 'Server Detail' } },
        { path: 'tools', name: 'tools', component: () => import('@/views/ToolsView.vue'), meta: { title: 'Tools' } },
        { path: 'routes', name: 'routes', component: () => import('@/views/RoutesView.vue'), meta: { title: 'Routes' } },
        { path: 'traffic', name: 'traffic', component: () => import('@/views/TrafficView.vue'), meta: { title: 'Traffic' } },
        { path: 'access', name: 'access', component: () => import('@/views/AccessControlView.vue'), meta: { title: 'Access Control' } },
        { path: 'testing', name: 'testing', component: () => import('@/views/TestingView.vue'), meta: { title: 'Testing' } },
        { path: 'observability', name: 'observability', component: () => import('@/views/ObservabilityView.vue'), meta: { title: 'Observability' } },
        { path: 'settings', name: 'settings', component: () => import('@/views/SettingsView.vue'), meta: { title: 'Settings' } },
      ],
    },
  ],
})

export default router