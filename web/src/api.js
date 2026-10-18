let csrf = null
let accessToken = null
let hooks = { generation: () => 0, unauthorized() {}, passwordRequired() {}, async recover() {}, async refresh() {} }
export function configureApi(value) { hooks = value }
export function setCsrf(value) { csrf = value }
export function setAccessToken(value) { accessToken = value }
export function getAccessToken() { return accessToken }
export class ApiError extends Error {
  constructor(message, code, status) { super(message); this.code = code; this.status = status }
}
export async function refreshCsrf() {
  const value = await api('/auth/csrf')
  csrf = value.token
  return csrf
}
const cookieWrites = ['/auth/login', '/auth/refresh', '/auth/logout']
export async function api(path, { method = 'GET', body, expectedUserId, refresh = true } = {}, retried = false) {
  const generation = hooks.generation()
  const headers = { Accept: 'application/json' }
  if (accessToken && !path.startsWith('/auth/')) headers.Authorization = `Bearer ${accessToken}`
  if (method !== 'GET') {
    if (cookieWrites.includes(path)) {
      if (!csrf) await refreshCsrf()
      headers['X-CSRF-Token'] = csrf
    } else if (path !== '/auth/register') {
      if (!Number.isSafeInteger(expectedUserId) || expectedUserId < 1) throw new ApiError('请重新加载表单后提交。', 'MISSING_IDENTITY')
      headers['X-Expected-User-ID'] = String(expectedUserId)
    }
  }
  if (generation !== hooks.generation()) throw new ApiError('身份已变化，请重新加载页面。', 'STALE_RESPONSE')
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  let response
  try { response = await fetch(`/api${path}`, { method, credentials: 'same-origin', headers, body: body === undefined ? undefined : JSON.stringify(body) }) }
  catch { throw new ApiError('连接失败，请检查网络后重试。', 'NETWORK_ERROR') }
  let payload = null
  if (response.status !== 204) {
    try { payload = await response.json() } catch { throw new ApiError('服务响应异常，请重试。', 'INVALID_RESPONSE', response.status) }
  }
  if (generation !== hooks.generation()) throw new ApiError('身份已变化，已丢弃旧请求结果。', 'STALE_RESPONSE')
  if (!response.ok) {
    const error = new ApiError(payload?.error?.message || '请求失败，请重试。', payload?.error?.code || 'REQUEST_FAILED', response.status)
    if (response.status === 401 && error.code === 'ACCESS_TOKEN_EXPIRED' && !path.startsWith('/auth/') && !retried && refresh) {
      await hooks.refresh()
      if (generation !== hooks.generation()) throw new ApiError('当前账号已变化，请重新提交。', 'STALE_RESPONSE')
      return api(path, { method, body, expectedUserId, refresh }, true)
    }
    if (response.status === 401 && !path.startsWith('/auth/') && (refresh || error.code !== 'ACCESS_TOKEN_EXPIRED')) hooks.unauthorized?.()
    if (error.code === 'PASSWORD_CHANGE_REQUIRED') hooks.passwordRequired?.()
    if (error.code === 'CSRF_INVALID' || error.code === 'IDENTITY_CHANGED') {
      csrf = null
      if (refresh && !path.startsWith('/auth/')) await hooks.recover?.()
      throw new ApiError('安全令牌或身份已更新，请检查页面后重新提交；本次操作未自动重试。', error.code, response.status)
    }
    throw error
  }
  return payload?.data
}
