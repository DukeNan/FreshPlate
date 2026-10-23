import { test, expect } from '@playwright/test'

test('RECO-COMPAT-001 Swagger UI loads all API operations and executes a same-origin request under the service CSP', async ({ page }) => {
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  page.on('console', message => { if (message.type() === 'error') errors.push(message.text()) })
  const response = await page.goto('/swagger/index.html')
  expect(response.status()).toBe(200)
  expect(response.headers()['content-security-policy']).toContain("script-src 'self'")
  await expect(page.getByRole('heading', { name: 'FreshPlate API', exact: false })).toBeVisible()
  await expect(page.locator('.opblock')).toHaveCount(34)
  await expect(page.locator('.opblock-summary-path').filter({ hasText: '/recommendations' })).toHaveCount(1)
  const csrf = page.locator('.opblock').filter({ hasText: '取得 CSRF 令牌' })
  await csrf.locator('.opblock-summary').click()
  await csrf.getByRole('button', { name: 'Try it out' }).click()
  const apiResponse = page.waitForResponse(response => response.url().endsWith('/api/auth/csrf'))
  await csrf.getByRole('button', { name: 'Execute', exact: true }).click()
  const result = await apiResponse
  expect(result.status()).toBe(200)
  expect((await result.json()).data.token).toBeTruthy()
  await expect(csrf.locator('.responses-inner')).toContainText('200')
  expect(errors).toEqual([])
})
