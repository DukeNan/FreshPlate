import { reactive } from 'vue'
import { api, configureApi, refreshCsrf, setCsrf, ApiError } from './api.js'
export const auth = reactive({ user: null, initialized: false, checking: false, checkError: '', generation: 0 })
let checkSequence = 0
let initPromise
let channel
function applyUser(user) {
  const previous = auth.user
  if (previous?.id !== user?.id || previous?.role !== user?.role || previous?.must_change_password !== user?.must_change_password) {
    auth.generation++
    setCsrf(null)
  }
  auth.user = user
}
export async function checkIdentity() {
  const sequence = ++checkSequence
  auth.checking = true
  auth.checkError = ''
  try {
    let user
    try { user = await api('/me') } catch (error) { if (error.status === 401) user = null; else throw error }
    if (sequence === checkSequence) { applyUser(user); auth.initialized = true }
    return sequence === checkSequence
  } catch (error) {
    if (sequence === checkSequence) auth.checkError = '无法确认当前身份，提交已暂停。请重试身份检查。'
    return false
  } finally { if (sequence === checkSequence) auth.checking = false }
}
export function initAuth() { return initPromise ||= checkIdentity() }
export async function verifyForm(expectedUserId) {
  if (!await checkIdentity() || auth.checking || auth.checkError) throw new ApiError('身份检查失败，请重试。', 'IDENTITY_UNVERIFIED')
  if (auth.user?.id !== expectedUserId) throw new ApiError('当前账号已变化，请在新页面重新填写。', 'IDENTITY_CHANGED')
}
export function acceptLogin(result) {
  ++checkSequence
  applyUser(result.user)
  setCsrf(result.csrf_token)
  auth.initialized = true
  auth.checking = false
  auth.checkError = ''
  broadcast()
}
export async function endSession() {
  ++checkSequence
  applyUser(null)
  auth.checking = false
  auth.checkError = ''
  setCsrf(null)
  broadcast()
  await refreshCsrf()
}
export async function logoutCurrentSession() {
  if (!await checkIdentity() || auth.checking || auth.checkError) throw new ApiError('身份检查失败，请重试。', 'IDENTITY_UNVERIFIED')
  await api('/auth/logout', { method: 'POST' })
  await endSession()
}
function broadcast() { channel?.postMessage({ type: 'identity-may-have-changed' }) }
export function startIdentityWatch() {
  if (typeof BroadcastChannel !== 'undefined') {
    channel = new BroadcastChannel('freshplate-auth')
    channel.onmessage = () => { void checkIdentity() }
  }
  window.addEventListener('focus', () => { void checkIdentity() })
  document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible') void checkIdentity() })
}
configureApi({
  generation: () => auth.generation,
  unauthorized: () => { ++checkSequence; applyUser(null); auth.checking = false; auth.checkError = '' },
  passwordRequired: () => { if (auth.user) applyUser({ ...auth.user, must_change_password: true }) },
  recover: async () => { await checkIdentity(); await refreshCsrf() },
})
