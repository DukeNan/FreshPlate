// 食材模块 E2E（FOOD-E2E-001~006）。
// 会话：管理员与普通用户 A 各一个已登录页面（见 three-modules.mjs 的说明），
// 不在用例内反复登录，也不跨 spec 复用登录状态。
import {
  test, expect, fixture, loggedInPage, ensureFood,
  successMessage, acceptNextDialog,
} from './three-modules.mjs'

let admin, user
const seeds = {}

test.beforeAll(async ({ browser, playwright }) => {
  admin = await loggedInPage(browser, playwright, fixture.admin)
  user = await loggedInPage(browser, playwright, fixture.users.A)
  seeds.plain = await ensureFood(admin, fixture.foods.plain)
})

test.afterAll(async () => {
  await Promise.all([admin, user].map(session => session?.close()))
})

test('FOOD-E2E-001 主链路：管理员新增→普通用户检索→停用→旧忌口保留', async () => {
  const page = admin.page
  // 管理员经 UI 新建含完整营养与过敏原标注的食材。
  await page.goto('/admin/foods')
  await page.getByRole('button', { name: '新建食材' }).click()
  const form = page.locator('section.card').filter({ has: page.getByRole('heading', { name: '新建食材' }) })
  await form.getByLabel('名称', { exact: true }).fill('E2E链路食材')
  await form.getByLabel('填写营养信息').check()
  await form.getByLabel('能量（千卡 / 100 克）').fill('260')
  await form.getByLabel('蛋白质（克 / 100 克）').fill('20')
  await form.getByLabel('脂肪（克 / 100 克）').fill('1.5')
  await form.getByLabel('碳水化合物（克 / 100 克）').fill('0.8')
  await form.getByLabel('营养数据来源').fill('E2E链路数据集')
  await form.getByLabel('营养核对条目').fill('E2E-CHAIN-001')
  await form.getByLabel('每毫升对应克数').fill('1.02')
  await form.getByLabel('换算依据').fill('E2E链路换算依据')
  await form.getByLabel('鱼', { exact: true }).check()
  await form.getByLabel('已核对', { exact: false }).check()
  await form.getByLabel('过敏原标注依据').fill('E2E链路核对依据')
  await form.getByRole('button', { name: '创建食材' }).click()
  await expect(successMessage(page)).toContainText('食材「E2E链路食材」已创建')

  // 普通用户在 /foods 检索到，详情显示数值与「已核对」。
  const userPage = user.page
  await userPage.goto('/foods')
  await userPage.getByLabel('搜索名称').fill('E2E链路食材')
  await userPage.getByRole('button', { name: '查询', exact: true }).click()
  const row = userPage.getByRole('row').filter({ hasText: 'E2E链路食材' })
  await expect(row).toBeVisible()
  await row.getByRole('button', { name: '详情' }).click()
  const detail = userPage.getByRole('region', { name: '食材详情' })
  await expect(detail).toBeVisible()
  await expect(detail.locator('tr').filter({ hasText: '能量' })).toContainText('260 千卡 / 100 克')
  await expect(detail.getByText('已核对：鱼')).toBeVisible()

  // 普通用户把该食材设为忌口并保存。
  await userPage.goto('/settings')
  await userPage.getByLabel('E2E链路食材').check()
  await userPage.getByRole('button', { name: '保存完整设置' }).click()
  await expect(successMessage(userPage)).toContainText('已保存')

  // 管理员停用该食材。
  await page.goto('/admin/foods')
  await page.getByLabel('名称', { exact: true }).fill('E2E链路食材')
  await page.getByRole('button', { name: '查询', exact: true }).click()
  const adminRow = page.getByRole('row').filter({ hasText: 'E2E链路食材' })
  await expect(adminRow).toBeVisible()
  acceptNextDialog(page)
  await adminRow.getByRole('button', { name: '停用', exact: true }).click()
  await expect(successMessage(page)).toContainText('已停用')

  // 普通用户：目录不再显示；设置页旧忌口仍勾选且可保存。
  await userPage.goto('/foods')
  await userPage.getByLabel('搜索名称').fill('E2E链路食材')
  await userPage.getByRole('button', { name: '查询', exact: true }).click()
  await expect(userPage.getByText('没有匹配的食材。')).toBeVisible()
  await userPage.goto('/settings')
  const pref = userPage.getByLabel('E2E链路食材')
  await expect(pref).toBeChecked()
  await expect(pref).toBeEnabled()
  await userPage.getByRole('button', { name: '保存完整设置' }).click()
  await expect(successMessage(userPage)).toContainText('已保存')
})

