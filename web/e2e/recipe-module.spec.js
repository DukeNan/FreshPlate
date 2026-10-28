// 食谱模块 E2E（REC-E2E-001~005，另并入 REC-SEC-015/SEC-012 的浏览器侧断言）。
// 管理员公共链路与普通用户私有链路各持独立已登录上下文。
import {
  test, expect, fixture, loggedInPage, ensureFood, setFoodStatus, createPublicRecipe,
  successMessage, acceptNextDialog,
} from './three-modules.mjs'

let admin, userA, userB
const seeds = {}

test.beforeAll(async ({ browser, playwright }) => {
  admin = await loggedInPage(browser, playwright, fixture.admin)
  userA = await loggedInPage(browser, playwright, fixture.users.A)
  userB = await loggedInPage(browser, playwright, fixture.users.B)
  seeds.complete = await ensureFood(admin, fixture.foods.complete)
})

test.afterAll(async () => {
  await Promise.all([admin, userA, userB].map(session => session?.close()))
})

// 公共列表按名称检索（scope 默认公共）。
async function searchPublic(page, keyword) {
  await page.getByLabel('名称', { exact: true }).fill(keyword)
  await page.getByRole('button', { name: '查询', exact: true }).click()
}

test('REC-E2E-001 公共食谱发布可见性闭环：草稿不可见→发布可见→撤回不可见', async () => {
  const page = admin.page
  // 管理员创建公共草稿（一个步骤 + 一条标注完整食材）。
  await page.goto('/admin/recipes')
  await page.getByRole('button', { name: '新建公共食谱' }).click()
  await page.getByLabel('名称', { exact: true }).fill('E2E公共番茄汤')
  await page.getByLabel('份数').fill('2')
  await page.getByLabel('预计总耗时（分钟）').fill('15')
  await page.getByRole('button', { name: '添加步骤' }).click()
  await page.getByLabel('第 1 步内容').fill('把番茄与鸡胸肉煮出味。')
  await page.getByLabel('搜索食材').fill('E2E鸡胸肉')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await page.locator('ul.picker li').filter({ hasText: 'E2E鸡胸肉' }).getByRole('button', { name: '添加' }).click()
  await page.getByLabel('E2E鸡胸肉 的数量').fill('200')
  await page.getByRole('button', { name: '保存草稿' }).click()
  await expect(page).toHaveURL(/\/admin\/recipes\/\d+\/edit/)
  // 新建成功后路由切到编辑页会重挂载组件，瞬时的「草稿已创建」提示随之
  // 消失；改用编辑页常驻的状态行断言。
  await expect(page.locator('p.notice')).toContainText('当前状态：草稿（v1）')
  const recipeId = page.url().match(/\/admin\/recipes\/(\d+)\/edit/)[1]

  // 普通用户：公共列表检索不到，详情 404。
  const userPage = userA.page
  await userPage.goto('/recipes')
  await searchPublic(userPage, 'E2E公共番茄汤')
  await expect(userPage.getByText('没有匹配的食谱。')).toBeVisible()
  await userPage.goto(`/recipes/${recipeId}`)
  await expect(userPage.getByRole('heading', { name: '未找到该食谱' })).toBeVisible()

  // 标注不全时发布先展示服务端失败原因。
  await page.getByRole('button', { name: '发布', exact: true }).click()
  await expect(page.locator('form').getByRole('alert')).toContainText('尚未标记完成')
  // 补齐标注并发布。
  await page.getByLabel('已核对本食谱层面的额外风险').check()
  await page.getByLabel('核对依据').fill('E2E 公共食谱核对依据')
  await page.getByRole('button', { name: '发布', exact: true }).click()
  await expect(successMessage(page)).toContainText('已发布')

  // 普通用户公共 tab 可见，详情完整显示。
  await userPage.goto('/recipes')
  await searchPublic(userPage, 'E2E公共番茄汤')
  const row = userPage.getByRole('row').filter({ hasText: 'E2E公共番茄汤' })
  await expect(row).toContainText('已发布')
  await row.getByRole('link', { name: '查看' }).click()
  await expect(userPage.getByRole('heading', { name: 'E2E公共番茄汤' })).toBeVisible()
  await expect(userPage.locator('tr').filter({ hasText: '份数' })).toContainText('2 份')
  await expect(userPage.locator('tr').filter({ hasText: '预计总耗时' })).toContainText('15 分钟')
  await expect(userPage.locator('tr').filter({ hasText: 'E2E鸡胸肉' })).toContainText('200 克')
  await expect(userPage.locator('ol.steps')).toContainText('把番茄与鸡胸肉煮出味。')
  await expect(userPage.locator('tr').filter({ hasText: '有效过敏原' })).toContainText('支持范围内无标注项')

  // 管理员撤回为草稿后普通用户再次不可见。
  await page.getByRole('button', { name: '撤回为草稿' }).click()
  await expect(successMessage(page)).toContainText('「草稿」')
  await userPage.goto('/recipes')
  await searchPublic(userPage, 'E2E公共番茄汤')
  await expect(userPage.getByText('没有匹配的食谱。')).toBeVisible()
  await userPage.goto(`/recipes/${recipeId}`)
  await expect(userPage.getByRole('heading', { name: '未找到该食谱' })).toBeVisible()
})

