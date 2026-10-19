<script setup>
// 食谱编辑（方案 §5/§7）：四条路由（/recipes/new、/recipes/:id/edit、
// /admin/recipes/new、/admin/recipes/:id/edit）复用本视图，由路由 props
// 区分模式：admin=true 走 /admin/recipes 系列公共接口，否则走 /recipes
// 系列私有接口。visibility/owner 由模式决定，绝不随表单提交；status 由
// 当前态计算目标态（POST 恒为 draft）。步骤/原料为可排序数组（上移/下移/
// 删除，不用拖拽）；数量全程字符串（至多三位小数），绝不经 Number 换算。
// 写路径错误契约：409 VERSION_CONFLICT 保留草稿并提示对比重载；
// RECIPE_NOT_PUBLISHABLE / INVALID_RECIPE_TRANSITION 原样显示后端 message；
// NETWORK_ERROR 结果不明，不自动重试；账号代次变化丢弃草稿。
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api.js'
import { auth, verifyForm } from '../auth.js'
import { QUANTITY_RE, quantityMilliString } from '../quantity.js'

const props = defineProps({
  admin: { type: Boolean, default: false },
  recipeId: { type: Number, default: 0 },
})
const router = useRouter()
const expectedUserId = auth.user?.id
const generation = auth.generation

const ALLERGENS = { milk: '乳', egg: '蛋', fish: '鱼', crustacean: '甲壳类', mollusc: '软体动物', peanut: '花生', soy: '大豆', wheat: '小麦', tree_nut: '树坚果', sesame: '芝麻' }
const UNITS = [{ value: 'g', label: '克（g）' }, { value: 'ml', label: '毫升（ml）' }, { value: 'piece', label: '个（piece）' }]
// 数量契约（沿用库存/方案 §4）：只接受 ASCII 十进制，0 或非零整数开头，至多三位小数。
// QUANTITY_RE / quantityMilliString 由共享模块 quantity.js 提供（REC-WEB-011）。
// 控制字符：名称/来源一律拒绝（含换行）；步骤仅允许 \n。
const CONTROL_RE = /[\u0000-\u001f\u007f-\u009f]/
const STEP_CONTROL_RE = /[\u0000-\u0009\u000b-\u001f\u007f-\u009f]/

const isEdit = computed(() => props.recipeId > 0)
const base = computed(() => (props.admin ? '/admin/recipes' : '/recipes'))

// —— 表单状态（新建默认：allergen_complete=false，不许预勾选）——
const loading = ref(false), loadError = ref('')
const name = ref(''), servings = ref(''), duration = ref(''), source = ref('')
const steps = ref([])          // [{ uid, text }]，顺序即 position
const ingredients = ref([])    // [{ foodId, foodName, foodStatus, quantity, unit }]
const pickedAllergens = ref([]), allergenComplete = ref(false), allergenSource = ref('')
const status = ref('draft'), version = ref(0)
const busy = ref(false), formError = ref(''), conflict = ref(false), message = ref('')
let uidSeed = 0

// —— 原料挑选（food-catalog 仅返回 active 食材，分页搜索）——
const pickerQ = ref(''), pickerItems = ref([]), pickerTotal = ref(0), pickerPage = ref(1)
const pickerLoading = ref(false), pickerError = ref(''), pickerSearched = ref(false), pickerAdded = ref('')
let pickerSequence = 0

const canPublish = computed(() => isEdit.value && (status.value === 'draft' || status.value === 'published'))
const canArchive = computed(() => isEdit.value && status.value !== 'archived')
const publishLabel = computed(() => (props.admin ? '发布' : '标记完成'))
const draftLabel = computed(() => {
  if (!isEdit.value || status.value === 'draft') return '保存草稿'
  return status.value === 'published' ? '撤回为草稿' : '恢复为草稿'
})
// 状态文案：模式即 visibility——管理员公共链路 published 显示「已发布」，
// 私有链路 published 按方案 §2 显示「已完成」。
const statusLabel = value => (value === 'published' ? (props.admin ? '已发布' : '已完成') : value === 'draft' ? '草稿' : value === 'archived' ? '已归档' : '未知')
// 重复食材即时提示：同一食谱同一食材只能出现一次（UNIQUE(recipe_id,food_id)）。
const duplicateSet = computed(() => {
  const seen = new Set(), duplicates = new Set()
  for (const row of ingredients.value) { if (seen.has(row.foodId)) duplicates.add(row.foodId); seen.add(row.foodId) }
  return duplicates
})

