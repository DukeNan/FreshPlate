let csrf = null
let hooks = { generation: () => 0, unauthorized() {}, passwordRequired() {}, async recover() {} }
export function configureApi(value) { hooks = value }
export function setCsrf(value) { csrf = value }
export class ApiError extends Error {
  constructor(message, code, status) { super(message); this.code = code; this.status = status }
}
export async function refreshCsrf() {
  const value = await api('/auth/csrf')
  csrf = value.token
  return csrf
}
export async function api(path, { method = 'GET', body, expectedUserId } = {}) {
  const generation = hooks.generation()
  const headers = { Accept: 'application/json' }
  if (method !== 'GET') {
    if (!csrf) await refreshCsrf()
    if (generation !== hooks.generation()) throw new ApiError('身份已变化，请重新加载页面。', 'STALE_RESPONSE')
    headers['X-CSRF-Token'] = csrf
    if (!['/auth/login', '/auth/register', '/auth/logout'].includes(path)) {
      if (!Number.isSafeInteger(expectedUserId) || expectedUserId < 1) throw new ApiError('请重新加载表单后提交。', 'MISSING_IDENTITY')
      headers['X-Expected-User-ID'] = String(expectedUserId)
    }
  }
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
    if (response.status === 401 && path !== '/auth/login' && path !== '/me') hooks.unauthorized()
    if (error.code === 'PASSWORD_CHANGE_REQUIRED') hooks.passwordRequired()
    if (error.code === 'CSRF_INVALID' || error.code === 'IDENTITY_CHANGED') {
      csrf = null
      await hooks.recover()
      throw new ApiError('安全令牌或身份已更新，请检查页面后重新提交；本次操作未自动重试。', error.code, response.status)
    }
    throw error
  }
  return payload?.data
}
