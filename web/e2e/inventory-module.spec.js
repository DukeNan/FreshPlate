// 库存模块 E2E（INV-E2E-001~010）。
// 日期均以 UTC 日历日构造（服务端按 UTC 判断临期/过期）；
// 数量全程字符串断言，不做浮点解析。
import {
  test, expect, fixture, loggedInPage, ensureFood, setFoodStatus, utcDate,
  successMessage, formAlert, acceptNextDialog, observeNextDialog,
} from './three-modules.mjs'

let admin, userA, userB
const seeds = {}
let today, plusFour

test.beforeAll(async ({ browser, playwright }) => {
  today = utcDate(0)
  plusFour = utcDate(4)
  admin = await loggedInPage(browser, playwright, fixture.admin)
  userA = await loggedInPage(browser, playwright, fixture.users.A)
  userB = await loggedInPage(browser, playwright, fixture.users.B)
  seeds.complete = await ensureFood(admin, fixture.foods.complete)
  seeds.disposable = await ensureFood(admin, fixture.foods.disposable)
})

test.afterAll(async () => {
  await Promise.all([admin, userA, userB].map(session => session?.close()))
})

// 在新建表单中选定指定食材（表单内搜索，避开选项加载时序）。
async function pickFood(form, food) {
  await form.getByLabel('搜索食材').fill(food.name)
  await form.getByRole('button', { name: '搜索', exact: true }).click()
  await form.getByLabel('选择食材').selectOption(String(food.id))
}

test('INV-E2E-001 新增多批同食材：独立保存、字符串数量原样提交、状态徽标', async () => {
  const page = userA.page
  const bodies = []
  page.on('request', request => {
    if (request.method() === 'POST' && request.url().endsWith('/api/inventory-items')) bodies.push(request.postDataJSON())
  })
  await page.goto('/inventory')
  await page.getByRole('button', { name: '新建库存批次' }).click()
  let form = page.getByRole('region', { name: '库存批次表单' })
  // 非法数量（四位小数）：客户端拦截且不发出请求。
  await pickFood(form, seeds.complete)
  await form.getByLabel(/数量/).fill('1.0001')
  await form.getByRole('button', { name: '创建批次' }).click()
  await expect(formAlert(page)).toContainText('数量只能为数字（至多三位小数）')
  expect(bodies).toHaveLength(0)
  // 第一批：今日到期（临期上界，徽标「今日到期」）。
  await form.getByLabel(/数量/).fill('6')
  await form.getByLabel('到期日（留空表示未知）').fill(today)
  await form.getByLabel('备注（至多 500 字）').fill('今日到期批次')
  await form.getByRole('button', { name: '创建批次' }).click()
  await expect(successMessage(page)).toContainText('已创建')
  // 数量以字符串原样进入 payload，未被浮点转换（INV-WEB-001）。
  expect(bodies).toHaveLength(1)
  expect(typeof bodies[0].quantity).toBe('string')
  expect(bodies[0].quantity).toBe('6')
  // 第二批：0.1 无到期日（unknown），验证 0.1 不经浮点。
  await page.getByRole('button', { name: '新建库存批次' }).click()
  form = page.getByRole('region', { name: '库存批次表单' })
  await pickFood(form, seeds.complete)
  await form.getByLabel(/数量/).fill('0.1')
  await form.getByLabel('备注（至多 500 字）').fill('无日期批次')
  await form.getByRole('button', { name: '创建批次' }).click()
  await expect(successMessage(page)).toContainText('已创建')
  expect(bodies[1].quantity).toBe('0.1')
  // 第三批：+4 天（fresh）。
  await page.getByRole('button', { name: '新建库存批次' }).click()
  form = page.getByRole('region', { name: '库存批次表单' })
  await pickFood(form, seeds.complete)
  await form.getByLabel(/数量/).fill('250')
  await form.getByLabel('到期日（留空表示未知）').fill(plusFour)
  await form.getByLabel('备注（至多 500 字）').fill('新鲜批次')
  await form.getByRole('button', { name: '创建批次' }).click()
  await expect(successMessage(page)).toContainText('已创建')
  // 列表出现三行独立批次（不合并），徽标为文字 + 颜色（INV-WEB-012）。
  const todayRow = page.getByRole('row').filter({ hasText: today })
  const freshRow = page.getByRole('row').filter({ hasText: plusFour })
  const unknownRow = page.getByRole('row').filter({ hasText: '0.1 克' })
  await expect(todayRow).toContainText('6 克')
  await expect(todayRow).toContainText('今日到期')
  await expect(freshRow).toContainText('250 克')
  await expect(freshRow).toContainText('新鲜')
  await expect(unknownRow).toContainText('未知')
  await expect(page.getByText('共 3 条')).toBeVisible()
})

