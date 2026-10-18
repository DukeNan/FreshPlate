import { beforeEach, afterEach, expect, it, vi } from 'vitest'
function response(status, data) { return new Response(status === 204 ? null : JSON.stringify(status >= 400 ? { error: data } : { data }), { status }) }
let api, configureApi, setCsrf, setAccessToken
beforeEach(async () => {
  vi.resetModules()
  ;({ api, configureApi, setCsrf, setAccessToken } = await import('./api.js'))
  vi.stubGlobal('fetch', vi.fn())
})
afterEach(() => vi.unstubAllGlobals())
it('uses Bearer and form identity on protected writes without CSRF', async () => {
  setCsrf('csrf'); setAccessToken('jwt')
  fetch.mockResolvedValue(response(204))
  await api('/me/password', { method: 'PUT', expectedUserId: 7, body: {} })
  const request = fetch.mock.calls[0][1]
  expect(request.headers).toMatchObject({ Authorization: 'Bearer jwt', 'X-Expected-User-ID': '7' })
  expect(request.headers['X-CSRF-Token']).toBeUndefined()
})
it('refuses a protected write without a form identity', async () => {
  await expect(api('/me/preferences', { method: 'PUT', body: {} })).rejects.toMatchObject({ code: 'MISSING_IDENTITY' })
  expect(fetch).not.toHaveBeenCalled()
})
it('uses CSRF on Cookie endpoints and never includes the Bearer token', async () => {
  setCsrf('csrf'); setAccessToken('jwt')
  fetch.mockResolvedValue(response(204))
  for (const path of ['/auth/login', '/auth/refresh', '/auth/logout']) await api(path, { method: 'POST' })
  for (const [, request] of fetch.mock.calls) {
    expect(request.headers['X-CSRF-Token']).toBe('csrf')
    expect(request.headers.Authorization).toBeUndefined()
  }
})
it('registers without CSRF', async () => {
  fetch.mockResolvedValue(response(200, {}))
  await api('/auth/register', { method: 'POST', body: {} })
  expect(fetch).toHaveBeenCalledOnce()
  expect(fetch.mock.calls[0][1].headers['X-CSRF-Token']).toBeUndefined()
})
it('refreshes an explicitly expired JWT and retries a write exactly once', async () => {
  const refresh = vi.fn(async () => setAccessToken('new'))
  configureApi({ generation: () => 0, refresh })
  setAccessToken('old')
  fetch.mockResolvedValueOnce(response(401, { code: 'ACCESS_TOKEN_EXPIRED' })).mockResolvedValueOnce(response(200, {}))
  await api('/me/preferences', { method: 'PUT', expectedUserId: 1, body: {} })
  expect(refresh).toHaveBeenCalledOnce()
  expect(fetch.mock.calls[1][1].headers.Authorization).toBe('Bearer new')
})
it('does not replay a write after an account changes during refresh', async () => {
  let generation = 0
  configureApi({ generation: () => generation, refresh: async () => { generation++ } })
  fetch.mockResolvedValue(response(401, { code: 'ACCESS_TOKEN_EXPIRED' }))
  await expect(api('/me/preferences', { method: 'PUT', expectedUserId: 1, body: {} })).rejects.toMatchObject({ code: 'STALE_RESPONSE' })
  expect(fetch).toHaveBeenCalledOnce()
})
it('does not refresh twice when the retried access token expires', async () => {
  const refresh = vi.fn(), unauthorized = vi.fn()
  configureApi({ generation: () => 0, refresh, unauthorized })
  fetch.mockImplementation(() => Promise.resolve(response(401, { code: 'ACCESS_TOKEN_EXPIRED' })))
  await expect(api('/me')).rejects.toMatchObject({ code: 'ACCESS_TOKEN_EXPIRED' })
  expect(refresh).toHaveBeenCalledOnce()
  expect(fetch).toHaveBeenCalledTimes(2)
  expect(unauthorized).toHaveBeenCalledOnce()
})
it('does not retry network failures, business failures or invalid authentication', async () => {
  const refresh = vi.fn(), unauthorized = vi.fn()
  configureApi({ generation: () => 0, refresh, unauthorized })
  fetch.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(response(403, { code: 'FORBIDDEN' })).mockResolvedValueOnce(response(401, { code: 'AUTH_REQUIRED' }))
  await expect(api('/me')).rejects.toMatchObject({ code: 'NETWORK_ERROR' })
  await expect(api('/me')).rejects.toMatchObject({ code: 'FORBIDDEN' })
  await expect(api('/me')).rejects.toMatchObject({ code: 'AUTH_REQUIRED' })
  expect(refresh).not.toHaveBeenCalled()
  expect(unauthorized).toHaveBeenCalledOnce()
})
it('drops a late private response after account switching', async () => {
  let generation = 1, resolve
  configureApi({ generation: () => generation })
  fetch.mockReturnValue(new Promise(done => { resolve = done }))
  const request = api('/me/preferences')
  generation++
  resolve(response(200, {}))
  await expect(request).rejects.toMatchObject({ code: 'STALE_RESPONSE' })
})
it('rejects CSRF without recursively refreshing the Cookie endpoint', async () => {
  const recover = vi.fn()
  configureApi({ generation: () => 0, recover })
  setCsrf('old')
  fetch.mockResolvedValue(response(403, { code: 'CSRF_INVALID' }))
  await expect(api('/auth/refresh', { method: 'POST' })).rejects.toMatchObject({ code: 'CSRF_INVALID' })
  expect(recover).not.toHaveBeenCalled()
  expect(fetch).toHaveBeenCalledOnce()
})