test('REC-E2E-002 双用户私有食谱隔离：B 详情 404、列表为空，A 可见可编辑', async () => {
  const a = userA.page
  await a.goto('/recipes')
  await a.getByRole('button', { name: '我的食谱', exact: true }).click()
  await a.getByRole('button', { name: '新建我的食谱' }).click()
  await a.getByLabel('名称', { exact: true }).fill('E2E私房菜A')
  await a.getByLabel('份数').fill('1')
  await a.getByLabel('预计总耗时（分钟）').fill('10')
  await a.getByRole('button', { name: '保存草稿' }).click()
  await expect(a).toHaveURL(/\/recipes\/\d+\/edit/)
  const privateId = a.url().match(/\/recipes\/(\d+)\/edit/)[1]

  // 用户 B：直接访问详情 URL 得 404 页；自己的列表为空；公共列表无此食谱。
  const b = userB.page
  await b.goto(`/recipes/${privateId}`)
  await expect(b.getByRole('heading', { name: '未找到该食谱' })).toBeVisible()
  await b.goto('/recipes')
  await b.getByRole('button', { name: '我的食谱', exact: true }).click()
  await expect(b.getByText('共 0 个')).toBeVisible()
  await b.getByRole('button', { name: '公共食谱', exact: true }).click()
  await searchPublic(b, 'E2E私房菜A')
  await expect(b.getByText('没有匹配的食谱。')).toBeVisible()

  // 用户 A 全程可见可编辑。
  await a.goto('/recipes')
  await a.getByRole('button', { name: '我的食谱', exact: true }).click()
  const row = a.getByRole('row').filter({ hasText: 'E2E私房菜A' })
  await expect(row).toBeVisible()
  await row.getByRole('link', { name: '编辑' }).click()
  await expect(a.getByLabel('名称', { exact: true })).toHaveValue('E2E私房菜A')
})