test('INV-E2E-002 状态筛选临期：仅显示 expiring 批次', async () => {
  const page = userA.page
  await page.goto('/inventory')
  await page.getByLabel('状态').selectOption('expiring')
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await expect(page.getByRole('row').filter({ hasText: today })).toBeVisible()
  await expect(page.locator('tbody tr')).toHaveCount(1)
  await expect(page.getByText('共 1 条')).toBeVisible()
  // 对照组：fresh 筛选只命中 +4 天批次。
  await page.getByLabel('状态').selectOption('fresh')
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await expect(page.getByRole('row').filter({ hasText: plusFour })).toBeVisible()
  await expect(page.locator('tbody tr')).toHaveCount(1)
})

test('INV-E2E-003 编辑批次：数量/备注/到期日更新，食材与单位锁定', async () => {
  const page = userA.page
  await page.goto('/inventory')
  const row = page.getByRole('row').filter({ hasText: today })
  await row.getByRole('button', { name: '编辑' }).click()
  const form = page.getByRole('region', { name: '库存批次表单' })
  await expect(form.getByLabel('食材（锁定，不可修改）')).toBeDisabled()
  await expect(form.getByLabel('食材（锁定，不可修改）')).toHaveValue(/E2E鸡胸肉/)
  await expect(form.getByLabel('单位（锁定，不可修改）')).toBeDisabled()
  await form.getByLabel(/数量/).fill('4.5')
  await form.getByLabel('到期日（留空表示未知）').fill(utcDate(2))
  await form.getByLabel('备注（至多 500 字）').fill('已编辑备注')
  await form.getByRole('button', { name: '保存修改' }).click()
  await expect(successMessage(page)).toContainText('版本已更新为 v2')
  // 列表刷新显示新值；+2 天为「临期」而非「今日到期」。
  const editedRow = page.getByRole('row').filter({ hasText: utcDate(2) })
  await expect(editedRow).toContainText('4.5 克')
  await expect(editedRow).toContainText('临期')
})

test('INV-E2E-004 设 0 表示用完：批次保留且标记 depleted', async () => {
  const page = userA.page
  await page.goto('/inventory')
  const row = page.getByRole('row').filter({ hasText: utcDate(2) })
  await row.getByRole('button', { name: '编辑' }).click()
  const form = page.getByRole('region', { name: '库存批次表单' })
  await form.getByLabel(/数量/).fill('0')
  await form.getByRole('button', { name: '保存修改' }).click()
  await expect(successMessage(page)).toContainText('版本已更新为 v3')
  // 用「已用完」徽标定位该批次（「0 克」会同时子串命中「250 克」）。
  const depletedRow = page.getByRole('row').filter({ hasText: '已用完' })
  await expect(depletedRow).toBeVisible()
  await expect(depletedRow).toContainText('0 克')
  // 数量为 0 优先于日期状态，不再显示临期/过期警告。
  await expect(depletedRow).not.toContainText('临期')
  await expect(depletedRow).not.toContainText('今日到期')
})

test('INV-E2E-005 删除批次：确认框显示食材与剩余数量，删除后消失', async () => {
  const page = userA.page
  await page.goto('/inventory')
  const row = page.getByRole('row').filter({ hasText: '已用完' })
  let dialogText = ''
  observeNextDialog(page, message => { dialogText = message })
  await row.getByRole('button', { name: '删除' }).click()
  expect(dialogText).toContain('E2E鸡胸肉')
  expect(dialogText).toContain('剩余数量 0')
  await expect(successMessage(page)).toContainText('已删除')
  await expect(page.getByRole('row').filter({ hasText: '已用完' })).toHaveCount(0)
})

