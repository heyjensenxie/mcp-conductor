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

// 响应拦截：统一解包信封；业务失败与非 2xx 都转成可读 Error。
http.interceptors.response.use(
  (resp) => {
    const env = resp.data as Envelope<unknown>
    if (env && typeof env.code === 'string' && env.code !== 'ok') {
      return Promise.reject(new Error(env.message || env.code))
    }
    return resp
  },
  (error) => {
    const status = error?.response?.status as number | undefined
    const data = error?.response?.data as { code?: string; message?: string } | undefined
    const code = data && typeof data.code === 'string' ? data.code : ''
    const message = data?.message || error?.message || '请求失败'

    // 认证失效（401 / authentication_error）：清除凭据并引导到登录页。
    // 登录接口自身失败（status 401）时已在 /login，不重复跳转。
    if (status === 401 || code === 'authentication_error') {
      localStorage.removeItem('mc_token')
      if (!window.location.hash.startsWith('#/login')) {
        window.location.hash = '#/login'
      }
      return Promise.reject(new Error(message || '登录已过期，请重新登录'))
    }
    return Promise.reject(new Error(message))
  },
)

// unwrap 提取信封内的数据体。
export async function unwrap<T>(promise: Promise<{ data: Envelope<T> }>): Promise<T> {
  return (await promise).data.data
}

export default http
