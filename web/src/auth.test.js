import { beforeEach, afterEach, expect, it, vi } from 'vitest'
const user = id => ({ id, username: `user_${id}`, role: 'user', status: 'active', must_change_password: false })
const result = (status, data) => new Response(JSON.stringify(status === 200 ? { data } : { error: { code: 'AUTH_REQUIRED' } }), { status })
let auth, checkIdentity, verifyForm, acceptLogin
beforeEach(async () => {
  vi.resetModules()
  vi.stubGlobal('fetch', vi.fn())
  ;({ auth, checkIdentity, verifyForm, acceptLogin } = await import('./auth.js'))
})
afterEach(() => vi.unstubAllGlobals())
it('restores identity; refuses an old form after another account logs in', async () => {
  fetch.mockResolvedValueOnce(result(200, user(1))).mockResolvedValueOnce(result(200, user(2)))
  await checkIdentity()
  const generation = auth.generation
  await expect(verifyForm(1)).rejects.toMatchObject({ code: 'IDENTITY_CHANGED' })
  expect(auth.user.id).toBe(2)
  expect(auth.generation).toBeGreaterThan(generation)
})
it('uses the newest concurrent identity check and ignores an older answer', async () => {
  let resolveOld
  fetch.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve })).mockResolvedValueOnce(result(200, user(2)))
  const old = checkIdentity()
  await checkIdentity()
  resolveOld(result(200, user(1)))
  await old
  expect(auth.user.id).toBe(2)
  expect(auth.checking).toBe(false)
})
it('a check started before login cannot overwrite the login result', async () => {
  let resolveOld
  fetch.mockReturnValue(new Promise(resolve => { resolveOld = resolve }))
  const old = checkIdentity()
  acceptLogin({ user: user(2), csrf_token: 'new-token' })
  resolveOld(result(401))
  await old
  expect(auth.user.id).toBe(2)
  expect(auth.checkError).toBe('')
})
it('pauses submitting on network failure and permits a successful retry', async () => {
  acceptLogin({ user: user(1), csrf_token: 'token' })
  fetch.mockRejectedValueOnce(new TypeError('offline')).mockResolvedValueOnce(result(200, user(1)))
  await expect(verifyForm(1)).rejects.toMatchObject({ code: 'IDENTITY_UNVERIFIED' })
  expect(auth.user.id).toBe(1)
  expect(auth.checkError).not.toBe('')
  await verifyForm(1)
  expect(auth.checkError).toBe('')
})
it('treats unauthenticated identity as a real transition and clears old forms', async () => {
  acceptLogin({ user: user(1), csrf_token: 'token' })
  const generation = auth.generation
  fetch.mockResolvedValue(result(401))
  await checkIdentity()
  expect(auth.user).toBeNull()
  expect(auth.generation).toBeGreaterThan(generation)
})
it('broadcasts only an identity event, and rechecks on channel/focus/visibility events', async () => {
  const postMessage = vi.fn(), windowListeners = {}, documentListeners = {}
  let instance
  vi.stubGlobal('BroadcastChannel', class {
    constructor(name) { this.name = name; instance = this }
    postMessage = postMessage
  })
  vi.stubGlobal('window', { addEventListener: (name, callback) => { windowListeners[name] = callback } })
  vi.stubGlobal('document', { visibilityState: 'visible', addEventListener: (name, callback) => { documentListeners[name] = callback } })
  const { startIdentityWatch } = await import('./auth.js')
  startIdentityWatch()
  acceptLogin({ user: user(1), csrf_token: 'never-broadcast-this' })
  expect(postMessage).toHaveBeenCalledWith({ type: 'identity-may-have-changed' })
  expect(JSON.stringify(postMessage.mock.calls)).not.toContain('never-broadcast-this')
  fetch.mockResolvedValue(result(200, user(1)))
  instance.onmessage({ data: { type: 'identity-may-have-changed' } })
  expect(auth.checking).toBe(true)
  await vi.waitFor(() => expect(auth.checking).toBe(false))
  windowListeners.focus()
  await vi.waitFor(() => expect(auth.checking).toBe(false))
  documentListeners.visibilitychange()
  await vi.waitFor(() => expect(auth.checking).toBe(false))
  expect(fetch).toHaveBeenCalledTimes(3)
})
it('keeps focus/visibility fallback when BroadcastChannel is unavailable', async () => {
  const callbacks = {}
  vi.stubGlobal('BroadcastChannel', undefined)
  vi.stubGlobal('window', { addEventListener: (name, fn) => { callbacks[name] = fn } })
  vi.stubGlobal('document', { visibilityState: 'visible', addEventListener: (name, fn) => { callbacks[name] = fn } })
  const { startIdentityWatch } = await import('./auth.js')
  startIdentityWatch()
  fetch.mockResolvedValue(result(200, user(1)))
  callbacks.focus()
  await vi.waitFor(() => expect(auth.checking).toBe(false))
  callbacks.visibilitychange()
  await vi.waitFor(() => expect(auth.checking).toBe(false))
  expect(auth.user.id).toBe(1)
  expect(fetch).toHaveBeenCalledTimes(2)
})
it('does not send logout when the required identity recheck fails', async () => {
  acceptLogin({ user: user(1), csrf_token: 'token' })
  fetch.mockRejectedValueOnce(new TypeError('offline'))
  const { logoutCurrentSession } = await import('./auth.js')
  await expect(logoutCurrentSession()).rejects.toMatchObject({ code: 'IDENTITY_UNVERIFIED' })
  expect(fetch.mock.calls.map(call => call[0])).toEqual(['/api/me'])
  expect(auth.checkError).not.toBe('')
})