test('INV-E2E-006 用户 B 看不到用户 A 的任何批次', async () => {
  const other = userB.page
  await other.goto('/inventory')
  await expect(other.getByText('没有匹配的库存批次')).toBeVisible()
  await expect(other.getByText('共 0 条')).toBeVisible()
  await expect(other.getByRole('row').filter({ hasText: 'E2E鸡胸肉' })).toHaveCount(0)
  await expect(other.getByRole('row').filter({ hasText: 'E2E停用材' })).toHaveCount(0)
})

test('INV-E2E-007 管理员停用食材后：减量成功、增量被 409 拒绝且草稿保留', async () => {
  const page = userA.page
  await page.goto('/inventory')
  await page.getByRole('button', { name: '新建库存批次' }).click()
  const form = page.getByRole('region', { name: '库存批次表单' })
  await pickFood(form, seeds.disposable)
  await form.getByLabel(/数量/).fill('5')
  await form.getByRole('button', { name: '创建批次' }).click()
  await expect(successMessage(page)).toContainText('已创建')
  // 管理员经 API 停用该食材（启停 UI 已由 food-module 覆盖）。
  await setFoodStatus(admin, seeds.disposable.id, 'disabled')
  await page.goto('/inventory')
  const row = page.getByRole('row').filter({ hasText: 'E2E停用材' })
  await expect(row).toContainText('已停用')
  await row.getByRole('button', { name: '编辑' }).click()
  // 停用提示：只允许减少数量。
  await expect(form.getByRole('alert')).toContainText('只允许减少数量')
  await form.getByLabel(/数量/).fill('3')
  await form.getByRole('button', { name: '保存修改' }).click()
  await expect(successMessage(page)).toContainText('版本已更新为 v2')
  // 增量：前端提示 + 服务端 409 FOOD_DISABLED，表单内容保留。
  await row.getByRole('button', { name: '编辑' }).click()
  await form.getByLabel(/数量/).fill('10')
  await expect(form.getByText('不允许把数量改得比当前更多')).toBeVisible()
  await form.getByRole('button', { name: '保存修改' }).click()
  await expect(form.locator('form').getByRole('alert')).toContainText('只允许减少数量')
  await expect(form.getByLabel(/数量/)).toHaveValue('10')
})

test('INV-E2E-008 两标签页编辑同一批次：旧版本保存收到 409 并可重载', async () => {
  const page = userA.page
  await page.goto('/inventory')
  const row = page.getByRole('row').filter({ hasText: plusFour })
  await row.getByRole('button', { name: '编辑' }).click()
  const firstForm = page.getByRole('region', { name: '库存批次表单' })
  await expect(firstForm.getByLabel(/数量/)).toHaveValue('250')
  // 标签页 2 基于同一旧版本打开编辑。
  const other = await page.context().newPage()
  await other.goto('/inventory')
  await other.getByRole('row').filter({ hasText: plusFour }).getByRole('button', { name: '编辑' }).click()
  const secondForm = other.getByRole('region', { name: '库存批次表单' })
  await expect(secondForm.getByLabel(/数量/)).toHaveValue('250')
  // 标签页 1 保存成功（version→2）。
  await firstForm.getByLabel('备注（至多 500 字）').fill('标签页一先保存')
  await firstForm.getByRole('button', { name: '保存修改' }).click()
  await expect(successMessage(page)).toContainText('版本已更新为 v2')
  // 标签页 2 原样保存 → 409，草稿保留，可从服务端重载后重提。
  await secondForm.getByLabel('备注（至多 500 字）').fill('标签页二后保存')
  await secondForm.getByRole('button', { name: '保存修改' }).click()
  await expect(secondForm.locator('form').getByRole('alert')).toContainText('从服务端重新加载')
  await expect(secondForm.getByLabel('备注（至多 500 字）')).toHaveValue('标签页二后保存')
  acceptNextDialog(other)
  await secondForm.getByRole('button', { name: '从服务端重新加载' }).click()
  await expect(secondForm.getByText('已加载服务端最新内容')).toBeVisible()
  await expect(secondForm.getByLabel('备注（至多 500 字）')).toHaveValue('标签页一先保存')
  // 修改内容后再保存：内容不变时服务端不升版本（相同内容 PUT 返回原详情）。
  await secondForm.getByLabel('备注（至多 500 字）').fill('标签页二重载后修改')
  await secondForm.getByRole('button', { name: '保存修改' }).click()
  await expect(successMessage(other)).toContainText('版本已更新为 v3')
  await other.close()
})

