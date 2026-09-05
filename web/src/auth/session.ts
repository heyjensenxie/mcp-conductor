import { getAuthStatus } from '@/api'

// 后台鉴权状态的小型门面：把“是否要求登录”探测缓存起来，避免每次路由
// 导航都请求 /api/auth/status；登出或 401 后失效缓存以重新探测。

let requiredPromise: Promise<boolean> | null = null

// ensureAuthRequired 返回后台当前是否要求鉴权（结果缓存）。
export function ensureAuthRequired(): Promise<boolean> {
  if (!requiredPromise) {
    requiredPromise = getAuthStatus()
      .then((s) => s.auth_required)
      .catch(() => false)
  }
  return requiredPromise
}

// invalidateAuthState 在登出/凭据失效后清除缓存，使下次守卫重新探测。
export function invalidateAuthState() {
  requiredPromise = null
}

// isSessionToken 判断令牌是否为登录会话令牌（mc1. 前缀，与后端 tokenPrefix 对齐）；
// operator_token 直填不是会话，但在后台鉴权下同样有效。
export function isSessionToken(token: string): boolean {
  return token.startsWith('mc1.')
}
