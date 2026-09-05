import { defineStore } from 'pinia'

// 应用级状态：管理凭据与会话。
export const useAppStore = defineStore('app', {
  state: () => ({
    token: localStorage.getItem('mc_token') || '',
  }),
  actions: {
    setToken(token: string) {
      this.token = token
      localStorage.setItem('mc_token', token)
    },
    // clearToken 清空管理凭据（登出 / 401 时使用）。
    clearToken() {
      this.token = ''
      localStorage.removeItem('mc_token')
    },
  },
})