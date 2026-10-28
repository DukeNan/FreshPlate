<script setup>
import { chinaDate } from '../date.js'
// 库存模块前端单页（方案 §4/§6）：列表筛选与分页、新建、编辑、删除。
// 约定：数量全程使用字符串（inputmode=decimal + 正则预检），绝不经 Number 转换；
// 到期日原样展示不转时区（页面注明按中国标准时间日期判断）；状态徽标为文字 + 颜色，
// 数量为 0 由服务端 expiry_state=depleted 保证优先于任何日期状态。
import { ref, computed, watch, onMounted } from 'vue'
import { api } from '../api.js'
import { auth, verifyForm } from '../auth.js'
import { QUANTITY_RE, quantityMilliString, compareQuantity } from '../quantity.js'

const expectedUserId = auth.user?.id
const generation = auth.generation
// 数量契约（方案 §3.1）：只接受 ASCII 十进制，0 或非零整数开头，至多三位小数。
// QUANTITY_RE / quantityMilliString / compareQuantity 由共享模块 quantity.js
// 提供（INV-WEB-018），与食谱编辑页保持同一实现。
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/
const UNITS = { g: '克', ml: '毫升', piece: '个' }
const STATE_TEXT = { fresh: '新鲜', expiring: '临期', expired: '已过期', unknown: '未知', depleted: '已用完' }
const PAGE_SIZE = 20
// 食材选项分页拉取上限；超过视为「选项过多」，回退为仅名称筛选。
const FOOD_OPTION_LIMIT = 500

// —— 列表状态 ——
const items = ref([]), total = ref(0), page = ref(1)
const filterQ = ref(''), filterFoodId = ref(0), filterState = ref('')
const listError = ref(''), message = ref(''), loading = ref(false), busy = ref(false)
let listSequence = 0

// —— 食材选项（food-catalog 仅返回 active 食材）——
const foodOptions = ref([]), foodOptionsTruncated = ref(false), foodOptionsError = ref('')

// —— 表单状态（新建与编辑共用一个区域）——
const formOpen = ref(false), editingId = ref(0)
const formFood = ref(null), formFoodId = ref(0), formUnit = ref('g')
const formQuantity = ref(''), formExpiresOn = ref(''), formNote = ref(''), formVersion = ref(0)
// baseQuantity 记录编辑打开时的原数量字符串，用于「停用食材只允许减量」的前端提示。
const baseQuantity = ref('')
const formError = ref(''), formMessage = ref('')
const foodSearchQ = ref(''), foodChoices = ref([]), foodChoicesTotal = ref(0)
const foodChoicesSearched = ref(false), foodChoicesLoading = ref(false)

function unitName(unit) { return UNITS[unit] || unit }
// 数量字符串 → 千分单位十进制字符串与两个千分串的大小比较均由共享模块
// quantity.js 提供（无前导零，只做字符串运算）。
// 状态徽标：expiring 且到期日等于中国标准时间今日时显示「今日到期」，其余按服务端状态。
function stateLabel(item) {
  if (item.expiry_state === 'expiring') return item.expires_on === chinaDate() ? '今日到期' : '临期'
  return STATE_TEXT[item.expiry_state] || '未知'
}
function badgeClass(item) {
  if (item.expiry_state === 'expiring' && item.expires_on === chinaDate()) return 'today'
  return item.expiry_state || 'unknown'
}

const noteLength = computed(() => [...formNote.value].length)
const expiryPast = computed(() => !!formExpiresOn.value && formExpiresOn.value < chinaDate())
const expiryInvalid = computed(() => !!formExpiresOn.value && !DATE_RE.test(formExpiresOn.value))
// 停用食材且草稿数量大于原值：给出提示，但仍允许提交（后端是最终裁决，409 FOOD_DISABLED 显示其消息）。
const increaseBlocked = computed(() => {
  if (!editingId.value || formFood.value?.status !== 'disabled') return false
  const next = quantityMilliString(formQuantity.value)
  const base = quantityMilliString(baseQuantity.value)
  return next !== '' && base !== '' && compareQuantity(next, base) > 0
})

// 新建表单内选择变化时同步选中食材对象。
watch(formFoodId, id => { if (!editingId.value) formFood.value = foodChoices.value.find(food => food.id === id) || null })

