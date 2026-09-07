import { defineStore } from 'pinia'

// 实例名称默认值（部署实例的自定义显示名，侧栏/登录页/标签标题共用）。
export const DEFAULT_INSTANCE_NAME = 'MCP Conductor'

// 应用级状态：管理凭据与会话 + 展示性实例设置。
export const useAppStore = defineStore('app', {
  state: () => ({
    token: localStorage.getItem('mc_token') || '',
    instanceName: localStorage.getItem('mc_instance_name') || DEFAULT_INSTANCE_NAME,
  }),
  actions: {
    setToken(token: string) {
      this.token = token
      localStorage.setItem('mc_token', token)
    },
    // 更新实例名（设置页保存时调用），并同步浏览器标签标题，使其全局一致可见。
    setInstanceName(name: string) {
      this.instanceName = name
      localStorage.setItem('mc_instance_name', name)
      document.title = name
    },
    // clearToken 清空管理凭据（登出 / 401 时使用）。
    clearToken() {
      this.token = ''
      localStorage.removeItem('mc_token')
    },
  },
})