test('INV-E2E-009 移动端视口可用：列表/新建/编辑无横向溢出且可键盘提交', async () => {
  const page = userA.page
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/inventory')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.getByRole('button', { name: '新建库存批次' }).click()
  const form = page.getByRole('region', { name: '库存批次表单' })
  await pickFood(form, seeds.complete)
  await form.getByLabel(/数量/).fill('0.2')
  await form.getByLabel('备注（至多 500 字）').fill('移动端批次')
  await form.getByRole('button', { name: '创建批次' }).click()
  await expect(successMessage(page)).toContainText('已创建')
  // 编辑并经键盘 Enter 提交。
  const row = page.getByRole('row').filter({ hasText: '0.2 克' })
  await row.getByRole('button', { name: '编辑' }).click()
  await form.getByLabel(/数量/).fill('0.3')
  const save = form.getByRole('button', { name: '保存修改' })
  await save.focus()
  await expect(save).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(successMessage(page)).toContainText('版本已更新为')
})

// INV-E2E-010 删除版本冲突：列表刷新后保留失败提示。
// 场景：页面持有过期 version 时删除 → 409 VERSION_CONFLICT；
// 修复点：先 await load() 再设 listError，错误提示不得被 load() 清除。
test('INV-E2E-010 删除版本冲突：列表刷新后保留失败提示', async () => {
  const page = userA.page
  // 用户 A 经页面外 API 预置批次（v1），记录 id 与 version。
  const created = await userA.api.post('/api/inventory-items', {
    headers: userA.headers,
    data: { food_id: seeds.complete.id, quantity: '12.5', unit: 'g', expires_on: plusFour, note: '删除冲突原始备注' },
  })
  if (!created.ok()) throw new Error(`批次种子失败: HTTP ${created.status()} ${await created.text()}`)
  const batch = (await created.json()).data
  // 页面此刻加载列表（version=v1），随后删除请求将携带该过期版本。
  await page.goto('/inventory')
  const row = page.getByRole('row').filter({ hasText: '12.5 克' })
  await expect(row).toContainText(`#${batch.id}`)
  // 页面外仍以 A 身份 PUT 修改数量与备注，服务端 version 升为 v2。
  const updated = await userA.api.put(`/api/inventory-items/${batch.id}`, {
    headers: userA.headers,
    data: { quantity: '8', expires_on: plusFour, note: '页面外修改后的备注', version: batch.version },
  })
  if (!updated.ok()) throw new Error(`批次预更新失败: HTTP ${updated.status()} ${await updated.text()}`)
  // 回到页面（不重新导航）删除该批次并确认 → 携带 v1 的 DELETE 返回 409 VERSION_CONFLICT。
  acceptNextDialog(page)
  await row.getByRole('button', { name: '删除' }).click()
  // 页面级错误提示（表单外的 p.error[role=alert]）在列表刷新后仍保留。
  const listError = page.locator('section > p.error[role="alert"]')
  await expect(listError).toBeVisible()
  await expect(listError).toContainText('已被他人修改')
  await expect(successMessage(page)).toHaveCount(0)
  // 批次未被删除：列表中仍存在，数量与版本为页面外更新后的值（8 克 / v2）。
  const refreshed = page.getByRole('row').filter({ hasText: '8 克' })
  await expect(refreshed).toContainText(`#${batch.id}`)
  await expect(refreshed).toContainText('版本 v2')
  // 备注为更新后的值（列表不展示备注列，经 API 核对）。
  const detail = await userA.api.get(`/api/inventory-items/${batch.id}`, { headers: userA.headers })
  if (!detail.ok()) throw new Error(`批次核对失败: HTTP ${detail.status()}`)
  expect((await detail.json()).data.note).toBe('页面外修改后的备注')
})
