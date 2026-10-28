import { randomUUID } from 'node:crypto'
import { test, expect, fixture, loggedInPage, ensureFood, createPublicRecipe, foodInputFromDetail } from './three-modules.mjs'

let admin, userA, userB, food, recipe

async function data(response) {
  expect(response.ok(), `${response.status()} ${await response.text()}`).toBe(true)
  return (await response.json()).data
}

async function batch(session, quantity = '100') {
  return data(await session.api.post('/api/inventory-items', {
    headers: session.headers,
    data: { food_id: food.id, quantity, unit: 'g', expires_on: null, note: '独立验收' },
  }))
}

async function inventory(session, id) {
  const result = await data(await session.api.get('/api/inventory-items?page_size=100', { headers: session.headers }))
  return result.items.find(item => item.id === id)
}

function confirmation(items) {
  return {
    request_key: randomUUID(), recipe_id: recipe.id, recipe_version: recipe.version,
    cooked_servings: '2', eaten_servings: '1', ate_at: '2026-10-20T12:00:00Z', items,
  }
}

function item(batch, quantity) { return { inventory_id: batch.id, version: batch.version, quantity } }
function submit(session, body) { return session.api.post('/api/meals', { headers: session.headers, data: body }) }

test.beforeAll(async ({ browser, playwright }) => {
  admin = await loggedInPage(browser, playwright, fixture.admin)
  userA = await loggedInPage(browser, playwright, fixture.users.A)
  userB = await loggedInPage(browser, playwright, fixture.users.B)
  food = await ensureFood(admin, { ...fixture.foods.complete, name: '记餐独立验收食材' })
  recipe = await createPublicRecipe(admin, { name: '记餐独立验收食谱', foodId: food.id, published: true })
})

test.afterAll(async () => {
  await Promise.all([admin, userA, userB].map(session => session?.close()))
})

test('重复并发提交只记一餐且只扣一次；相同 key 不同内容冲突', async () => {
  const stock = await batch(userA)
  const body = confirmation([item(stock, '40')])
  const responses = await Promise.all([submit(userA, body), submit(userA, body)])
  for (const response of responses) expect(response.ok(), `${response.status()} ${await response.text()}`).toBe(true)
  const ids = await Promise.all(responses.map(async response => (await response.json()).data.id))
  expect(ids[0]).toBe(ids[1])
  expect((await inventory(userA, stock.id)).quantity).toBe('60')
  const conflict = await submit(userA, { ...body, eaten_servings: '2' })
  expect(conflict.status()).toBe(409)
  expect((await inventory(userA, stock.id)).quantity).toBe('60')
  const meals = await data(await userA.api.get('/api/meals?page_size=100', { headers: userA.headers }))
  expect(meals.items.filter(meal => meal.id === ids[0])).toHaveLength(1)
  const otherMeals = await data(await userB.api.get('/api/meals?page_size=100', { headers: userB.headers }))
  expect(otherMeals.items.some(meal => meal.id === ids[0])).toBe(false)
})

test('超量及越权批次失败时全部回滚', async () => {
  const first = await batch(userA)
  const second = await batch(userA, '5')
  const foreign = await batch(userB)
  const before = await data(await userA.api.get('/api/meals?page_size=100', { headers: userA.headers }))
  for (const items of [[item(first, '20'), item(second, '6')], [item(first, '20'), item(foreign, '20')]]) {
    const response = await submit(userA, confirmation(items))
    expect(response.ok()).toBe(false)
    expect((await inventory(userA, first.id)).quantity).toBe('100')
    expect((await inventory(userA, second.id)).quantity).toBe('5')
    expect((await inventory(userB, foreign.id)).quantity).toBe('100')
  }
  const after = await data(await userA.api.get('/api/meals?page_size=100', { headers: userA.headers }))
  expect(after.total).toBe(before.total)
})