function applyDetail(d) {
  status.value = d.status
  version.value = d.version
  name.value = d.name
  servings.value = String(d.servings)
  duration.value = String(d.duration_minutes)
  source.value = d.source || ''
  steps.value = (d.steps || []).map(text => ({ uid: ++uidSeed, text }))
  ingredients.value = (d.ingredients || []).map(item => ({ foodId: item.food.id, foodName: item.food.name, foodStatus: item.food.status, quantity: item.quantity, unit: item.unit }))
  pickedAllergens.value = [...(d.additional_allergens || [])]
  allergenComplete.value = !!d.allergen_complete
  allergenSource.value = d.allergen_source || ''
}
async function load() {
  loading.value = true; loadError.value = ''
  try {
    const result = await api(`${base.value}/${props.recipeId}`)
    if (generation !== auth.generation) return
    applyDetail(result)
  } catch (e) { if (generation === auth.generation) loadError.value = e.message } finally { loading.value = false }
}

// —— 步骤操作 ——
function addStep() { if (steps.value.length < 30) steps.value.push({ uid: ++uidSeed, text: '' }) }
// 模板传入的 list 已是解包后的数组（steps/ingredients），直接 splice 即可。
function removeAt(list, index) { list.splice(index, 1) }
function move(list, index, delta) {
  const target = index + delta
  if (target < 0 || target >= list.length) return
  const [item] = list.splice(index, 1)
  list.splice(target, 0, item)
}

// —— 原料操作 ——
function inRecipe(foodId) { return ingredients.value.some(row => row.foodId === foodId) }
async function searchFoods(reset = false) {
  const keyword = pickerQ.value.trim()
  if ([...keyword].length > 100) { pickerError.value = '搜索关键词去除首尾空白后最多 100 个字。'; return }
  if (reset) pickerPage.value = 1
  const own = ++pickerSequence
  pickerLoading.value = true; pickerError.value = ''; pickerAdded.value = ''
  try {
    const query = new URLSearchParams({ page: String(pickerPage.value), page_size: '20' })
    if (keyword) query.set('q', keyword)
    const result = await api(`/food-catalog?${query}`)
    if (own === pickerSequence && generation === auth.generation) { pickerItems.value = result.items; pickerTotal.value = result.total; pickerSearched.value = true }
  } catch (e) { if (own === pickerSequence) pickerError.value = e.message } finally { if (own === pickerSequence) pickerLoading.value = false }
}
function pickerMove(delta) {
  const next = pickerPage.value + delta
  if (next < 1 || next * 20 > pickerTotal.value + 19) return
  pickerPage.value = next
  searchFoods()
}
function addIngredient(food) {
  pickerAdded.value = ''
  if (inRecipe(food.id)) { pickerAdded.value = `「${food.name}」已在原料列表中：同一食材在一个食谱里只能出现一次，请直接修改其数量。`; return }
  if (ingredients.value.length >= 100) { pickerAdded.value = '原料最多 100 条。'; return }
  ingredients.value.push({ foodId: food.id, foodName: food.name, foodStatus: food.status || 'active', quantity: '', unit: 'g' })
}
// 数量字符串 → 千分单位字符串（非法输入返回 ''）由共享模块 quantity.js 提供。

