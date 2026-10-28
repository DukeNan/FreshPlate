import {
  test, expect, fixture, loggedInPage, ensureFood, setFoodStatus, utcDate,
} from './three-modules.mjs'

let admin, userA, userB
const food = {}, recipe = {}
const prefix = '推荐验收'
const blankPreferences = {
  allergens: [], excluded_food_ids: [], dietary_preferences: [],
  daily_energy_kcal: null, daily_protein_g: null, daily_fat_g: null, daily_carbohydrate_g: null,
}

async function json(response) {
  expect(response.ok(), `${response.status()} ${await response.text()}`).toBe(true)
  return (await response.json()).data
}
async function recommendations(session, query = '') {
  return json(await session.api.get(`/api/recommendations?${query}`, { headers: session.headers }))
}
async function preferences(session, excluded = []) {
  await json(await session.api.put('/api/me/preferences', {
    headers: session.headers, data: { ...blankPreferences, excluded_food_ids: excluded },
  }))
}
async function inventory(session, foodId, quantity, expiresOn = null) {
  return json(await session.api.post('/api/inventory-items', {
    headers: session.headers,
    data: { food_id: foodId, quantity, unit: 'g', expires_on: expiresOn, note: '推荐独立验收' },
  }))
}
async function clearInventory(session) {
  const list = await json(await session.api.get('/api/inventory-items?page_size=100', { headers: session.headers }))
  for (const item of list.items) {
    const response = await session.api.delete(`/api/inventory-items/${item.id}`, { headers: session.headers, data: { version: item.version } })
    expect(response.status()).toBe(204)
  }
}
async function createRecipe(name, foodId, duration = 15) {
  const body = {
    name, servings: 1, duration_minutes: duration, steps: ['煮熟。'], source: '',
    ingredients: [{ food_id: foodId, quantity: '100', unit: 'g' }],
    additional_allergens: [], allergen_complete: true, allergen_source: '独立验收',
  }
  const draft = await json(await admin.api.post('/api/admin/recipes', { headers: admin.headers, data: body }))
  const published = await json(await admin.api.put(`/api/admin/recipes/${draft.id}`, {
    headers: admin.headers, data: { ...body, status: 'published', version: draft.version },
  }))
  return { ...published, input: body }
}
async function search(page, value) {
  await page.getByLabel('食谱名称').fill(value)
  await page.getByRole('button', { name: '查询 / 刷新推荐', exact: true }).click()
}

test.beforeAll(async ({ browser, playwright }) => {
  admin = await loggedInPage(browser, playwright, fixture.admin)
  userA = await loggedInPage(browser, playwright, fixture.users.A)
  userB = await loggedInPage(browser, playwright, fixture.users.B)
  for (const session of [userA, userB]) {
    await preferences(session)
    await clearInventory(session)
  }
  for (const key of ['临期', '充足', '缺料']) {
    food[key] = await ensureFood(admin, { ...fixture.foods.complete, name: `${prefix}${key}食材` })
    recipe[key] = await createRecipe(`${prefix}${key}食谱`, food[key].id, key === '临期' ? 30 : 5)
  }
})
test.afterAll(async () => {
  for (const session of [userA, userB]) if (session) await preferences(session)
  await Promise.all([admin, userA, userB].map(session => session?.close()))
})

test('RECO-E2E-001 无库存仍展示缺料；录入临期库存后按可做、临期排序，偏好立即过滤', async () => {
  const a = userA.page
  await a.goto('/recommendations')
  await expect(a.getByText('当前没有可用于推荐的库存', { exact: false })).toBeVisible()
  const empty = await recommendations(userA, `q=${prefix}`)
  expect(empty.stats.has_usable_inventory).toBe(false)
  expect(empty.items).toHaveLength(3)
  expect(empty.items.every(item => item.ingredients.every(i => i.state === 'missing'))).toBe(true)
  await inventory(userA, food['临期'].id, '100', utcDate())
  await inventory(userA, food['充足'].id, '100')
  await inventory(userA, food['缺料'].id, '1')
  const result = await recommendations(userA, `q=${prefix}`)
  expect(result.items.map(item => item.recipe.id)).toEqual([recipe['临期'].id, recipe['充足'].id, recipe['缺料'].id])
  expect(result.items[0]).toMatchObject({ can_cook: true, uses_expiring: true, missing_count: 0 })
  expect(result.items[2].ingredients[0]).toMatchObject({ state: 'short', have: '1', lack: '99' })
  await a.reload()
  await expect(a.getByText('当前没有可用于推荐的库存', { exact: false })).toHaveCount(0)
  await search(a, prefix)
  await expect(a.locator('article.card')).toHaveCount(3)
  await expect(a.locator('article.card').filter({ hasText: `${prefix}缺料食谱` })).toContainText('99')
  await a.screenshot({ path: '../docs/recommendation/tasks/recommendations.png', fullPage: true })
  await preferences(userA, [food['临期'].id])
  const filtered = await recommendations(userA, `q=${prefix}`)
  expect(filtered.items.some(item => item.recipe.id === recipe['临期'].id)).toBe(false)
  expect(filtered.stats.filtered_by_restrictions).toBe(result.stats.filtered_by_restrictions + 1)
  await preferences(userA)
  const b = await recommendations(userB, `q=${prefix}`)
  expect(b.stats.has_usable_inventory).toBe(false)
  expect(b.items.every(item => !item.can_cook && item.ingredients[0].state === 'missing')).toBe(true)
})