test('遗漏食谱原料或违反忌口时不扣库存、不记餐', async () => {
  const extra = await ensureFood(admin, { ...fixture.foods.complete, name: '记餐独立第二食材' })
  const body = {
    name: '记餐独立多原料食谱', servings: 2, duration_minutes: 15, steps: ['煮熟'], source: '',
    ingredients: [{ food_id: food.id, quantity: '50', unit: 'g' }, { food_id: extra.id, quantity: '50', unit: 'g' }],
    additional_allergens: [], allergen_complete: true, allergen_source: '独立验收',
  }
  const draft = await data(await admin.api.post('/api/admin/recipes', { headers: admin.headers, data: body }))
  const published = await data(await admin.api.put(`/api/admin/recipes/${draft.id}`, {
    headers: admin.headers, data: { ...body, status: 'published', version: draft.version },
  }))
  const stock = await batch(userA)
  const request = { ...confirmation([item(stock, '20')]), recipe_id: published.id, recipe_version: published.version }
  const before = await data(await userA.api.get('/api/meals?page_size=100', { headers: userA.headers }))
  expect((await submit(userA, request)).ok()).toBe(false)
  expect((await inventory(userA, stock.id)).quantity).toBe('100')

  const preferences = await data(await userA.api.get('/api/me/preferences', { headers: userA.headers }))
  const original = {
    allergens: preferences.allergens, excluded_food_ids: preferences.excluded_foods.map(food => food.id),
    dietary_preferences: preferences.dietary_preferences, daily_energy_kcal: preferences.daily_energy_kcal,
    daily_protein_g: preferences.daily_protein_g, daily_fat_g: preferences.daily_fat_g,
    daily_carbohydrate_g: preferences.daily_carbohydrate_g,
  }
  try {
    await data(await userA.api.put('/api/me/preferences', {
      headers: userA.headers, data: { ...original, excluded_food_ids: [...original.excluded_food_ids, food.id] },
    }))
    expect((await submit(userA, confirmation([item(stock, '20')]))).ok()).toBe(false)
    expect((await inventory(userA, stock.id)).quantity).toBe('100')
    expect((await data(await userA.api.get('/api/meals?page_size=100', { headers: userA.headers }))).total).toBe(before.total)
  } finally {
    await data(await userA.api.put('/api/me/preferences', { headers: userA.headers, data: original }))
  }
})

test('营养按实际份量计算并保留快照；七天边界及未知值', async () => {
  const summary = async () => data(await userA.api.get('/api/nutrition?end=2026-10-20', { headers: userA.headers }))
  const first = await summary()
  expect(first.days).toHaveLength(7)
  expect(first.days[0]).toMatchObject({ date: '2026-10-14', meals_count: 0, complete: true, energy_kcal: 0 })
  expect(first.days[6]).toMatchObject({ date: '2026-10-20', meals_count: 1, complete: true })
  expect(first.days[6].energy_kcal).toBeCloseTo(23.6)
  expect(first.days[6].protein_g).toBeCloseTo(4.48)

  const detail = await data(await admin.api.get(`/api/foods/${food.id}`, { headers: admin.headers }))
  await data(await admin.api.put(`/api/admin/foods/${food.id}`, {
    headers: admin.headers,
    data: { ...foodInputFromDetail(detail), energy_kcal_100g: 200, version: detail.version },
  }))
  expect((await summary()).days[6].energy_kcal).toBeCloseTo(23.6)

  const unknown = await ensureFood(admin, {
    ...fixture.foods.plain, name: '记餐独立未知营养', allergen_complete: true, allergen_source: '独立验收',
  })
  const unknownRecipe = await createPublicRecipe(admin, { name: '记餐独立未知营养食谱', foodId: unknown.id, published: true })
  const stock = await data(await userA.api.post('/api/inventory-items', {
    headers: userA.headers,
    data: { food_id: unknown.id, quantity: '100', unit: 'g', expires_on: null, note: '独立验收' },
  }))
  const body = { ...confirmation([item(stock, '20')]), recipe_id: unknownRecipe.id, recipe_version: unknownRecipe.version, ate_at: '2026-10-19T16:00:00Z' }
  await data(await submit(userA, body))
  const previousDay = await data(await userA.api.get('/api/nutrition?end=2026-10-19', { headers: userA.headers }))
  expect(previousDay.days[6].meals_count).toBe(0)
  expect((await summary()).days[6]).toMatchObject({
    meals_count: 2, complete: false, energy_kcal: null, protein_g: null, fat_g: null, carbs_g: null,
  })
})

test('浏览器从食谱确认烹饪到记录及营养概览', async () => {
  const stock = await batch(userA)
  const page = userA.page
  await page.goto(`/recipes/${recipe.id}`)
  await page.getByRole('button', { name: '填写烹饪记录' }).click()
  await page.getByLabel('烹饪份数').fill('2')
  await page.getByLabel('食用份数').fill('1')
  await page.getByLabel('就餐时间（中国标准时间）').fill('2026-10-19T12:00')
  await page.getByLabel(new RegExp(`批次 #${stock.id} ·`)).fill('30')
  await page.getByRole('button', { name: '确认烹饪并记餐' }).click()
  await expect(page.getByRole('status').filter({ hasText: '烹饪已确认' })).toBeVisible()
  expect((await inventory(userA, stock.id)).quantity).toBe('70')
  await page.getByRole('link', { name: '查看饮食记录' }).click()
  await expect(page.getByRole('row').filter({ hasText: recipe.name }).first()).toContainText('1')
  await page.goto('/nutrition')
  await page.getByLabel('截止日期（中国标准时间）').fill('2026-10-19')
  await page.getByRole('button', { name: '查询' }).click()
  await expect(page.getByRole('row').filter({ hasText: '2026-10-19' })).toContainText('30')
})