// —— 客户端校验：只拦截明显无效输入，最终以后端校验为准；失败不发请求。——
function buildBody() {
  const trimmedName = name.value.trim()
  const nameLen = [...trimmedName].length
  if (nameLen < 1 || nameLen > 100) return { error: '名称去除首尾空白后需为 1~100 个字。' }
  if (CONTROL_RE.test(trimmedName)) return { error: '名称不能包含控制字符。' }
  const servingsNum = Number(servings.value)
  if (!Number.isInteger(servingsNum) || servingsNum < 1 || servingsNum > 100) return { error: '份数需为 1~100 的整数。' }
  const durationNum = Number(duration.value)
  if (!Number.isInteger(durationNum) || durationNum < 1 || durationNum > 1440) return { error: '预计总耗时需为 1~1440 的整数（分钟）。' }
  if (steps.value.length > 30) return { error: '步骤最多 30 条。' }
  const stepTexts = []
  for (let i = 0; i < steps.value.length; i++) {
    const text = steps.value[i].text.trim()
    const len = [...text].length
    if (len < 1 || len > 1000) return { error: `第 ${i + 1} 步去除首尾空白后需为 1~1000 个字（草稿也不允许空步骤）。` }
    if (STEP_CONTROL_RE.test(text)) return { error: `第 ${i + 1} 步包含不允许的控制字符（步骤仅允许换行）。` }
    stepTexts.push(text)
  }
  const trimmedSource = source.value.trim()
  if ([...trimmedSource].length > 1000) return { error: '来源去除首尾空白后最多 1000 个字。' }
  if (CONTROL_RE.test(trimmedSource)) return { error: '来源不能包含控制字符。' }
  if (duplicateSet.value.size) return { error: '存在重复食材：同一食材在一个食谱里只能出现一次，请合并或删除重复项。' }
  if (ingredients.value.length > 100) return { error: '原料最多 100 条。' }
  const items = []
  for (let i = 0; i < ingredients.value.length; i++) {
    const row = ingredients.value[i]
    const quantity = row.quantity.trim()
    if (!QUANTITY_RE.test(quantity)) return { error: `第 ${i + 1} 条原料「${row.foodName}」的数量只能为数字且至多三位小数（如 250 或 0.5），不接受负数或科学计数法。` }
    const milli = quantityMilliString(quantity)
    if (milli === '0') return { error: `第 ${i + 1} 条原料「${row.foodName}」的数量必须大于 0。` }
    if (milli.length > 10 || (milli.length === 10 && milli > '1000000000')) return { error: `第 ${i + 1} 条原料「${row.foodName}」的数量过大（按整份食谱计，最多 1000000）。` }
    items.push({ food_id: row.foodId, quantity, unit: row.unit })
  }
  const trimmedAllergenSource = allergenSource.value.trim()
  if ([...trimmedAllergenSource].length > 500) return { error: '过敏原核对依据最多 500 个字。' }
  if (allergenComplete.value && !trimmedAllergenSource) return { error: '勾选「已核对」时必须填写核对依据（不能为纯空白）。' }
  return { body: { name: trimmedName, servings: servingsNum, duration_minutes: durationNum, steps: stepTexts, source: trimmedSource, ingredients: items, additional_allergens: [...pickedAllergens.value], allergen_complete: allergenComplete.value, allergen_source: trimmedAllergenSource ? trimmedAllergenSource : null } }
}