test('FOOD-E2E-002 未知信息明确显示：不把未知当 0，也不默认 1ml=1g', async () => {
  const userPage = user.page
  await userPage.goto('/foods')
  await userPage.getByLabel('搜索名称').fill('E2E神秘果')
  await userPage.getByRole('button', { name: '查询', exact: true }).click()
  const row = userPage.getByRole('row').filter({ hasText: 'E2E神秘果' })
  await expect(row).toBeVisible()
  // 目录状态徽标用文字表达（不只靠颜色）。
  await expect(row).toContainText('未核对')
  await row.getByRole('button', { name: '详情' }).click()
  const detail = userPage.getByRole('region', { name: '食材详情' })
  await expect(detail).toBeVisible()
  // 能量显示「未知」而不是 0 千卡。
  const energyRow = detail.locator('tr').filter({ hasText: '能量' })
  await expect(energyRow).toContainText('未知')
  await expect(energyRow).not.toContainText('千卡')
  // 两个换算行均显示「无法换算」，不默认 1ml=1g。
  await expect(detail.getByText('无法换算')).toHaveCount(2)
  // 过敏原未核对显示「未知（未核对）」。
  await expect(detail.getByText('未知（未核对）')).toBeVisible()
})

test('FOOD-E2E-003 并发修改提示重新加载，后保存方不静默覆盖', async () => {
  const page = admin.page
  await page.goto('/admin/foods')
  await page.getByLabel('名称', { exact: true }).fill('E2E神秘果')
  await page.getByRole('button', { name: '查询', exact: true }).click()
  // 会话 A 打开编辑表单（载入 version=N）。
  await page.getByRole('row').filter({ hasText: 'E2E神秘果' }).getByRole('button', { name: '编辑' }).click()
  const firstForm = page.locator('section.card').filter({ has: page.getByRole('heading', { name: '编辑 E2E神秘果' }) })
  await expect(firstForm.getByLabel('名称', { exact: true })).toHaveValue('E2E神秘果')
  // 会话 B（同账号新标签页）也打开同一食材。
  const other = await page.context().newPage()
  await other.goto('/admin/foods')
  await other.getByLabel('名称', { exact: true }).fill('E2E神秘果')
  await other.getByRole('button', { name: '查询', exact: true }).click()
  await other.getByRole('row').filter({ hasText: 'E2E神秘果' }).getByRole('button', { name: '编辑' }).click()
  const secondForm = other.locator('section.card').filter({ has: other.getByRole('heading', { name: '编辑 E2E神秘果' }) })
  await expect(secondForm.getByLabel('名称', { exact: true })).toHaveValue('E2E神秘果')
  // A 先保存成功（version→N+1）。
  await firstForm.getByLabel('换算依据').fill('A 会话先保存')
  await firstForm.getByRole('button', { name: '保存修改' }).click()
  await expect(successMessage(page)).toContainText('已更新')
  // B 后保存：409 冲突提示 + 草稿保留。
  await secondForm.getByLabel('换算依据').fill('B 会话后保存')
  await secondForm.getByRole('button', { name: '保存修改' }).click()
  await expect(secondForm.locator('form').getByRole('alert')).toContainText('已被他人修改')
  await expect(secondForm.getByLabel('换算依据')).toHaveValue('B 会话后保存')
  // 载入最新数据后重新提交成功。
  await secondForm.getByRole('button', { name: '载入最新数据' }).click()
  await expect(secondForm.getByLabel('换算依据')).toHaveValue('A 会话先保存')
  await secondForm.getByLabel('换算依据').fill('B 重载后再保存')
  await secondForm.getByRole('button', { name: '保存修改' }).click()
  await expect(successMessage(other)).toContainText('已更新')
  await other.close()
})