test('响应丢失后重试复用请求且不重复扣减', async () => {
  const stock = await batch(userA)
  const page = userA.page
  const requests = []
  await page.route('**/api/meals', async route => {
    if (route.request().method() !== 'POST') return route.continue()
    requests.push(route.request().postDataJSON())
    if (requests.length === 1) { await route.fetch(); await route.abort('failed') } else await route.continue()
  })
  try {
    await page.goto(`/recipes/${recipe.id}`)
    await page.getByRole('button', { name: '填写烹饪记录' }).click()
    await page.getByLabel(new RegExp(`批次 #${stock.id} ·`)).fill('20')
    await page.getByRole('button', { name: '确认烹饪并记餐' }).click()
    await expect(page.getByRole('alert')).toContainText('保存结果暂时未知')
    await expect(page.getByLabel('烹饪份数')).toBeDisabled()
    await page.getByRole('button', { name: '重试保存' }).click()
    await expect(page.getByRole('status').filter({ hasText: '烹饪已确认' })).toBeVisible()
    expect(requests).toHaveLength(2)
    expect(requests[1]).toEqual(requests[0])
    expect((await inventory(userA, stock.id)).quantity).toBe('80')
  } finally { await page.unroute('**/api/meals') }
})

test('结果未知后的权限错误保持锁定，明确的库存冲突解除锁定', async () => {
  const stock = await batch(userA)
  const page = userA.page
  const requests = []
  await page.route('**/api/meals', async route => {
    if (route.request().method() !== 'POST') return route.continue()
    requests.push(route.request().postDataJSON())
    if (requests.length === 1) return route.abort('failed')
    if (requests.length === 2) return route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ error: { code: 'FORBIDDEN', message: '无权限' } }) })
    if (requests.length === 3) return route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ error: { code: 'FOOD_UNAVAILABLE', message: '食材不可用' } }) })
    if (requests.length === 4) return route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ error: { code: 'DIETARY_RESTRICTION', message: '饮食限制' } }) })
    return route.continue()
  })
  try {
    await page.goto(`/recipes/${recipe.id}`)
    await page.getByRole('button', { name: '填写烹饪记录' }).click()
    await page.getByLabel(new RegExp(`批次 #${stock.id} ·`)).fill('20')
    await page.getByRole('button', { name: '确认烹饪并记餐' }).click()
    await expect(page.getByRole('alert')).toContainText('保存结果暂时未知')
    await page.getByRole('button', { name: '重试保存' }).click()
    await expect(page.getByRole('alert')).toContainText('保存结果暂时未知')
    await expect(page.getByLabel('烹饪份数')).toBeDisabled()
    await page.getByRole('button', { name: '重试保存' }).click()
    await expect(page.getByRole('alert')).toContainText('保存结果暂时未知')
    await page.getByRole('button', { name: '重试保存' }).click()
    await expect(page.getByRole('alert')).toContainText('保存结果暂时未知')
    await expect(page.getByLabel('烹饪份数')).toBeDisabled()
    await data(await userA.api.put(`/api/inventory-items/${stock.id}`, { headers: userA.headers, data: { quantity: '90', expires_on: null, note: '外部修改', version: stock.version } }))
    await page.getByRole('button', { name: '重试保存' }).click()
    await expect(page.getByRole('alert')).toContainText('库存已变化')
    await expect(page.getByLabel('烹饪份数')).toBeEnabled()
    expect(requests).toHaveLength(5)
    for (const request of requests) expect(request).toEqual(requests[0])
    expect((await inventory(userA, stock.id)).quantity).toBe('90')
  } finally { await page.unroute('**/api/meals') }
})