// —— 列表加载（照 UsersView 的序列防竞态 + generation 检查）——
async function load(reset = false) {
  const keyword = filterQ.value.trim()
  if ([...keyword].length > 100) { listError.value = '名称筛选去除首尾空白后最多 100 个字。'; return }
  if (reset) page.value = 1
  const sequence = ++listSequence
  loading.value = true; listError.value = ''
  try {
    const query = new URLSearchParams({ page: String(page.value), page_size: String(PAGE_SIZE) })
    if (keyword) query.set('q', keyword)
    if (!foodOptionsTruncated.value && filterFoodId.value) query.set('food_id', String(filterFoodId.value))
    if (filterState.value) query.set('expiry_state', filterState.value)
    const result = await api(`/inventory-items?${query}`)
    if (sequence === listSequence && generation === auth.generation) { items.value = result.items; total.value = result.total }
  } catch (e) { if (sequence === listSequence) listError.value = e.message } finally { if (sequence === listSequence) loading.value = false }
}

// 全量分页拉取 active 食材作为筛选项；超过上限则放弃下拉，回退为仅名称筛选。
async function loadFoodOptions() {
  foodOptionsError.value = ''
  const collected = []
  let more = false
  try {
    while (collected.length < FOOD_OPTION_LIMIT) {
      const result = await api(`/food-catalog?page=${collected.length / 100 + 1}&page_size=100`)
      collected.push(...result.items)
      if (result.items.length < 100) break
      if (collected.length >= FOOD_OPTION_LIMIT && result.total > collected.length) { more = true; break }
    }
  } catch { foodOptionsError.value = '食材选项加载失败，请改用名称筛选。' }
  if (more) { foodOptions.value = []; foodOptionsTruncated.value = true }
  else { foodOptions.value = collected; foodOptionsTruncated.value = false }
}

// 新建表单：按名称分页检索 active 食材（第 1 页 100 条，可继续输入缩小范围）。
async function searchFoods() {
  const keyword = foodSearchQ.value.trim()
  if ([...keyword].length > 100) { formError.value = '搜索关键词去除首尾空白后最多 100 个字。'; return }
  foodChoicesLoading.value = true; formError.value = ''
  try {
    const query = new URLSearchParams({ page: '1', page_size: '100' })
    if (keyword) query.set('q', keyword)
    const result = await api(`/food-catalog?${query}`)
    if (generation !== auth.generation) return
    foodChoices.value = result.items
    foodChoicesTotal.value = result.total
    foodChoicesSearched.value = true
    if (formFoodId.value && !result.items.some(food => food.id === formFoodId.value)) formFoodId.value = 0
  } catch (e) { formError.value = e.message } finally { foodChoicesLoading.value = false }
}

