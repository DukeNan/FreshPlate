<script setup>
// 食材管理（管理员）：分页查询全部状态食材，新建/编辑（含乐观并发
// version）与启停。营养组整组填写或整组留空；未知信息显式提交 null/false。
import { ref, onMounted } from 'vue'
import { api } from '../api.js'
import { auth, verifyForm } from '../auth.js'
const expectedUserId = auth.user?.id
const generation = auth.generation
const allergens = { milk: '乳', egg: '蛋', fish: '鱼', crustacean: '甲壳类', mollusc: '软体动物', peanut: '花生', soy: '大豆', wheat: '小麦', tree_nut: '树坚果', sesame: '芝麻' }
const items = ref([]), total = ref(0), page = ref(1), q = ref(''), status = ref(''), error = ref(''), message = ref(''), busy = ref(false), loading = ref(false)
// 表单状态：editing 为 null 表示新建；version 保存编辑起点，用于 PUT。
const formOpen = ref(false), editing = ref(null), saving = ref(false), loadingForm = ref(false), formError = ref(''), conflict = ref(false)
const name = ref(''), foodStatus = ref('active')
const nutritionOn = ref(false), energy = ref(''), protein = ref(''), fat = ref(''), carb = ref(''), nutritionSource = ref(''), nutritionReference = ref('')
const density = ref(''), pieceWeight = ref(''), conversionSource = ref('')
const pickedAllergens = ref([]), complete = ref(false), allergenSource = ref('')
// formReady：编辑模式下「当前目标的详情已成功加载」的标志。false 表示表单
// 数据不可信（详情加载失败或仍在途），此时保存按钮不可用，save() 也会拒绝。
const formReady = ref(false)
let version = 0
let sequence = 0
// 编辑表单请求序列：任何表单生命周期变化（新建/再编辑/关闭）都递增，
// 作废在途的 GET /foods/:id，防止晚到的旧响应覆盖新表单的数据与 version。
let editSequence = 0
async function load(reset = false) {
  if (reset) page.value = 1
  const keyword = q.value.trim()
  if ([...keyword].length > 100) { error.value = '搜索关键词去除首尾空白后最多 100 个字。'; return }
  const own = ++sequence
  loading.value = true; error.value = ''
  try {
    const query = new URLSearchParams({ page: String(page.value), page_size: '20' })
    if (keyword) query.set('q', keyword)
    if (status.value) query.set('status', status.value)
    const result = await api(`/admin/foods?${query}`)
    if (own === sequence && generation === auth.generation) { items.value = result.items; total.value = result.total }
  } catch (e) { if (own === sequence) error.value = e.message } finally { if (own === sequence) loading.value = false }
}
function clearNutrition() { energy.value = protein.value = fat.value = carb.value = nutritionSource.value = nutritionReference.value = '' }
// 取消勾选营养组时整体清空，保持「整组填写或整组留空」的联动语义。
function toggleNutrition() { if (!nutritionOn.value) clearNutrition() }
// 清空全部表单字段与并发版本：进入新建/编辑时先回到空白基线，防止上一次
// 编辑残留的数据与 version 在详情加载失败时被误保存到当前目标。
function resetForm() {
  version = 0
  name.value = ''; foodStatus.value = 'active'
  nutritionOn.value = false; clearNutrition()
  density.value = pieceWeight.value = conversionSource.value = ''
  pickedAllergens.value = []; complete.value = false; allergenSource.value = ''
}
function openCreate() {
  editSequence++ // 作废在途的编辑详情请求
  conflict.value = false; formError.value = ''; message.value = ''
  editing.value = null
  resetForm()
  formReady.value = true // 新建不依赖详情加载，表单立即可保存
  formOpen.value = true
}
async function openEdit(food) {
  // 序列防竞态：快速依次点开食材 A、B 时，A 的晚到响应不得覆盖 B 的表单。
  const own = ++editSequence
  conflict.value = false; formError.value = ''; message.value = ''
  editing.value = food; formOpen.value = true; loadingForm.value = true
  // 先清空字段与 version 再请求详情：若详情加载失败，表单不会残留上一个
  // 食材的数据与版本（否则保存目标已切到当前食材，会静默覆盖他人数据）。
  resetForm()
  formReady.value = false
  try {
    const detail = await api(`/foods/${food.id}`)
    if (own !== editSequence || generation !== auth.generation) return
    version = detail.version
    name.value = detail.name; foodStatus.value = detail.status
    nutritionOn.value = detail.energy_kcal_100g !== null && detail.energy_kcal_100g !== undefined
    energy.value = detail.energy_kcal_100g ?? ''; protein.value = detail.protein_g_100g ?? ''
    fat.value = detail.fat_g_100g ?? ''; carb.value = detail.carbohydrate_g_100g ?? ''
    nutritionSource.value = detail.nutrition_source ?? ''; nutritionReference.value = detail.nutrition_reference ?? ''
    density.value = detail.density_g_ml ?? ''; pieceWeight.value = detail.piece_weight_g ?? ''; conversionSource.value = detail.conversion_source ?? ''
    pickedAllergens.value = [...(detail.allergens || [])]
    complete.value = detail.allergen_complete
    allergenSource.value = detail.allergen_source ?? ''
    // 详情填充完毕，表单数据确实来自当前目标，才允许保存。
    formReady.value = true
  } catch (e) {
    if (own !== editSequence) return
    formError.value = e.message
  } finally { if (own === editSequence) loadingForm.value = false }
}
function closeForm() {
  editSequence++ // 关闭表单同样作废在途请求
  formOpen.value = false; editing.value = null
}
function decimals(text) { const i = text.indexOf('.'); return i === -1 ? 0 : text.length - i - 1 }
// 客户端校验：只拦截明显无效输入，最终以后端校验为准；校验失败不发请求。
// 返回 null 表示通过，否则返回错误文案。
function validate() {
  const trimmed = name.value.trim()
  const length = [...trimmed].length
  if (length < 1 || length > 100) return '名称去除首尾空白后需为 1~100 个字。'
  if (/[\u0000-\u001f\u007f]/.test(trimmed)) return '名称不能包含控制字符。'
  let nutrition = null
  if (nutritionOn.value) {
    // v-model 在 type="number" 输入上会把可解析值转成 number，这里统一归一为
    // 字符串后再做空白/边界校验，避免对 number 调用 .trim() 抛错。
    const raw = [energy.value, protein.value, fat.value, carb.value].map(v => String(v ?? '').trim())
    if (raw.some(v => v === '') || !nutritionSource.value.trim() || !nutritionReference.value.trim()) return '营养信息必须整组填写或整组留空：能量、蛋白质、脂肪、碳水化合物与两个来源需全部填写。'
    const nums = raw.map(v => Number(v))
    if (nums.some(v => !Number.isFinite(v))) return '营养数值必须为有效数字。'
    if (nums[0] < 0 || nums[0] > 1000) return '能量需在 0~1000 千卡/100 克之间。'
    if (nums.slice(1).some(v => v < 0 || v > 100)) return '蛋白质、脂肪、碳水化合物需在 0~100 克/100 克之间。'
    nutrition = { energy: nums[0], protein: nums[1], fat: nums[2], carb: nums[3] }
  }
  let densityValue = null, pieceValue = null
  // 同上：type="number" 的 v-model 值可能是 number，先归一为字符串再校验。
  const densityText = String(density.value ?? '').trim()
  if (densityText !== '') {
    const v = Number(densityText)
    if (!Number.isFinite(v) || v <= 0 || v > 100) return '每毫升克数需为大于 0 且不超过 100 的数字。'
    if (decimals(densityText) > 6) return '换算系数最多六位小数。'
    densityValue = v
  }
  const pieceText = String(pieceWeight.value ?? '').trim()
  if (pieceText !== '') {
    const v = Number(pieceText)
    if (!Number.isFinite(v) || v <= 0 || v > 1000000) return '单个重量需为大于 0 且不超过 1000000 的数字。'
    if (decimals(pieceText) > 6) return '换算系数最多六位小数。'
    pieceValue = v
  }
  if ((densityValue !== null || pieceValue !== null) && !conversionSource.value.trim()) return '填写换算系数时必须提供换算依据（不能为纯空白）。'
  if (complete.value && !allergenSource.value.trim()) return '勾选「已核对」时必须提供过敏原标注依据（不能为纯空白）。'
  return {
    name: trimmed, status: foodStatus.value,
    energy_kcal_100g: nutrition ? nutrition.energy : null,
    protein_g_100g: nutrition ? nutrition.protein : null,
    fat_g_100g: nutrition ? nutrition.fat : null,
    carbohydrate_g_100g: nutrition ? nutrition.carb : null,
    nutrition_source: nutrition ? nutritionSource.value.trim() : null,
    nutrition_reference: nutrition ? nutritionReference.value.trim() : null,
    density_g_ml: densityValue, piece_weight_g: pieceValue,
    conversion_source: conversionSource.value.trim() ? conversionSource.value.trim() : null,
    allergens: [...pickedAllergens.value], allergen_complete: complete.value,
    allergen_source: allergenSource.value.trim() ? allergenSource.value.trim() : null,
  }
}
async function save() {
  const body = validate()
  if (!body || typeof body === 'string') { formError.value = typeof body === 'string' ? body : '输入无效，请检查表单。'; return }
  if (saving.value) return
  // 编辑模式守卫：详情未成功加载（formReady=false）时表单数据不可信，
  // 可能是上一个食材的残留内容，直接拒绝保存而不发请求。
  if (editing.value && !formReady.value) { formError.value = '该食材详情尚未加载成功，表单数据不可信，请重新载入该食材后再保存。'; return }
  // 固定快照：verifyForm 与后续请求存在网络往返，期间列表可能被点到其他
  // 食材的「编辑」而改写 editing/version。提交目标与内容必须来自同一快照，
  // 防止把 A 的内容 PUT 到 B（跨对象覆盖）。
  const target = editing.value
  const targetVersion = version
  const isEdit = !!target
  saving.value = true; formError.value = ''; message.value = ''
  try {
    await verifyForm(expectedUserId)
    let result
    if (isEdit) {
      if (!Number.isSafeInteger(targetVersion) || targetVersion < 1) { formError.value = '缺少有效的当前版本（version），请重新载入该食材后再保存。'; return }
      result = await api(`/admin/foods/${target.id}`, { method: 'PUT', expectedUserId, body: { ...body, version: targetVersion } })
    } else {
      result = await api('/admin/foods', { method: 'POST', expectedUserId, body })
    }
    if (generation !== auth.generation) return
    message.value = isEdit ? `食材「${result.name}」已更新，当前版本 v${result.version}。` : `食材「${result.name}」已创建。`
    closeForm()
    await load()
  } catch (e) {
    if (e.code === 'VERSION_CONFLICT') {
      // 冲突：保留草稿，不自动重放，引导管理员载入最新数据后重新提交。
      conflict.value = true
      formError.value = '保存失败：该食材已被他人修改（版本冲突）。你填写的内容仍保留在表单中，尚未覆盖他人修改；请点击「载入最新数据」查看最新内容，确认后再重新提交。'
    } else formError.value = e.message
  } finally { saving.value = false }
}
async function reloadLatest() {
  if (!editing.value) return
  await openEdit(editing.value)
  conflict.value = false
}
async function toggleStatus(food) {
  const disabling = food.status === 'active'
  const tip = disabling
    ? `确认停用食材「${food.name}」？停用后新增引用不可选，既有引用不受影响（此操作影响新增引用的可用范围）。`
    : `确认启用食材「${food.name}」？启用后将重新出现在目录与新增引用的可选项中。`
  if (!window.confirm(tip)) return
  busy.value = true; error.value = ''; message.value = ''
  try {
    await verifyForm(expectedUserId)
    const detail = await api(`/foods/${food.id}`)
    if (generation !== auth.generation) return
    const body = {
      name: detail.name, status: disabling ? 'disabled' : 'active',
      energy_kcal_100g: detail.energy_kcal_100g, protein_g_100g: detail.protein_g_100g, fat_g_100g: detail.fat_g_100g, carbohydrate_g_100g: detail.carbohydrate_g_100g,
      nutrition_source: detail.nutrition_source, nutrition_reference: detail.nutrition_reference,
      density_g_ml: detail.density_g_ml, piece_weight_g: detail.piece_weight_g, conversion_source: detail.conversion_source,
      allergens: [...(detail.allergens || [])], allergen_complete: detail.allergen_complete, allergen_source: detail.allergen_source,
    }
    await api(`/admin/foods/${food.id}`, { method: 'PUT', expectedUserId, body: { ...body, version: detail.version } })
    message.value = disabling ? `食材「${food.name}」已停用。` : `食材「${food.name}」已启用。`
    await load()
  } catch (e) {
    error.value = e.code === 'VERSION_CONFLICT' ? '操作失败：该食材已被他人修改（版本冲突），请重新查询后重试；本次未覆盖他人修改。' : e.message
  } finally { busy.value = false }
}
onMounted(() => load())
</script>
<template><section><p class="eyebrow">管理员工作台</p><h1>食材管理</h1><p class="muted">维护公共食材的营养、换算与过敏原标注。同一 ID 必须始终指代同一种食材及加工形态：名称只能纠正错字，语义变更（换成另一种食材、或生改熟等加工形态变化）应创建新食材并停用旧食材，不能直接改名。</p><form class="filters card" @submit.prevent="load(true)"><div><label for="admin-food-q">名称</label><input id="admin-food-q" v-model="q" autocomplete="off" placeholder="按名称字面包含搜索"></div><div><label for="admin-food-status">状态</label><select id="admin-food-status" v-model="status"><option value="">全部</option><option value="active">启用</option><option value="disabled">停用</option></select></div><button :disabled="loading || busy">查询</button><button type="button" class="quiet" :disabled="formOpen" @click="openCreate">新建食材</button></form><p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="message" class="success" role="status">{{ message }}</p><p v-if="loading" role="status">正在加载食材…</p><div class="table-wrap card"><table><caption>食材列表，共 {{ total }} 项</caption><thead><tr><th scope="col">名称</th><th scope="col">状态</th><th scope="col">版本</th><th scope="col">过敏原标记</th><th scope="col">操作</th></tr></thead><tbody><tr v-for="food in items" :key="food.id"><td>{{ food.name }}<small>#{{ food.id }}</small></td><td>{{ food.status === 'active' ? '启用' : '停用' }}</td><td>v{{ food.version }}</td><td>{{ food.allergen_complete ? '已核对 ✓' : '未核对' }}</td><td><div class="actions"><button class="quiet" :disabled="busy || loading || saving || loadingForm" @click="openEdit(food)">编辑</button><button class="quiet" :disabled="busy || loading || saving || auth.checking || !!auth.checkError" @click="toggleStatus(food)">{{ food.status === 'active' ? '停用' : '启用' }}</button></div></td></tr><tr v-if="!items.length && !loading"><td colspan="5">没有匹配的食材。</td></tr></tbody></table></div><div class="pagination"><button class="quiet" :disabled="page <= 1 || loading || busy" @click="page--; load()">上一页</button><span>第 {{ page }} 页</span><button class="quiet" :disabled="page * 20 >= total || loading || busy" @click="page++; load()">下一页</button></div><section v-if="formOpen" class="card"><h2>{{ editing ? `编辑 ${editing.name}` : '新建食材' }}</h2><p v-if="loadingForm" role="status">正在载入食材数据…</p><form v-if="!loadingForm" @submit.prevent="save"><fieldset :disabled="saving || auth.checking || !!auth.checkError"><legend class="sr-only">食材表单</legend><p class="muted">提交会保存全部字段：暂无数据的项目将以 null（未知）或 false（未核对）显式记录，不会按 0 处理。</p><fieldset><legend>基本信息</legend><div class="grid"><div><label for="food-name">名称</label><input id="food-name" v-model="name" maxlength="200" autocomplete="off" required><small>去除首尾空白后 1~100 个字。同一 ID 必须始终指代同一种食材及加工形态，语义变更请新建并停用旧记录。</small></div><div><label for="food-status">状态</label><select id="food-status" v-model="foodStatus"><option value="active">启用</option><option value="disabled">停用</option></select><small>停用后新增引用不可选，既有引用不受影响。</small></div></div></fieldset><fieldset><legend>营养来源</legend><label class="choices"><input type="checkbox" v-model="nutritionOn" @change="toggleNutrition">填写营养信息（每 100 克可食部，须整组填写或整组留空）</label><div class="grid"><div><label for="food-energy">能量（千卡 / 100 克）</label><input id="food-energy" v-model="energy" type="number" min="0" max="1000" step="any" inputmode="decimal" :disabled="!nutritionOn"></div><div><label for="food-protein">蛋白质（克 / 100 克）</label><input id="food-protein" v-model="protein" type="number" min="0" max="100" step="any" inputmode="decimal" :disabled="!nutritionOn"></div><div><label for="food-fat">脂肪（克 / 100 克）</label><input id="food-fat" v-model="fat" type="number" min="0" max="100" step="any" inputmode="decimal" :disabled="!nutritionOn"></div><div><label for="food-carb">碳水化合物（克 / 100 克）</label><input id="food-carb" v-model="carb" type="number" min="0" max="100" step="any" inputmode="decimal" :disabled="!nutritionOn"></div><div><label for="food-nutrition-source">营养数据来源</label><input id="food-nutrition-source" v-model="nutritionSource" maxlength="500" autocomplete="off" :disabled="!nutritionOn"><small>数据集名称、版本或来源说明。</small></div><div><label for="food-nutrition-reference">营养核对条目</label><input id="food-nutrition-reference" v-model="nutritionReference" maxlength="1000" autocomplete="off" :disabled="!nutritionOn"><small>可核对的条目编号或来源链接。</small></div></div><small>0 是有来源证明的真实零值；留空则整组记为未知，不会补 0 或推测。</small></fieldset><fieldset><legend>换算</legend><div class="grid"><div><label for="food-density">每毫升对应克数</label><input id="food-density" v-model="density" type="number" min="0.000001" max="100" step="any" inputmode="decimal"><small>用于毫升 → 克换算；留空表示无法换算。</small></div><div><label for="food-piece">单个重量（克，可食部）</label><input id="food-piece" v-model="pieceWeight" type="number" min="0.000001" max="1000000" step="any" inputmode="decimal"><small>用于个 → 克换算；留空表示无法换算。</small></div><div><label for="food-conversion-source">换算依据</label><input id="food-conversion-source" v-model="conversionSource" maxlength="500" autocomplete="off"><small>填写任一系数时必填。</small></div></div></fieldset><fieldset><legend>过敏原</legend><div class="choices"><label v-for="(label, code) in allergens" :key="code"><input type="checkbox" v-model="pickedAllergens" :value="code">{{ label }}</label></div><label class="choices"><input type="checkbox" v-model="complete">已核对（在支持的 10 类编码范围内完成核对）</label><small>默认未核对；「已核对无」需要在核对后勾选并填写依据，不能预勾选。</small><label for="food-allergen-source">过敏原标注依据</label><input id="food-allergen-source" v-model="allergenSource" maxlength="500" autocomplete="off"><small>勾选「已核对」时必填。</small></fieldset><p v-if="formError" class="error" role="alert">{{ formError }} <button v-if="conflict" type="button" class="quiet" :disabled="saving" @click="reloadLatest">载入最新数据</button></p><div class="actions"><button type="submit" :disabled="saving || auth.checking || !!auth.checkError || (!!editing && !formReady)">{{ saving ? '正在保存…' : editing ? '保存修改' : '创建食材' }}</button><button type="button" class="quiet" :disabled="saving" @click="closeForm">取消</button></div></fieldset></form></section></section></template>