test('REC-E2E-003 私有食谱全生命周期：草稿→完成→归档→恢复→再完成（含纯文本脚本不执行）', async () => {
  const a = userA.page
  const pageErrors = []
  a.on('pageerror', error => pageErrors.push(error.message))
  // REC-SEC-015 浏览器侧：记录 CSP 违规，验证脚本未被当作代码。
  await a.addInitScript(() => {
    window.__cspViolations = []
    document.addEventListener('securitypolicyviolation', event => window.__cspViolations.push(event.violatedDirective))
  })
  await a.goto('/recipes')
  await a.getByRole('button', { name: '我的食谱', exact: true }).click()
  await a.getByRole('button', { name: '新建我的食谱' }).click()
  await a.getByLabel('名称', { exact: true }).fill('E2E私房全周期')
  await a.getByLabel('份数').fill('1')
  await a.getByLabel('预计总耗时（分钟）').fill('20')
  await a.getByLabel('来源（纯文本，可选）').fill('javascript:alert(1) 仅为纯文本示例')
  await a.getByRole('button', { name: '添加步骤' }).click()
  await a.getByLabel('第 1 步内容').fill('<script>window.__xss=true</script>把食材煮熟。')
  await a.getByRole('button', { name: '添加步骤' }).click()
  await a.getByLabel('第 2 步内容').fill('装盘即可。')
  await a.getByLabel('搜索食材').fill('E2E鸡胸肉')
  await a.getByRole('button', { name: '搜索', exact: true }).click()
  await a.locator('ul.picker li').filter({ hasText: 'E2E鸡胸肉' }).getByRole('button', { name: '添加' }).click()
  await a.getByLabel('E2E鸡胸肉 的数量').fill('0.5')
  await a.getByLabel('已核对本食谱层面的额外风险').check()
  await a.getByLabel('核对依据').fill('E2E 私房核对依据')
  await a.getByRole('button', { name: '保存草稿' }).click()
  await expect(a).toHaveURL(/\/recipes\/\d+\/edit/)
  const recipeId = a.url().match(/\/recipes\/(\d+)\/edit/)[1]
  // 归档（确认对话框）。
  acceptNextDialog(a)
  await a.getByRole('button', { name: '归档', exact: true }).click()
  await expect(a.locator('p.notice')).toContainText('当前状态：已归档')
  // 列表徽标显示已归档。
  await a.goto('/recipes')
  await a.getByRole('button', { name: '我的食谱', exact: true }).click()
  const row = a.getByRole('row').filter({ hasText: 'E2E私房全周期' })
  await expect(row).toContainText('已归档')
  // 恢复为草稿后再次标记完成。
  await row.getByRole('link', { name: '编辑' }).click()
  await a.getByRole('button', { name: '恢复为草稿' }).click()
  await expect(a.locator('p.notice')).toContainText('当前状态：草稿')
  await a.getByRole('button', { name: '标记完成' }).click()
  await expect(a.locator('p.notice')).toContainText('当前状态：已完成')
  // 详情：脚本按纯文本渲染，不执行、无 CSP 违规、无页面错误。
  await a.goto(`/recipes/${recipeId}`)
  await expect(a.getByRole('heading', { name: 'E2E私房全周期' })).toBeVisible()
  await expect(a.locator('ol.steps li').filter({ hasText: 'window.__xss' })).toContainText('<script>window.__xss=true</script>把食材煮熟。')
  expect(await a.evaluate(() => window.__xss)).toBeUndefined()
  expect(await a.evaluate(() => window.__cspViolations)).toEqual([])
  expect(pageErrors).toEqual([])
  // 归档取代删除，已完成食谱提供烹饪入口。
  await expect(a.getByRole('button', { name: /删除/ })).toHaveCount(0)
  await expect(a.getByRole('button', { name: '填写烹饪记录' })).toBeVisible()
})

test('REC-E2E-004 版本冲突：草稿保留、提示对比重载、重载后可重新提交', async () => {
  const a = userA.page
  await a.goto('/recipes')
  await a.getByRole('button', { name: '我的食谱', exact: true }).click()
  await a.getByRole('button', { name: '新建我的食谱' }).click()
  await a.getByLabel('名称', { exact: true }).fill('E2E冲突谱')
  await a.getByLabel('份数').fill('2')
  await a.getByLabel('预计总耗时（分钟）').fill('30')
  await a.getByRole('button', { name: '保存草稿' }).click()
  await expect(a).toHaveURL(/\/recipes\/\d+\/edit/)
  // 页面二基于同一版本打开。
  const second = await a.context().newPage()
  await second.goto(a.url())
  await expect(second.getByLabel('名称', { exact: true })).toHaveValue('E2E冲突谱')
  // 页面一保存成功。
  await a.getByLabel('来源（纯文本，可选）').fill('页面一先保存')
  await a.getByRole('button', { name: '保存草稿' }).click()
  await expect(successMessage(a)).toContainText('已保存')
  // 页面二旧版本保存 → 409，本地草稿保留。
  await second.getByLabel('来源（纯文本，可选）').fill('页面二后保存')
  await second.getByRole('button', { name: '保存草稿' }).click()
  await expect(second.locator('form').getByRole('alert')).toContainText('已被他人修改')
  await expect(second.getByLabel('来源（纯文本，可选）')).toHaveValue('页面二后保存')
  // 载入服务端最新内容（有确认对话框）后重新提交成功。
  acceptNextDialog(second)
  await second.getByRole('button', { name: '载入服务端最新内容' }).click()
  await expect(second.getByText('已载入服务端最新内容')).toBeVisible()
  await expect(second.getByLabel('来源（纯文本，可选）')).toHaveValue('页面一先保存')
  await second.getByRole('button', { name: '保存草稿' }).click()
  await expect(successMessage(second)).toContainText('已保存')
  await second.close()
})