// action：draft=保存草稿；publish=标记完成/发布；archive=归档。
// 目标 status 按当前态计算：新建走 POST（服务端恒为 draft），不提交 status。
async function save(action) {
  if (busy.value) return
  const built = buildBody()
  if (built.error) { formError.value = built.error; return }
  const body = built.body
  if (isEdit.value) {
    if (!Number.isSafeInteger(version.value) || version.value < 1) { formError.value = '缺少有效的当前版本（version），请重新载入该食谱后再保存。'; return }
    body.status = action === 'publish' ? 'published' : action === 'archive' ? 'archived' : 'draft'
    body.version = version.value
  }
  if (action === 'archive' && !window.confirm(`确认将「${body.name}」归档？归档不会删除，之后可恢复为草稿。`)) return
  busy.value = true; formError.value = ''; conflict.value = false; message.value = ''
  try {
    await verifyForm(expectedUserId)
    if (generation !== auth.generation) return
    let result
    if (isEdit.value) result = await api(`${base.value}/${props.recipeId}`, { method: 'PUT', expectedUserId, body })
    else result = await api(base.value, { method: 'POST', expectedUserId, body })
    if (generation !== auth.generation) return
    if (isEdit.value) {
      applyDetail(result)
      message.value = `已保存，当前状态「${statusLabel(result.status)}」，版本 v${result.version}。`
    } else {
      // 新建成功后切换到编辑路由：之后即可在本页「标记完成/发布」「归档」。
      applyDetail(result)
      message.value = `草稿已创建（#${result.id}）。可继续编辑，或在本页${props.admin ? '发布' : '标记完成'}。`
      await router.replace(`${base.value}/${result.id}/edit`)
    }
  } catch (e) {
    // 账号代次已变化的丢弃逻辑由 api()/verifyForm 抛错承担，这里按错误码分诊。
    if (e.code === 'VERSION_CONFLICT') {
      conflict.value = true
      formError.value = '保存失败：该食谱已被他人修改（版本冲突）。你填写的内容仍保留在表单中，尚未覆盖他人修改；请点击「载入服务端最新内容」对比后再重新提交。'
    } else if (e.code === 'NETWORK_ERROR') {
      formError.value = '网络异常，本次提交结果不明，未自动重试；请先刷新核对实际状态，再决定是否重新提交。'
    } else {
      // 409 RECIPE_NOT_PUBLISHABLE / INVALID_RECIPE_TRANSITION 等原样显示后端 message。
      formError.value = e.message
    }
  } finally { busy.value = false }
}
// 版本冲突后的人工重载：先确认，避免静默覆盖本地草稿。
async function reloadServer() {
  if (!isEdit.value || busy.value) return
  if (!window.confirm('载入服务端最新内容将覆盖你当前未保存的修改，继续吗？')) return
  busy.value = true
  try {
    const result = await api(`${base.value}/${props.recipeId}`)
    if (generation !== auth.generation) return
    applyDetail(result)
    conflict.value = false; formError.value = ''
    message.value = '已载入服务端最新内容，请核对后再提交。'
  } catch (e) { formError.value = e.message } finally { busy.value = false }
}
onMounted(() => { if (isEdit.value) load(); searchFoods(true) })
</script>
<template><section>
  <p class="eyebrow">{{ admin ? '管理员工作台' : '我的食谱' }}</p>
  <h1>{{ isEdit ? (admin ? '编辑公共食谱' : '编辑我的食谱') : (admin ? '新建公共食谱' : '新建我的食谱') }}</h1>
  <p class="muted" v-if="admin">公共食谱不属于任何个人账号；visibility 由管理模式固定为 public，不在表单中提交。</p>
  <p class="muted" v-else>私有食谱仅本人可见；visibility 由页面固定为 private，不在表单中提交。数量按整份食谱填写，不同阶段的用量请合并并在步骤中说明。</p>

  <p v-if="loading" role="status">正在载入食谱…</p>
  <template v-if="loadError">
    <p class="error" role="alert">{{ loadError }} <button type="button" class="quiet" @click="load">重试</button></p>
    <p><RouterLink :to="admin ? '/admin/recipes' : '/recipes'">返回列表</RouterLink></p>
  </template>

  <form v-if="!loading && !loadError" @submit.prevent="save('draft')">
    <fieldset :disabled="busy || auth.checking || !!auth.checkError">
      <legend class="sr-only">食谱表单</legend>
      <p v-if="isEdit" class="notice" role="status">当前状态：{{ statusLabel(status) }}（v{{ version }}）<template v-if="status === 'archived'">；已归档食谱需先「恢复为草稿」才能再次{{ publishLabel }}。</template></p>

      <div class="card">
        <h2>基本信息</h2>
        <div class="grid">
          <div><label for="recipe-name">名称</label><input id="recipe-name" v-model="name" maxlength="100" autocomplete="off"><small>去除首尾空白后 1~100 个字。</small></div>
          <div><label for="recipe-servings">份数</label><input id="recipe-servings" v-model="servings" type="number" min="1" max="100" step="1" inputmode="numeric"><small>1~100 的整数，按整份食谱计。</small></div>
          <div><label for="recipe-duration">预计总耗时（分钟）</label><input id="recipe-duration" v-model="duration" type="number" min="1" max="1440" step="1" inputmode="numeric"><small>1~1440 的整数。</small></div>
          <div><label for="recipe-source">来源（纯文本，可选）</label><input id="recipe-source" v-model="source" maxlength="1000" autocomplete="off"><small>来源说明，按纯文本展示，至多 1000 个字。</small></div>
        </div>
      </div>

      <div class="card">
        <h2>步骤（{{ steps.length }}/30）</h2>
        <p class="muted">纯文本，允许换行；顺序即步骤顺序，使用上移/下移调整。</p>
        <ol class="rows">
          <li v-for="(step, index) in steps" :key="step.uid">
            <div class="row-head"><strong>第 {{ index + 1 }} 步</strong><div class="actions">
              <button type="button" class="quiet" :disabled="index === 0" :aria-label="`上移第 ${index + 1} 步`" @click="move(steps, index, -1)">上移</button>
              <button type="button" class="quiet" :disabled="index === steps.length - 1" :aria-label="`下移第 ${index + 1} 步`" @click="move(steps, index, 1)">下移</button>
              <button type="button" class="quiet" :aria-label="`删除第 ${index + 1} 步`" @click="removeAt(steps, index)">删除</button>
            </div></div>
            <label class="sr-only" :for="`step-text-${step.uid}`">第 {{ index + 1 }} 步内容</label>
            <textarea :id="`step-text-${step.uid}`" v-model="step.text" rows="3" placeholder="描述这一步的操作，可换行"></textarea>
          </li>
        </ol>
        <p v-if="!steps.length" class="muted">还没有步骤。草稿可以先不写；{{ publishLabel }}前至少需要一步。</p>
        <button type="button" class="quiet" :disabled="steps.length >= 30" @click="addStep">添加步骤</button>
      </div>

      <div class="card">
        <h2>原料（{{ ingredients.length }}/100）</h2>
        <p class="muted">从食材目录中选择启用中的食材；已停用食材不可新增，仅编辑既有食谱时保留并警示。</p>
        <div class="actions"><label class="inline" for="picker-q">搜索食材</label><input id="picker-q" v-model="pickerQ" autocomplete="off" placeholder="按名称字面包含搜索启用中的食材" @keydown.enter.prevent="searchFoods(true)"><button type="button" class="quiet" :disabled="pickerLoading" @click="searchFoods(true)">搜索</button></div>
        <p v-if="pickerError" class="error" role="alert">{{ pickerError }}</p>
        <p v-if="pickerAdded" class="notice" role="status">{{ pickerAdded }}</p>
        <ul class="picker"><li v-for="food in pickerItems" :key="food.id"><span>{{ food.name }}<small>#{{ food.id }}</small></span><button type="button" class="quiet" :disabled="inRecipe(food.id)" @click="addIngredient(food)">{{ inRecipe(food.id) ? '已添加' : '添加' }}</button></li></ul>
        <p v-if="pickerSearched && !pickerItems.length && !pickerLoading" class="muted">没有匹配的启用中食材。</p>
        <div class="pagination" v-if="pickerSearched && pickerTotal"><button type="button" class="quiet" :disabled="pickerPage <= 1 || pickerLoading" @click="pickerMove(-1)">上一页</button><span>第 {{ pickerPage }} 页 / 共 {{ pickerTotal }} 项</span><button type="button" class="quiet" :disabled="pickerPage * 20 >= pickerTotal || pickerLoading" @click="pickerMove(1)">下一页</button></div>

        <ol class="rows">
          <li v-for="(row, index) in ingredients" :key="row.foodId">
            <div class="row-head"><strong>{{ row.foodName }}<small class="rowtag">#{{ row.foodId }}{{ row.foodStatus === 'disabled' ? ' · 已停用（保留引用，不可新增同类）' : '' }}</small></strong><div class="actions">
              <button type="button" class="quiet" :disabled="index === 0" :aria-label="`上移原料 ${row.foodName}`" @click="move(ingredients, index, -1)">上移</button>
              <button type="button" class="quiet" :disabled="index === ingredients.length - 1" :aria-label="`下移原料 ${row.foodName}`" @click="move(ingredients, index, 1)">下移</button>
              <button type="button" class="quiet" :aria-label="`删除原料 ${row.foodName}`" @click="removeAt(ingredients, index)">删除</button>
            </div></div>
            <p v-if="duplicateSet.has(row.foodId)" class="error" role="alert">食材重复：「{{ row.foodName }}」已出现多次，请合并为一条或删除多余项。</p>
            <div class="ing-row">
              <div><label class="inline" :for="`ing-qty-${row.foodId}`">{{ row.foodName }} 的数量</label><input :id="`ing-qty-${row.foodId}`" v-model="row.quantity" type="text" inputmode="decimal" autocomplete="off" placeholder="如 250 或 0.5"><small>数字，至多三位小数；按整份食谱计。</small></div>
              <div><label class="inline" :for="`ing-unit-${row.foodId}`">单位</label><select :id="`ing-unit-${row.foodId}`" v-model="row.unit"><option v-for="unit in UNITS" :key="unit.value" :value="unit.value">{{ unit.label }}</option></select></div>
            </div>
          </li>
        </ol>
        <p v-if="!ingredients.length" class="muted">还没有原料。草稿可以先不添加；{{ publishLabel }}前至少需要一条。</p>
      </div>

      <div class="card">
        <h2>过敏原标注</h2>
        <p class="muted">仅标注食材之外、食谱层面新增的过敏原风险（如调味料引入）；食材自身标注自动并入「有效过敏原」，无需在此重复。</p>
        <div class="choices"><label v-for="(label, code) in ALLERGENS" :key="code"><input type="checkbox" v-model="pickedAllergens" :value="code">{{ label }}</label></div>
        <label class="choices"><input type="checkbox" v-model="allergenComplete">已核对本食谱层面的额外风险</label>
        <small>默认未核对，不许预勾选；勾选时必须填写依据。</small>
        <label for="recipe-allergen-source">核对依据</label><input id="recipe-allergen-source" v-model="allergenSource" maxlength="500" autocomplete="off"><small>配料表、检测报告或来源说明，至多 500 个字。</small>
      </div>

      <p v-if="formError" class="error" role="alert">{{ formError }} <button v-if="conflict" type="button" class="quiet" :disabled="busy" @click="reloadServer">载入服务端最新内容</button></p>
      <p v-if="message" class="success" role="status">{{ message }}</p>
      <div class="actions">
        <button type="submit" :disabled="busy || auth.checking || !!auth.checkError">{{ busy ? '正在保存…' : draftLabel }}</button>
        <button v-if="canPublish" type="button" :disabled="busy || auth.checking || !!auth.checkError" @click="save('publish')">{{ publishLabel }}</button>
        <button v-if="canArchive" type="button" class="quiet" :disabled="busy || auth.checking || !!auth.checkError" @click="save('archive')">归档</button>
        <RouterLink class="linklike" :to="admin ? '/admin/recipes' : '/recipes'">返回列表</RouterLink>
      </div>
      <small v-if="!isEdit">新食谱将首先保存为草稿；保存后即可在本页「{{ publishLabel }}」或「归档」。</small>
      <small v-else>「{{ publishLabel }}」要求：至少一个步骤与一条原料、全部食材为启用状态且过敏原标注完整、已勾选「已核对」并填写依据；以服务端校验为准，失败原因会原样展示。</small>
    </fieldset>
  </form>
</section></template>
<style scoped>
/* 本页补充样式：步骤/原料行、原料挑选列表与多行输入。 */
textarea { display: block; width: 100%; padding: 11px 12px; border: 1px solid #aebfb1; border-radius: 7px; background: #fff; color: inherit; font: inherit; }
textarea:focus-visible { outline: 3px solid #97bd64; outline-offset: 3px; }
ol.rows { list-style: none; margin: 12px 0; padding: 0; }
ol.rows > li { border-top: 1px solid #e3e9e0; padding: 12px 0; }
ol.rows > li:first-child { border-top: 0; }
.row-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; margin-bottom: 8px; }
.rowtag { font-weight: 400; }
.ing-row { display: grid; grid-template-columns: minmax(0, 2fr) minmax(0, 1fr); gap: 0 24px; }
ul.picker { list-style: none; margin: 10px 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 6px 16px; }
ul.picker li { display: flex; align-items: center; justify-content: space-between; gap: 8px; border: 1px solid #e3e9e0; border-radius: 7px; padding: 6px 10px; }
label.inline { margin-top: 10px; }
.linklike { font-size: 14px; font-weight: 600; align-self: center; }
@media (max-width: 600px) { .ing-row { grid-template-columns: 1fr; } }
</style>
