import { reactive } from 'vue'
import { api, configureApi, refreshCsrf, setCsrf, setAccessToken, getAccessToken, ApiError } from './api.js'
export const auth = reactive({ user: null, initialized: false, checking: false, checkError: '', generation: 0 })
let checkSequence = 0
let initPromise
let refreshPromise
let refreshBlocked = false
let channel
let appliedRevision
const revisionKey = 'freshplate-auth-revision'
function revision() {
  try { return localStorage.getItem(revisionKey) }
  catch { throw new ApiError('无法安全协调标签页，请允许站点存储后重试。', 'STORAGE_UNAVAILABLE') }
}
function publish(session) {
  const value = crypto.randomUUID()
  try { localStorage.setItem(revisionKey, value) }
  catch { throw new ApiError('无法安全协调标签页，请允许站点存储后重试。', 'STORAGE_UNAVAILABLE') }
  appliedRevision = value
  channel?.postMessage({ type: 'session', revision: value, session })
}
function applyUser(user) {
  const previous = auth.user
  if (previous?.id !== user?.id || previous?.role !== user?.role || previous?.must_change_password !== user?.must_change_password) {
    auth.generation++
    setCsrf(null)
  }
  auth.user = user
}
function coordination() {
  if (!globalThis.navigator?.locks || typeof BroadcastChannel === 'undefined') throw new ApiError('浏览器不支持安全的多标签页登录，请使用新版浏览器。', 'BROWSER_UNSUPPORTED')
  if (appliedRevision === undefined) appliedRevision = revision()
  if (!channel) {
    channel = new BroadcastChannel('freshplate-auth')
    channel.onmessage = ({ data }) => {
      try {
        if (data?.revision !== revision()) return
        if (data.type === 'session') { appliedRevision = data.revision; applySession(data.session) }
        if (data.type === 'refresh-failed') refreshBlocked = true
      } catch (error) { auth.checkError = error.message }
    }
  }
}
function locked(action, refreshing = false) {
  coordination()
  return navigator.locks.request('freshplate-auth', () => {
    const current = revision()
    if (current !== appliedRevision && !refreshing) throw new ApiError('其他标签页的登录状态已变化，请重新检查身份。', 'IDENTITY_CHANGED')
    try {
      const marker = current || crypto.randomUUID()
      localStorage.setItem(revisionKey, marker)
      if (!current) appliedRevision = marker
    } catch { throw new ApiError('无法安全协调标签页，请允许站点存储后重试。', 'STORAGE_UNAVAILABLE') }
    return action()
  })
}
function applySession(result, invalidate = true) {
  if (invalidate) ++checkSequence
  applyUser(result?.user || null)
  setAccessToken(result?.access_token || null)
  setCsrf(result?.csrf_token || null)
  refreshBlocked = false
  auth.initialized = true
  if (invalidate) auth.checking = false
  auth.checkError = ''
}
export function acceptLogin(result) {
  applySession(result)
  publish(result)
}
export function refreshSession() {
  if (refreshPromise) return refreshPromise
  const token = getAccessToken()
  const generation = auth.generation
  refreshPromise = locked(async () => {
    if (generation !== auth.generation) throw new ApiError('当前账号已变化，请重新提交。', 'STALE_RESPONSE')
    if (getAccessToken() && getAccessToken() !== token) return
    if (refreshBlocked) throw new ApiError('刷新结果未能确认，请重新登录。', 'REFRESH_UNCERTAIN')
    try {
      await refreshCsrf()
      const result = await api('/auth/refresh', { method: 'POST' })
      applySession(result, false)
      publish(result)
    } catch (error) {
      if (error.status === 401) { await endSession(); throw error }
      // A lost response may already have consumed the refresh token.
      if (error.code === 'NETWORK_ERROR' || error.code === 'INVALID_RESPONSE') {
        await endSession()
        refreshBlocked = true
        channel?.postMessage({ type: 'refresh-failed', revision: revision() })
      }
      throw error
    }
  }, true).finally(() => { refreshPromise = null })
  return refreshPromise
}
export async function login(credentials) {
  const generation = auth.generation
  return locked(async () => {
    if (generation !== auth.generation) throw new ApiError('当前账号已变化，请重新提交。', 'IDENTITY_CHANGED')
    await refreshCsrf()
    const result = await api('/auth/login', { method: 'POST', body: credentials })
    acceptLogin(result)
    return result
  })
}
export async function checkIdentity() {
  const sequence = ++checkSequence
  auth.checking = true
  auth.checkError = ''
  try {
    if (!getAccessToken()) {
      if (refreshBlocked) { auth.initialized = true; return true }
      try { await refreshSession() } catch (error) { if (error.status !== 401) throw error }
      return true
    }
    let user
    try { user = await api('/me') } catch (error) { if (error.status === 401) user = null; else throw error }
    if (sequence === checkSequence) { applyUser(user); if (!user) setAccessToken(null); auth.initialized = true }
    return sequence === checkSequence
  } catch (error) {
    if (sequence === checkSequence) auth.checkError = error.code === 'BROWSER_UNSUPPORTED' || error.code === 'REFRESH_UNCERTAIN' ? error.message : '无法确认当前身份，提交已暂停。请重试身份检查。'
    return false
  } finally { if (sequence === checkSequence) auth.checking = false }
}
export function initAuth() { return initPromise ||= checkIdentity() }
export async function verifyForm(expectedUserId) {
  if (!await checkIdentity() || auth.checking || auth.checkError) throw new ApiError('身份检查失败，请重试。', 'IDENTITY_UNVERIFIED')
  if (auth.user?.id !== expectedUserId) throw new ApiError('当前账号已变化，请在新页面重新填写。', 'IDENTITY_CHANGED')
}
export async function endSession() {
  applySession(null)
  publish(null)
}
export async function logoutCurrentSession() {
  if (!await checkIdentity() || auth.checking || auth.checkError) throw new ApiError('身份检查失败，请重试。', 'IDENTITY_UNVERIFIED')
  const generation = auth.generation
  await locked(async () => {
    if (generation !== auth.generation) throw new ApiError('当前账号已变化，请重新提交。', 'IDENTITY_CHANGED')
    await refreshCsrf()
    await api('/auth/logout', { method: 'POST' })
    await endSession()
  })
}
export async function changePassword(expectedUserId, body) {
  await verifyForm(expectedUserId)
  const generation = auth.generation
  await locked(async () => {
    if (generation !== auth.generation) throw new ApiError('当前账号已变化，请重新提交。', 'IDENTITY_CHANGED')
    await api('/me/password', { method: 'PUT', expectedUserId, body, refresh: false })
    await endSession()
  })
}
export function startIdentityWatch() {
  try { coordination() } catch (error) { auth.checkError = error.message }
  window.addEventListener('focus', () => { void checkIdentity() })
  document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible') void checkIdentity() })
}
configureApi({
  generation: () => auth.generation,
  unauthorized: () => { applySession(null) },
  passwordRequired: () => { if (auth.user) applyUser({ ...auth.user, must_change_password: true }) },
  recover: async () => { await checkIdentity(); await refreshCsrf() },
  refresh: refreshSession,
})