test('营养概览较早的响应不会覆盖最新查询', async () => {
  const page = userA.page
  let release
  const held = new Promise(resolve => { release = resolve })
  let firstSeen
  const firstRequest = new Promise(resolve => { firstSeen = resolve })
  let count = 0
  await page.route('**/api/nutrition?*', async route => {
    const index = ++count
    if (index === 1) { firstSeen(); await held }
    const date = index === 1 ? '2020-01-01' : '2026-10-20'
    await route.fulfill({ json: { data: { days: [{ date, meals_count: 0, energy_kcal: 0, protein_g: 0, fat_g: 0, carbs_g: 0 }] } } })
  })
  try {
    await page.goto('/nutrition')
    await firstRequest
    await page.getByLabel('截止日期（中国标准时间）').fill('2026-10-20')
    await page.locator('form').dispatchEvent('submit')
    await expect(page.getByRole('row').filter({ hasText: '2026-10-20' })).toBeVisible()
    const firstResponse = page.waitForResponse(response => response.url().includes('/api/nutrition?') && !response.url().includes('end=2026-10-20'))
    release()
    await firstResponse
    await expect(page.getByRole('row').filter({ hasText: '2020-01-01' })).toHaveCount(0)
    await expect(page.getByRole('row').filter({ hasText: '2026-10-20' })).toBeVisible()
  } finally { release(); await page.unroute('**/api/nutrition?*') }
})

test('饮食记录较早的响应不会覆盖最新页', async () => {
  const page = userA.page
  let release, firstSeen
  const held = new Promise(resolve => { release = resolve })
  const firstRequest = new Promise(resolve => { firstSeen = resolve })
  let count = 0
  await page.route('**/api/meals?*', async route => {
    const index = ++count
    if (index === 1) { firstSeen(); await held }
    const item = { id: index, recipe_name: index === 1 ? '旧记录' : '新记录', ate_at: '2026-10-20T12:00:00Z', cooked_servings: '2', eaten_servings: '1' }
    await route.fulfill({ json: { data: { items: [item], total: 40 } } })
  })
  try {
    await page.goto('/meals')
    await firstRequest
    await page.getByRole('button', { name: '下一页' }).evaluate(button => { button.disabled = false; button.click() })
    await expect(page.getByRole('row').filter({ hasText: '新记录' })).toBeVisible()
    const firstResponse = page.waitForResponse(response => response.url().includes('/api/meals?page=1&'))
    release()
    await firstResponse
    await expect(page.getByRole('row').filter({ hasText: '旧记录' })).toHaveCount(0)
    await expect(page.getByRole('row').filter({ hasText: '新记录' })).toBeVisible()
  } finally { release(); await page.unroute('**/api/meals?*') }
})

test('结果未知后忌口可恢复时始终复用原请求', async () => {
  const stock = await batch(userA)
  const page = userA.page
  const preferences = await data(await userA.api.get('/api/me/preferences', { headers: userA.headers }))
  const original = {
    allergens: preferences.allergens, excluded_food_ids: preferences.excluded_foods.map(item => item.id),
    dietary_preferences: preferences.dietary_preferences, daily_energy_kcal: preferences.daily_energy_kcal,
    daily_protein_g: preferences.daily_protein_g, daily_fat_g: preferences.daily_fat_g,
    daily_carbohydrate_g: preferences.daily_carbohydrate_g,
  }
  const requests = []
  await page.route('**/api/meals', async route => {
    if (route.request().method() !== 'POST') return route.continue()
    requests.push(route.request().postDataJSON())
    if (requests.length === 1) return route.abort('failed')
    return route.continue()
  })
  try {
    await page.goto(`/recipes/${recipe.id}`)
    await page.getByRole('button', { name: '填写烹饪记录' }).click()
    await page.getByLabel(new RegExp(`批次 #${stock.id} ·`)).fill('20')
    await page.getByRole('button', { name: '确认烹饪并记餐' }).click()
    await expect(page.getByRole('alert')).toContainText('保存结果暂时未知')
    await data(await userA.api.put('/api/me/preferences', { headers: userA.headers, data: { ...original, excluded_food_ids: [...original.excluded_food_ids, food.id] } }))
    await page.getByRole('button', { name: '重试保存' }).click()
    await expect(page.getByRole('alert')).toContainText('保存结果暂时未知')
    await expect(page.getByLabel('烹饪份数')).toBeDisabled()
    expect((await inventory(userA, stock.id)).quantity).toBe('100')
    await data(await userA.api.put('/api/me/preferences', { headers: userA.headers, data: original }))
    await page.getByRole('button', { name: '重试保存' }).click()
    await expect(page.getByRole('status').filter({ hasText: '烹饪已确认' })).toBeVisible()
    expect(requests).toHaveLength(3)
    for (const request of requests) expect(request).toEqual(requests[0])
    expect((await inventory(userA, stock.id)).quantity).toBe('80')
  } finally {
    await data(await userA.api.put('/api/me/preferences', { headers: userA.headers, data: original }))
    await page.unroute('**/api/meals')
  }
})
