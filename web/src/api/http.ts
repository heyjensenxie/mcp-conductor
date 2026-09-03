import axios from 'axios'

// 后端统一响应信封：{ code, message, request_id, data }
export interface Envelope<T> {
  code: string
  message: string
  request_id: string
  data: T
}

const http = axios.create({ baseURL: '/api', timeout: 20000 })

// 请求拦截：附加管理凭据（X-Api-Key），与后端 auth 中间件对齐。
http.interceptors.request.use((config) => {
  const token = localStorage.getItem('mc_token')
  if (token) config.headers['X-Api-Key'] = token
  return config
})

// 响应拦截：统一解包信封，业务失败转为 reject。
http.interceptors.response.use((resp) => {
  const env = resp.data as Envelope<unknown>
  if (env && typeof env.code === 'string' && env.code !== 'ok') {
    return Promise.reject(new Error(env.message || env.code))
  }
  return resp
})

// unwrap 提取信封内的数据体。
export async function unwrap<T>(promise: Promise<{ data: Envelope<T> }>): Promise<T> {
  return (await promise).data.data
}

export default http