test('FOOD-E2E-004 编辑与启停确认可键盘操作', async () => {
  const page = admin.page
  // 键盘提交新建表单。
  await page.goto('/admin/foods')
  await page.getByRole('button', { name: '新建食材' }).click()
  const form = page.locator('section.card').filter({ has: page.getByRole('heading', { name: '新建食材' }) })
  await form.getByLabel('名称', { exact: true }).fill('E2E键盘材')
  const create = form.getByRole('button', { name: '创建食材' })
  await create.focus()
  await expect(create).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(successMessage(page)).toContainText('已创建')
  // 键盘提交编辑表单。
  await page.getByLabel('名称', { exact: true }).fill('E2E键盘材')
  await page.getByRole('button', { name: '查询', exact: true }).click()
  const row = page.getByRole('row').filter({ hasText: 'E2E键盘材' })
  await row.getByRole('button', { name: '编辑' }).click()
  const editForm = page.locator('section.card').filter({ has: page.getByRole('heading', { name: '编辑 E2E键盘材' }) })
  await expect(editForm.getByLabel('名称', { exact: true })).toHaveValue('E2E键盘材')
  await editForm.getByLabel('换算依据').fill('键盘修改依据')
  const save = editForm.getByRole('button', { name: '保存修改' })
  await save.focus()
  await expect(save).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(successMessage(page)).toContainText('当前版本 v2')
  // 启停走确认对话框：焦点在按钮上按 Enter 触发，对话框可接受完成。
  const disable = row.getByRole('button', { name: '停用', exact: true })
  await disable.focus()
  await expect(disable).toBeFocused()
  acceptNextDialog(page)
  await page.keyboard.press('Enter')
  await expect(successMessage(page)).toContainText('已停用')
  await expect(row.getByRole('button', { name: '启用', exact: true })).toBeVisible()
})

test('FOOD-E2E-005 资源审计页面联动：创建/停用/启用事件与筛选', async () => {
  const page = admin.page
  await page.goto('/admin/foods')
  await page.getByRole('button', { name: '新建食材' }).click()
  const form = page.locator('section.card').filter({ has: page.getByRole('heading', { name: '新建食材' }) })
  await form.getByLabel('名称', { exact: true }).fill('E2E审计材')
  await form.getByRole('button', { name: '创建食材' }).click()
  await expect(successMessage(page)).toContainText('已创建')
  // 停用 → 启用，共三类事件。
  await page.getByLabel('名称', { exact: true }).fill('E2E审计材')
  await page.getByRole('button', { name: '查询', exact: true }).click()
  const row = page.getByRole('row').filter({ hasText: 'E2E审计材' })
  acceptNextDialog(page)
  await row.getByRole('button', { name: '停用', exact: true }).click()
  await expect(successMessage(page)).toContainText('已停用')
  acceptNextDialog(page)
  await row.getByRole('button', { name: '启用', exact: true }).click()
  await expect(successMessage(page)).toContainText('已启用')
  // 打开资源审计页并按资源 ID + 类型筛选。
  const list = await admin.api.get('/api/admin/foods?q=' + encodeURIComponent('E2E审计材'), { headers: admin.headers })
  const foodId = ((await list.json()).data?.items || []).find(item => item.name === 'E2E审计材').id
  await page.goto('/admin/resource-audit')
  await page.getByLabel('资源类型').selectOption('food')
  await page.getByLabel('资源 ID').fill(String(foodId))
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await expect(page.locator('tbody tr')).toHaveCount(3)
  // 按 id DESC：最新事件（启用）在最前。
  await expect(page.locator('tbody tr').first()).toContainText('food_enabled')
  await expect(page.getByRole('cell', { name: 'food_enabled', exact: true })).toHaveCount(1)
  await expect(page.getByRole('cell', { name: 'food_disabled', exact: true })).toHaveCount(1)
  await expect(page.getByRole('cell', { name: 'food_created', exact: true })).toHaveCount(1)
  // 操作编码筛选生效。
  await page.getByLabel('操作编码').fill('food_disabled')
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await expect(page.locator('tbody tr')).toHaveCount(1)
  await expect(page.getByRole('cell', { name: 'food_disabled', exact: true })).toBeVisible()
  // 账号审计页面不混入资源事件。
  await page.goto('/admin/audit')
  await expect(page.getByRole('cell', { name: 'food_created', exact: true })).toHaveCount(0)
})