test('RECO-E2E-002 q 与仅可立即做组合空态保持完整归因，筛选 total 不产生空页', async () => {
  const page = userA.page
  await page.goto('/recommendations')
  await page.getByLabel('库存筛选').selectOption('can_cook')
  for (const query of [`${prefix}不存在`, `${prefix}缺料`]) {
    await search(page, query)
    await expect(page.getByText(`没有匹配「${query}」且可立即做的食谱`, { exact: false })).toBeVisible()
  }
  await search(page, `${prefix}临期`)
  await expect(page.getByText('共 1 个推荐')).toBeVisible()
  await expect(page.getByRole('button', { name: '下一页' })).toBeDisabled()
  const selected = await recommendations(userA, `q=${prefix}临期&availability=can_cook&page_size=1`)
  expect(selected.total).toBe(1)
  expect(selected.items).toHaveLength(1)
  const next = await recommendations(userA, `q=${prefix}临期&availability=can_cook&page_size=1&page=2`)
  expect(next.total).toBe(1)
  expect(next.items).toEqual([])
  expect(next.stats).toEqual(selected.stats)
  const literal = await recommendations(userA, 'q=%25')
  expect(literal.total).toBe(0)
  const invalid = await userA.api.get('/api/recommendations?availability=unknown', { headers: userA.headers })
  expect(invalid.status()).toBe(400)
})

test('RECO-E2E-003 详情版本变化完全丢弃推荐匹配，刷新详情无内存快照', async () => {
  const page = userA.page
  await page.goto('/recommendations')
  await search(page, `${prefix}缺料`)
  await page.getByRole('link', { name: `${prefix}缺料食谱`, exact: true }).click()
  await expect(page.getByRole('region', { name: '推荐匹配快照' })).toBeVisible()
  await page.goto('/recommendations')
  await search(page, `${prefix}缺料`)
  const edited = recipe['缺料']
  await json(await admin.api.put(`/api/admin/recipes/${edited.id}`, {
    headers: admin.headers,
    data: { ...edited.input, duration_minutes: 6, status: 'published', version: edited.version },
  }))
  await page.getByRole('link', { name: `${prefix}缺料食谱`, exact: true }).click()
  await expect(page.getByText('该食谱在推荐加载后已被修改，请返回并刷新推荐', { exact: false })).toBeVisible()
  await expect(page.getByRole('region', { name: '推荐匹配快照' })).toHaveCount(0)
  await expect(page.getByText(/缺.*99/)).toHaveCount(0)
  await page.reload()
  await expect(page.getByText('该食谱在推荐加载后已被修改，请返回并刷新推荐', { exact: false })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '填写烹饪记录' })).toBeVisible()
})

test('RECO-E2E-004 唯一库存食材停用后无可用库存，安全幸存列表全 missing', async () => {
  await inventory(userB, food['临期'].id, '100')
  expect((await recommendations(userB)).stats.has_usable_inventory).toBe(true)
  await setFoodStatus(admin, food['临期'].id, 'disabled')
  const result = await recommendations(userB, `q=${prefix}`)
  expect(result.stats.has_usable_inventory).toBe(false)
  expect(result.items.length).toBeGreaterThan(0)
  expect(result.items.every(item => item.ingredients.every(i => i.state === 'missing'))).toBe(true)
  await userB.page.goto('/recommendations')
  await expect(userB.page.getByText('当前没有可用于推荐的库存', { exact: false })).toBeVisible()
  await search(userB.page, `${prefix}充足`)
  await expect(userB.page.getByText(`${prefix}充足食谱`, { exact: false })).toBeVisible()
  await setFoodStatus(admin, food['临期'].id, 'active')
})


