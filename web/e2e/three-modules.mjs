// 三模块 E2E 共享辅助（food/inventory/recipe）。
// 登录策略：每个 spec 在 beforeAll 中为每个账号各完成一次 API 登录
// （独立 APIRequestContext，拿 refresh/csrf cookie），再以 storageState
// 派生唯一的浏览器上下文。这样既不消耗 /auth/login 的每 IP 30 次/15 分钟
// 限流，也保证每个 spec 一个全新上下文、不跨 spec 复用登录状态。
// 种子策略：服务端每次运行建全新临时库，因此种子按「先查询、缺失才创建」
// 实现幂等；食材走 /api/admin/foods，食谱走 /api/admin/recipes。
import { readFileSync } from 'node:fs'
import { test as base, expect } from '@playwright/test'

const ORIGIN = 'http://127.0.0.1:18080'
export const fixture = JSON.parse(readFileSync(new URL('./fixtures.json', import.meta.url), 'utf8'))
export { expect }

// 按 UTC 日历日计算偏移日期字符串（YYYY-MM-DD）。服务端按 UTC 判断
// 临期/过期，因此构造「今日到期 / +N 天」日期必须用 getUTC* 系列。
export function utcDate(offsetDays = 0) {
  const now = new Date()
  const day = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()) + offsetDays * 86400000)
  return `${day.getUTCFullYear()}-${String(day.getUTCMonth() + 1).padStart(2, '0')}-${String(day.getUTCDate()).padStart(2, '0')}`
}

export function authHeaders(session) {
  return { Authorization: `Bearer ${session.access_token}`, 'X-Expected-User-ID': String(session.user.id) }
}

// 以已登录状态打开一个浏览器页面；返回 page/context/session 与可复用的
// API 请求上下文（带 Bearer 头），close() 负责整体清理。
let loginAttempts = 0
export async function loggedInPage(browser, playwright, account) {
  const api = await playwright.request.newContext()
  try {
    const csrf = (await (await api.get('/api/auth/csrf')).json()).data.token
    console.error(`[seed] login #${++loginAttempts} ${account.username} @${new Date().toISOString()}`)
    const response = await api.post('/api/auth/login', {
      headers: { Origin: ORIGIN, 'X-CSRF-Token': csrf },
      data: { username: account.username, password: account.password },
    })
    if (!response.ok()) throw new Error(`种子登录失败 ${account.username}: HTTP ${response.status()}`)
    const session = (await response.json()).data
    const state = await api.storageState()
    const context = await browser.newContext({ storageState: state })
    const page = await context.newPage()
    return {
      page, context, session, api,
      headers: authHeaders(session),
      close: async () => { await context.close(); await api.dispose() },
    }
  } catch (error) { await api.dispose(); throw error }
}

// 幂等食材种子：管理列表按名称字面检索，精确匹配到即复用，否则创建。
// 响应统一包着 { data: ... } 信封，取值须经 .data。
export async function ensureFood(admin, def) {
  const query = new URLSearchParams({ q: def.name, page: '1', page_size: '100' })
  const found = await admin.api.get(`/api/admin/foods?${query}`, { headers: admin.headers })
  if (!found.ok()) throw new Error(`食材查询失败: HTTP ${found.status()}`)
  const existing = ((await found.json()).data?.items || []).find(item => item.name === def.name)
  if (existing) return existing
  const created = await admin.api.post('/api/admin/foods', { headers: admin.headers, data: def })
  if (!created.ok()) throw new Error(`食材种子失败 ${def.name}: HTTP ${created.status()} ${await created.text()}`)
  return (await created.json()).data
}

// 从食材详情提炼 PUT 需要的完整输入（foodInput 全字段必填）。
export function foodInputFromDetail(detail) {
  return {
    name: detail.name, status: detail.status,
    energy_kcal_100g: detail.energy_kcal_100g, protein_g_100g: detail.protein_g_100g,
    fat_g_100g: detail.fat_g_100g, carbohydrate_g_100g: detail.carbohydrate_g_100g,
    nutrition_source: detail.nutrition_source, nutrition_reference: detail.nutrition_reference,
    density_g_ml: detail.density_g_ml, piece_weight_g: detail.piece_weight_g, conversion_source: detail.conversion_source,
    allergens: [...(detail.allergens || [])], allergen_complete: detail.allergen_complete, allergen_source: detail.allergen_source,
  }
}

// 服务端启停食材（带乐观并发 version），供库存/食谱用例快速构造前置。
export async function setFoodStatus(admin, foodId, status) {
  const detailResponse = await admin.api.get(`/api/foods/${foodId}`, { headers: admin.headers })
  if (!detailResponse.ok()) throw new Error(`食材详情失败: HTTP ${detailResponse.status()}`)
  const detail = (await detailResponse.json()).data
  const response = await admin.api.put(`/api/admin/foods/${foodId}`, {
    headers: admin.headers,
    data: { ...foodInputFromDetail(detail), status, version: detail.version },
  })
  if (!response.ok()) throw new Error(`食材启停失败 ${foodId}→${status}: HTTP ${response.status()}`)
  return (await response.json()).data
}

// 经管理接口创建公共食谱草稿，published=true 时随即发布。
export async function createPublicRecipe(admin, { name, foodId, published = false }) {
  const body = {
    name, servings: 2, duration_minutes: 15,
    steps: ['把食材煮熟即可。'], source: 'E2E 种子数据',
    ingredients: [{ food_id: foodId, quantity: '100', unit: 'g' }],
    additional_allergens: [], allergen_complete: true, allergen_source: 'E2E 种子核对依据',
  }
  const created = await admin.api.post('/api/admin/recipes', { headers: admin.headers, data: body })
  if (!created.ok()) throw new Error(`食谱种子失败 ${name}: HTTP ${created.status()} ${await created.text()}`)
  const draft = (await created.json()).data
  if (!published) return draft
  const updated = await admin.api.put(`/api/admin/recipes/${draft.id}`, {
    headers: admin.headers, data: { ...body, status: 'published', version: draft.version },
  })
  if (!updated.ok()) throw new Error(`食谱发布失败 ${name}: HTTP ${updated.status()} ${await updated.text()}`)
  return (await updated.json()).data
}

// —— 常用定位/交互辅助 ——

// 成功提示（各视图统一为 p.success[role=status]）。
export const successMessage = page => page.locator('p.success[role="status"]')
// 表单内错误提示（区别于页面级 listError）。
export const formAlert = page => page.locator('form').getByRole('alert')

// 注册一次性对话框接受器；Playwright 默认会 dismiss 对话框，必须先挂监听。
export function acceptNextDialog(page) {
  page.once('dialog', dialog => { dialog.accept().catch(() => {}) })
}
// 注册一次性对话框观察器，捕获确认文案后接受（用于断言确认框内容）。
export function observeNextDialog(page, onMessage) {
  page.once('dialog', async dialog => {
    try { onMessage(dialog.message()) } catch { /* 断言在测试体中进行 */ }
    await dialog.accept().catch(() => {})
  })
}

export const test = base.test