test('FOOD-E2E-006 移动端视口可用：普通目录与管理列表无横向滚动', async () => {
  const userPage = user.page
  const page = admin.page
  await userPage.setViewportSize({ width: 390, height: 844 })
  await userPage.goto('/foods')
  expect(await userPage.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await userPage.getByLabel('搜索名称').fill('E2E神秘果')
  await userPage.getByRole('button', { name: '查询', exact: true }).click()
  const row = userPage.getByRole('row').filter({ hasText: 'E2E神秘果' })
  await expect(row).toBeVisible()
  await row.getByRole('button', { name: '详情' }).click()
  await expect(userPage.getByRole('region', { name: '食材详情' })).toBeVisible()
  await expect(userPage.getByText('第 1 页')).toBeVisible()
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/admin/foods')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await expect(page.getByRole('row').filter({ hasText: 'E2E神秘果' })).toBeVisible()
  await expect(page.getByText('第 1 页')).toBeVisible()
})

test('FOOD-E2E-007 编辑对象切换的失败态与保存目标固定', async () => {
  const page = admin.page
  // 食材 A（详情可正常加载）与 B（将被注入详情失败）。
  const foodA = seeds.plain
  const foodB = await ensureFood(admin, fixture.foods.annotated)
  // FOOD-E2E-006 把管理页视口缩到移动端宽度，这里恢复桌面视口。
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.goto('/admin/foods')
  // 先正常编辑 A：详情加载成功后表单持有 A 的数据与版本。
  await page.getByRole('row').filter({ hasText: foodA.name }).getByRole('button', { name: '编辑' }).click()
  const formA = page.locator('section.card').filter({ has: page.getByRole('heading', { name: `编辑 ${foodA.name}` }) })
  await expect(formA.getByLabel('名称', { exact: true })).toHaveValue(foodA.name)
  // 拦截 B 的详情 GET（仅 GET，PUT 不受影响），注入 500。
  const detailUrl = `**/api/foods/${foodB.id}`
  await page.route(detailUrl, async route => {
    if (route.request().method() === 'GET') {
      await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: { message: 'E2E 注入的详情加载失败', code: 'E2E_INJECTED' } }) })
    } else {
      await route.continue()
    }
  })
  // 切换编辑 B：详情失败后表单不得残留 A 的数据，保存按钮不可用（formReady=false）。
  await page.getByRole('row').filter({ hasText: foodB.name }).getByRole('button', { name: '编辑' }).click()
  const formB = page.locator('section.card').filter({ has: page.getByRole('heading', { name: `编辑 ${foodB.name}` }) })
  await expect(formB.getByRole('alert')).toContainText('E2E 注入的详情加载失败')
  await expect(formB.getByLabel('名称', { exact: true })).toHaveValue('')
  await expect(formB.getByRole('button', { name: '保存修改' })).toBeDisabled()
  // 恢复网络后重新编辑 B：详情正常加载，表单为 B 的数据且可保存。
  await page.unroute(detailUrl)
  await page.getByRole('row').filter({ hasText: foodB.name }).getByRole('button', { name: '编辑' }).click()
  await expect(formB.getByLabel('名称', { exact: true })).toHaveValue(foodB.name)
  const saveB = formB.getByRole('button', { name: '保存修改' })
  await expect(saveB).toBeEnabled()
  // 实际保存一次：成功消息应指向 B 而非 A。
  await formB.getByLabel('换算依据').fill('E2E-007 保存目标固定')
  await saveB.click()
  await expect(successMessage(page)).toContainText(`食材「${foodB.name}」已更新`)
})