function resetForm() {
  editingId.value = 0
  formFood.value = null; formFoodId.value = 0; formUnit.value = 'g'
  formQuantity.value = ''; formExpiresOn.value = ''; formNote.value = ''; formVersion.value = 0
  baseQuantity.value = ''
  formError.value = ''; formMessage.value = ''
}
function openCreate() {
  resetForm()
  foodSearchQ.value = ''
  // 预填已拉取的食材选项；选项过多（未全量拉取）时留空，要求先搜索。
  foodChoices.value = foodOptions.value
  foodChoicesTotal.value = foodOptionsTruncated.value ? 0 : foodOptions.value.length
  foodChoicesSearched.value = foodOptions.value.length > 0
  formOpen.value = true
}
function applyEdit(item) {
  formFood.value = { ...item.food }
  formUnit.value = item.unit
  formQuantity.value = item.quantity
  formExpiresOn.value = item.expires_on || ''
  formNote.value = item.note
  formVersion.value = item.version
  baseQuantity.value = item.quantity
}
function openEdit(item) {
  resetForm()
  editingId.value = item.id
  applyEdit(item)
  formOpen.value = true
  window.scrollTo?.({ top: 0, behavior: 'smooth' })
}
function closeForm() {
  formOpen.value = false
  resetForm()
}
// 409 VERSION_CONFLICT 后从服务端重载该批次，供核对后重新提交（不自动覆盖草稿之外的数据）。
async function reloadEditing() {
  if (!editingId.value) return
  busy.value = true; formError.value = ''; formMessage.value = ''
  try {
    const result = await api(`/inventory-items/${editingId.value}`)
    if (generation !== auth.generation) return
    applyEdit(result)
    formMessage.value = '已加载服务端最新内容，请核对后再提交。'
  } catch (e) { formError.value = e.message } finally { busy.value = false }
}
// 写路径错误提示：网络失败 → 结果不明禁止自动重试；版本冲突 → 保留草稿提示重载；
// 其余（含 400/409 FOOD_DISABLED/503）显示后端消息。
function writeErrorText(e) {
  if (e.code === 'NETWORK_ERROR') return '网络异常，本次操作结果不明：请先刷新列表核对实际状态，不要盲目重复提交。'
  let text = e.message
  if (e.code === 'VERSION_CONFLICT') text += '当前记录可能已被其他页面修改，请点击「从服务端重新加载」核对后再试。'
  return text
}
async function submitForm() {
  formError.value = ''; formMessage.value = ''
  if (expiryInvalid.value) { formError.value = '到期日格式必须为 YYYY-MM-DD，或留空表示未知。'; return }
  if (noteLength.value > 500) { formError.value = '备注最多 500 字。'; return }
  if (!QUANTITY_RE.test(formQuantity.value)) { formError.value = '数量只能为数字（至多三位小数），如 250 或 0.5。'; return }
  if (editingId.value) {
    busy.value = true
    try {
      await verifyForm(expectedUserId)
      if (generation !== auth.generation) return
      // PUT 必须提交全部三个可变字段与当前 version；不接受 food_id/unit。
      const result = await api(`/inventory-items/${editingId.value}`, {
        method: 'PUT', expectedUserId,
        body: { quantity: formQuantity.value, expires_on: formExpiresOn.value || null, note: formNote.value, version: formVersion.value },
      })
      if (generation !== auth.generation) return
      formOpen.value = false
      resetForm()
      message.value = `批次已保存，版本已更新为 v${result.version}。`
      await load()
    } catch (e) { formError.value = writeErrorText(e) } finally { busy.value = false }
  } else {
    if (!formFood.value) { formError.value = '请先搜索并选择食材。'; return }
    // 新建数量必须 >0："0" 或纯零小数在字符串层面判零，不经 Number 转换。
    if (quantityMilliString(formQuantity.value) === '0') { formError.value = '新建批次的数量必须大于 0。'; return }
    busy.value = true
    try {
      await verifyForm(expectedUserId)
      if (generation !== auth.generation) return
      const result = await api('/inventory-items', {
        method: 'POST', expectedUserId,
        body: { food_id: formFood.value.id, quantity: formQuantity.value, unit: formUnit.value, expires_on: formExpiresOn.value || null, note: formNote.value },
      })
      if (generation !== auth.generation) return
      formOpen.value = false
      resetForm()
      message.value = `批次 #${result.id} 已创建。`
      page.value = 1
      await load()
    } catch (e) { formError.value = writeErrorText(e) } finally { busy.value = false }
  }
}
// 删除：确认框先显示食材名与剩余数量；DELETE 带 {"version":N}。
async function remove(item) {
  listError.value = ''; message.value = ''
  if (!window.confirm(`确认删除批次 #${item.id}？食材「${item.food.name}」，剩余数量 ${item.quantity} ${unitName(item.unit)}。删除后不可恢复。`)) return
  busy.value = true
  try {
    await verifyForm(expectedUserId)
    if (generation !== auth.generation) return
    await api(`/inventory-items/${item.id}`, { method: 'DELETE', expectedUserId, body: { version: item.version } })
    if (generation !== auth.generation) return
    message.value = `批次 #${item.id} 已删除。`
    // 删除当前页最后一条后回退到有效页再加载。
    if (items.value.length === 1 && page.value > 1) page.value -= 1
    await load()
  } catch (e) {
    if (e.code === 'NETWORK_ERROR') { listError.value = writeErrorText(e) }
    else {
      // 404（已删除/不存在）与 409（版本已被修改）：先刷新列表取回最新状态，
      // 再写错误提示——load() 开头会清空 listError，若先设提示会被立即清除；
      // 删除失败是用户操作的直接后果，优先展示（若 load 自身失败，其错误被覆盖也可接受）。
      await load()
      listError.value = writeErrorText(e)
    }
  } finally { busy.value = false }
}
onMounted(() => { load(); loadFoodOptions() })
</script>
<template>
<section>
  <p class="eyebrow">我的食材库存</p>
  <h1>我的库存</h1>
  <p class="muted">按批次记录食材的数量、单位与到期日；到期与临期状态按中国标准时间日期判断，到期日当天计为「今日到期」。无到期日 ≠ 不会过期，请自行留意。</p>

  <p v-if="!formOpen" class="actions"><button :disabled="busy || auth.checking || !!auth.checkError" @click="openCreate">新建库存批次</button></p>

  <section v-if="formOpen" class="card" aria-label="库存批次表单">
    <h2>{{ editingId ? `编辑批次 #${editingId}` : '新建库存批次' }}</h2>
    <p v-if="editingId && formFood && formFood.status === 'disabled'" class="notice" role="alert">该食材已停用，只允许减少数量或删除。</p>
    <p v-if="increaseBlocked" class="notice" role="alert">食材已停用：不允许把数量改得比当前更多，提交将被拒绝（以服务端裁决为准）。如需纠错，请减少数量、设为 0（用完）或删除批次。</p>
    <form @submit.prevent="submitForm">
      <fieldset :disabled="busy || auth.checking || !!auth.checkError">
        <template v-if="editingId">
          <label for="edit-food">食材（锁定，不可修改）</label>
          <input id="edit-food" :value="`${formFood ? formFood.name : ''}（#${formFood ? formFood.id : ''}）${formFood && formFood.status === 'disabled' ? ' · 已停用' : ''}`" disabled>
          <label for="edit-unit">单位（锁定，不可修改）</label>
          <input id="edit-unit" :value="unitName(formUnit)" disabled>
        </template>
        <template v-else>
          <label for="food-search">搜索食材</label>
          <div class="actions">
            <input id="food-search" v-model="foodSearchQ" type="text" autocomplete="off" placeholder="按名称字面包含搜索启用中的食材" @keydown.enter.prevent="searchFoods">
            <button type="button" class="quiet" :disabled="foodChoicesLoading" @click="searchFoods">搜索</button>
          </div>
          <label for="food-pick">选择食材</label>
          <select id="food-pick" v-model="formFoodId">
            <option :value="0">{{ foodChoices.length ? '请选择' : '暂无候选，请先搜索' }}</option>
            <option v-for="food in foodChoices" :key="food.id" :value="food.id">{{ food.name }}（#{{ food.id }}）</option>
          </select>
          <small v-if="foodChoicesSearched">共 {{ foodChoicesTotal }} 项启用中食材{{ foodChoicesTotal > foodChoices.length ? '，仅显示前 100 条，请继续输入缩小范围' : '' }}。</small>
          <p v-if="formFood" class="muted">已选食材：{{ formFood.name }}（#{{ formFood.id }}）</p>
          <label for="form-unit">单位</label>
          <select id="form-unit" v-model="formUnit">
            <option value="g">克（g）</option>
            <option value="ml">毫升（ml）</option>
            <option value="piece">个（piece）</option>
          </select>
        </template>
        <label for="form-quantity">数量{{ editingId ? '（0 表示用完）' : '（必须大于 0）' }}</label>
        <input id="form-quantity" v-model="formQuantity" type="text" inputmode="decimal" autocomplete="off" placeholder="如 250 或 0.5">
        <small>只接受数字与至多三位小数；按原样字符串提交，不做浮点换算。</small>
        <label for="form-expires">到期日（留空表示未知）</label>
        <input id="form-expires" v-model="formExpiresOn" type="date">
        <p v-if="expiryInvalid" class="error" role="alert">到期日格式必须为 YYYY-MM-DD，或留空。</p>
        <p v-else-if="expiryPast" class="notice" role="status">到期日早于今日（按中国标准时间日期判断）：允许录入已过期批次，保存后该批次会标记为「已过期」。</p>
        <label for="form-note">备注（至多 500 字）</label>
        <textarea id="form-note" v-model="formNote" rows="3" maxlength="500"></textarea>
        <small>已输入 {{ noteLength }}/500 字。</small>
        <p v-if="formError" class="error" role="alert">{{ formError }}</p>
        <p v-if="formMessage" class="success" role="status">{{ formMessage }}</p>
        <div class="actions">
          <button type="submit" :disabled="busy || auth.checking || !!auth.checkError">{{ busy ? '正在保存…' : editingId ? '保存修改' : '创建批次' }}</button>
          <button v-if="editingId" type="button" class="quiet" :disabled="busy" @click="reloadEditing">从服务端重新加载</button>
          <button type="button" class="quiet" :disabled="busy" @click="closeForm">取消</button>
        </div>
      </fieldset>
    </form>
  </section>

  <form class="filters card" @submit.prevent="load(true)">
    <div><label for="filter-q">食材名称</label><input id="filter-q" v-model="filterQ" autocomplete="off" placeholder="按名称字面包含搜索"></div>
    <div v-if="!foodOptionsTruncated"><label for="filter-food">食材</label><select id="filter-food" v-model="filterFoodId"><option :value="0">全部食材</option><option v-for="food in foodOptions" :key="food.id" :value="food.id">{{ food.name }}</option></select><small v-if="foodOptionsError">{{ foodOptionsError }}</small></div>
    <div v-else><label for="filter-food-placeholder">食材</label><input id="filter-food-placeholder" value="食材较多，请使用名称筛选" disabled></div>
    <div><label for="filter-state">状态</label><select id="filter-state" v-model="filterState"><option value="">全部</option><option value="expiring">临期（含今日到期）</option><option value="expired">已过期</option><option value="depleted">已用完</option><option value="fresh">新鲜</option><option value="unknown">未知到期日</option></select></div>
    <button :disabled="loading || busy">查询</button>
  </form>

  <p v-if="listError" class="error" role="alert">{{ listError }}</p>
  <p v-if="message" class="success" role="status">{{ message }}</p>
  <p v-if="loading" role="status">正在加载库存…</p>

  <div class="table-wrap card">
    <table>
      <caption>我的库存批次，共 {{ total }} 条（状态按中国标准时间日期判断）</caption>
      <thead><tr><th scope="col">食材</th><th scope="col">批次 / 版本</th><th scope="col">数量 / 单位</th><th scope="col">到期日</th><th scope="col">状态</th><th scope="col">操作</th></tr></thead>
      <tbody>
        <tr v-for="item in items" :key="item.id">
          <td>{{ item.food.name }}<small>#{{ item.food.id }}{{ item.food.status === 'disabled' ? ' · 已停用' : '' }}</small></td>
          <td>#{{ item.id }}<small>版本 v{{ item.version }}</small></td>
          <td>{{ item.quantity }} {{ unitName(item.unit) }}</td>
          <td>{{ item.expires_on || '未知' }}</td>
          <td><span class="badge" :class="badgeClass(item)">{{ stateLabel(item) }}</span></td>
          <td><div class="actions"><button class="quiet" :disabled="busy || loading || auth.checking || !!auth.checkError" @click="openEdit(item)">编辑</button><button class="quiet" :disabled="busy || loading || auth.checking || !!auth.checkError" @click="remove(item)">删除</button></div></td>
        </tr>
        <tr v-if="!items.length && !loading"><td colspan="6">没有匹配的库存批次。无到期日 ≠ 不会过期；可用上方「新建库存批次」录入。</td></tr>
      </tbody>
    </table>
  </div>

  <div class="pagination">
    <button class="quiet" :disabled="page <= 1 || loading || busy" @click="page--; load()">上一页</button>
    <span>第 {{ page }} 页</span>
    <button class="quiet" :disabled="page * PAGE_SIZE >= total || loading || busy" @click="page++; load()">下一页</button>
  </div>
</section>
</template>
<style scoped>
/* 本页补充样式：备注多行输入与状态徽标（文字 + 颜色，不只靠颜色表达）。 */
textarea { display: block; width: 100%; padding: 11px 12px; border: 1px solid #aebfb1; border-radius: 7px; background: #fff; color: inherit; font: inherit; }
textarea:focus-visible { outline: 3px solid #97bd64; outline-offset: 3px; }
.badge { display: inline-block; padding: 3px 12px; border-radius: 999px; font-size: 12px; font-weight: 700; border: 1px solid currentColor; white-space: nowrap; }
.badge.fresh { color: #236344; background: #e9f4e6; }
.badge.expiring { color: #8a5a00; background: #f1ecd9; }
.badge.today { color: #fff; background: #b3491c; border-color: #b3491c; }
.badge.expired { color: #9c2d24; background: #fff0eb; }
.badge.depleted { color: #3f5764; background: #e7eef2; }
.badge.unknown { color: #627467; background: #f0f2ef; }
</style>