test('REC-E2E-005 列表检索/分页、读取时派生警示徽标与移动端可用性', async () => {
  // 经管理接口预置 21 个已发布公共食谱 + 1 个用于徽标的食谱。
  for (let i = 1; i <= 21; i++) {
    await createPublicRecipe(admin, { name: `E2E批量食谱${String(i).padStart(2, '0')}`, foodId: seeds.complete.id, published: true })
  }
  const badgeRecipe = await createPublicRecipe(admin, { name: 'E2E徽标谱', foodId: seeds.complete.id, published: true })
  const a = userA.page
  await a.goto('/recipes')
  await searchPublic(a, 'E2E批量')
  await expect(a.getByText('共 21 个')).toBeVisible()
  await expect(a.locator('tbody tr')).toHaveCount(20)
  await a.getByRole('button', { name: '下一页' }).click()
  await expect(a.getByText('第 2 页')).toBeVisible()
  await expect(a.locator('tbody tr')).toHaveCount(1)
  // 食材停用后读取时派生：列表徽标 + 详情警示（REC-SEC-012 浏览器侧）。
  // 默认列表按 id DESC 只显示前 20 条（批量01 在第 2 页），先检索锁定该行。
  await setFoodStatus(admin, seeds.complete.id, 'disabled')
  await a.goto('/recipes')
  await searchPublic(a, 'E2E批量食谱01')
  await expect(a.getByRole('row').filter({ hasText: 'E2E批量食谱01' })).toContainText('含停用食材')
  await a.goto(`/recipes/${badgeRecipe.id}`)
  await expect(a.getByText('该食谱引用了已停用的食材')).toBeVisible()
  // 移动端：无横向滚动，警示徽标在窄屏可见。
  await a.setViewportSize({ width: 390, height: 844 })
  await a.goto('/recipes')
  expect(await a.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await expect(a.getByText('含停用食材').first()).toBeVisible()
})

// REC-E2E-006 步骤编辑持久化与删除按钮
test('REC-E2E-006 步骤编辑持久化与删除按钮：改步骤可保存可回读，删除步骤/原料行可用', async () => {
  const page = admin.page
  const pageErrors = []
  page.on('pageerror', error => pageErrors.push(error.message))
  // 经管理 API 预置一个含 2 步 2 原料的公共草稿（body 结构参考 createPublicRecipe）。
  const secondFood = await ensureFood(admin, fixture.foods.annotated)
  // 同文件前面的用例可能已停用本用例引用的任一种子食材；建谱种子前全部
  // 强制置回 active（幂等：已 active 时 PUT 走无变化路径直接成功），
  // 避免 FOOD_UNAVAILABLE。
  await setFoodStatus(admin, seeds.complete.id, 'active')
  await setFoodStatus(admin, secondFood.id, 'active')
  const draftBody = {
    name: 'E2E编辑持久化谱', servings: 2, duration_minutes: 15,
    steps: ['第一步：备料。', '第二步：翻炒。'], source: 'E2E 种子数据',
    ingredients: [
      { food_id: seeds.complete.id, quantity: '100', unit: 'g' },
      { food_id: secondFood.id, quantity: '0.5', unit: 'piece' },
    ],
    additional_allergens: [], allergen_complete: true, allergen_source: 'E2E 种子核对依据',
  }
  const created = await admin.api.post('/api/admin/recipes', { headers: admin.headers, data: draftBody })
  if (!created.ok()) throw new Error(`食谱种子失败 E2E编辑持久化谱: HTTP ${created.status()} ${await created.text()}`)
  const draft = (await created.json()).data

  // 编辑页：仅修改步骤文本并保存。
  await page.goto(`/admin/recipes/${draft.id}/edit`)
  await expect(page.getByLabel('第 2 步内容')).toHaveValue('第二步：翻炒。')
  await page.getByLabel('第 2 步内容').fill('第二步：小火慢炒至上色。')
  await page.getByRole('button', { name: '保存草稿' }).click()
  await expect(successMessage(page)).toContainText('已保存')

  // 重新进入编辑页，断言新步骤文本已持久化。
  await page.goto(`/admin/recipes/${draft.id}/edit`)
  await expect(page.getByLabel('第 2 步内容')).toHaveValue('第二步：小火慢炒至上色。')
  await expect(page.getByRole('heading', { name: '步骤（2/30）' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '原料（2/100）' })).toBeVisible()

  // 点一次「删除步骤」与一次「删除原料」：行数减少、无页面错误。
  await page.getByRole('button', { name: '删除第 2 步' }).click()
  await expect(page.getByRole('heading', { name: '步骤（1/30）' })).toBeVisible()
  await page.getByRole('button', { name: `删除原料 ${secondFood.name}` }).click()
  await expect(page.getByRole('heading', { name: '原料（1/100）' })).toBeVisible()

  // 删除后仍可保存成功。
  await page.getByRole('button', { name: '保存草稿' }).click()
  await expect(successMessage(page)).toContainText('已保存')
  expect(pageErrors).toEqual([])
})
