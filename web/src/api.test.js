import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
function response(status, data) { return new Response(status === 204 ? null : JSON.stringify(status >= 400 ? { error: data } : { data }), { status, headers: { 'Content-Type': 'application/json' } }) }
let api, configureApi, setCsrf
beforeEach(async () => {
  vi.resetModules()
  ;({ api, configureApi, setCsrf } = await import('./api.js'))
  vi.stubGlobal('fetch', vi.fn())
})
afterEach(() => vi.unstubAllGlobals())
describe('HTTP security contract', () => {
  it('uses Cookie, JSON and the explicit form identity snapshot', async () => {
    setCsrf('token')
    fetch.mockResolvedValue(response(204))
    await api('/me/password', { method: 'PUT', expectedUserId: 7, body: { current_password: 'old', new_password: 'new' } })
    expect(fetch).toHaveBeenCalledWith('/api/me/password', expect.objectContaining({ credentials: 'same-origin', method: 'PUT', headers: expect.objectContaining({ 'X-CSRF-Token': 'token', 'X-Expected-User-ID': '7' }) }))
  })
  it('refuses a protected write without a form identity', async () => {
    setCsrf('token')
    await expect(api('/me/preferences', { method: 'PUT', body: {} })).rejects.toMatchObject({ code: 'MISSING_IDENTITY' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it('refreshes on CSRF rejection without replaying a destructive write', async () => {
    setCsrf('old')
    const recover = vi.fn().mockResolvedValue()
    configureApi({ generation: () => 0, recover, unauthorized: vi.fn(), passwordRequired: vi.fn() })
    fetch.mockResolvedValue(response(403, { code: 'CSRF_INVALID', message: 'expired' }))
    await expect(api('/admin/users/3/reset-password', { method: 'POST', expectedUserId: 1, body: { temporary_password: 'secret' } })).rejects.toMatchObject({ code: 'CSRF_INVALID' })
    expect(recover).toHaveBeenCalledOnce()
    expect(fetch).toHaveBeenCalledOnce()
  })
  it('drops a late private response after an account switch', async () => {
    let generation = 1, resolve
    configureApi({ generation: () => generation })
    fetch.mockReturnValue(new Promise(done => { resolve = done }))
    const request = api('/me/preferences')
    generation = 2
    resolve(response(200, { allergens: ['milk'] }))
    await expect(request).rejects.toMatchObject({ code: 'STALE_RESPONSE' })
  })
  it('clears identity on 401 and distinguishes forced password changes', async () => {
    const unauthorized = vi.fn(), passwordRequired = vi.fn()
    configureApi({ generation: () => 0, unauthorized, passwordRequired })
    fetch.mockResolvedValueOnce(response(401, { code: 'AUTH_REQUIRED' })).mockResolvedValueOnce(response(403, { code: 'PASSWORD_CHANGE_REQUIRED' }))
    await expect(api('/me/preferences')).rejects.toMatchObject({ status: 401 })
    await expect(api('/admin/users')).rejects.toMatchObject({ code: 'PASSWORD_CHANGE_REQUIRED' })
    expect(unauthorized).toHaveBeenCalledOnce()
    expect(passwordRequired).toHaveBeenCalledOnce()
  })
  it('does not clear identity when login credentials are rejected', async () => {
    const unauthorized = vi.fn()
    setCsrf('token')
    configureApi({ generation: () => 0, unauthorized })
    fetch.mockResolvedValue(response(401, { code: 'INVALID_CREDENTIALS' }))
    await expect(api('/auth/login', { method: 'POST', body: {} })).rejects.toMatchObject({ code: 'INVALID_CREDENTIALS' })
    expect(unauthorized).not.toHaveBeenCalled()
  })
})
it('a CSRF recovery really fetches /me and a fresh token, then waits for user resubmission', async () => {
  const { acceptLogin, auth } = await import('./auth.js')
  acceptLogin({ user: { id: 1, role: 'user', must_change_password: false }, csrf_token: 'old-token' })
  fetch.mockResolvedValueOnce(response(403, { code: 'CSRF_INVALID' }))
    .mockResolvedValueOnce(response(200, { id: 1, role: 'user', must_change_password: false }))
    .mockResolvedValueOnce(response(200, { token: 'fresh-token' }))
  await expect(api('/me/preferences', { method: 'PUT', expectedUserId: 1, body: {} })).rejects.toMatchObject({ code: 'CSRF_INVALID' })
  expect(fetch.mock.calls.map(call => call[0])).toEqual(['/api/me/preferences', '/api/me', '/api/auth/csrf'])
  expect(auth.user.id).toBe(1)
  fetch.mockResolvedValueOnce(response(200, {}))
  await api('/me/preferences', { method: 'PUT', expectedUserId: 1, body: {} })
  expect(fetch.mock.calls[3][1].headers['X-CSRF-Token']).toBe('fresh-token')
})