test('RECO-E2E-005 搜索按码点校验，超长搜索不请求，100个补充平面字符可提交', async () => {
  const page = userA.page
  await page.goto('/recommendations')
  const button = page.getByRole('button', { name: '查询 / 刷新推荐', exact: true })
  await expect(button).toBeEnabled()
  const requests = []
  const track = request => {
    if (new URL(request.url()).pathname === '/api/recommendations') requests.push(request.url())
  }
  page.on('request', track)
  try {
    for (const character of ['菜', '𠮷']) {
      const before = requests.length
      await search(page, character.repeat(101))
      await expect(page.getByRole('alert')).toContainText('食谱名称搜索最多 100 个字符')
      expect(requests).toHaveLength(before)
      const query = character.repeat(100)
      const response = page.waitForResponse(result => {
        const url = new URL(result.url())
        return url.pathname === '/api/recommendations' && url.searchParams.get('q') === query
      })
      await search(page, `  ${query}  `)
      expect((await response).status()).toBe(200)
      await expect(button).toBeEnabled()
      await expect(page.getByRole('alert')).toHaveCount(0)
      await expect(page.getByText('无匹配结果', { exact: true })).toBeVisible()
    }
    const normalized = '菜'.repeat(100)
    const nelResponse = page.waitForResponse(result => {
      const url = new URL(result.url())
      return url.pathname === '/api/recommendations' && url.searchParams.get('q') === normalized
    })
    await search(page, `\u0085${normalized}\u0085`)
    expect((await nelResponse).status()).toBe(200)
    await expect(button).toBeEnabled()
    await expect(page.getByRole('alert')).toHaveCount(0)
    expect(new URL(requests.at(-1)).searchParams.get('q')).toBe(normalized)

    const beforeBom = requests.length
    await search(page, `\uFEFF${normalized}`)
    await expect(page.getByRole('alert')).toContainText('食谱名称搜索最多 100 个字符')
    expect(requests).toHaveLength(beforeBom)

    const preserved = `\uFEFF${'菜'.repeat(99)}`
    const bomResponse = page.waitForResponse(result => {
      const url = new URL(result.url())
      return url.pathname === '/api/recommendations' && url.searchParams.get('q') === preserved
    })
    await search(page, preserved)
    expect((await bomResponse).status()).toBe(200)
    await expect(button).toBeEnabled()
    await expect(page.getByRole('alert')).toHaveCount(0)
    expect(new URL(requests.at(-1)).searchParams.get('q')).toBe(preserved)
  } finally { page.off('request', track) }
})

test('RECO-E2E-006 超长查询翻页不请求且保留当前页，修正查询后正常翻页', async () => {
  const page = userA.page
  const requests = []
  const makeItem = id => ({
    recipe: { id, name: `分页校验食谱${String(id).padStart(2, '0')}`, visibility: 'public', servings: 1, duration_minutes: 5, version: 1 },
    can_cook: true, missing_count: 0, uses_expiring: false, ingredients: [],
  })
  const pattern = '**/api/recommendations?*'
  await page.route(pattern, async route => {
    const url = new URL(route.request().url())
    requests.push(url)
    const currentPage = Number(url.searchParams.get('page'))
    const items = currentPage === 1 ? Array.from({ length: 20 }, (_, index) => makeItem(index + 1)) : [makeItem(21)]
    await route.fulfill({ json: { data: { items, total: 21, stats: {
      candidates: 21, filtered_by_restrictions: 0, filtered_incomplete_metadata: 0,
      filtered_disabled_reference: 0, has_usable_inventory: true,
    } } } })
  })
  try {
    await page.goto('/recommendations')
    await expect(page.locator('article.card')).toHaveCount(20)
    await expect(page.getByText('第 1 页', { exact: true })).toBeVisible()
    const before = requests.length
    await search(page, '𠮷'.repeat(101))
    await expect(page.getByRole('alert')).toContainText('食谱名称搜索最多 100 个字符')
    await page.getByRole('button', { name: '下一页', exact: true }).click()
    await expect(page.getByText('第 1 页', { exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: '分页校验食谱01', exact: true })).toBeVisible()
    await expect(page.locator('article.card')).toHaveCount(20)
    expect(requests).toHaveLength(before)
    await search(page, '分页校验')
    await expect(page.getByRole('alert')).toHaveCount(0)
    await page.getByRole('button', { name: '下一页', exact: true }).click()
    await expect(page.getByText('第 2 页', { exact: true })).toBeVisible()
    await expect(page.locator('article.card')).toHaveCount(1)
    await expect(page.getByRole('link', { name: '分页校验食谱21', exact: true })).toBeVisible()
    expect(requests.at(-1).searchParams.get('page')).toBe('2')
    expect(requests.at(-1).searchParams.get('q')).toBe('分页校验')
  } finally { await page.unroute(pattern) }
})
