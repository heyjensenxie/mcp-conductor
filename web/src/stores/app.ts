import { defineStore } from 'pinia'

// 应用级状态：管理凭据与全局加载态。
export const useAppStore = defineStore('app', {
  state: () => ({
    token: localStorage.getItem('mc_token') || '',
  }),
  actions: {
    setToken(token: string) {
      this.token = token
      localStorage.setItem('mc_token', token)
    },
  },
})