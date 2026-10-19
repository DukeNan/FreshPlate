<script setup>
// 食材目录：登录用户按名称检索 active 食材摘要并查看完整详情。
// 未知信息（null）一律显示「未知」或「无法换算」，绝不把未知当成 0。
import { ref, onMounted } from 'vue'
import { api } from '../api.js'
import { auth } from '../auth.js'
const generation = auth.generation
const items = ref([]), total = ref(0), page = ref(1), q = ref(''), error = ref(''), loading = ref(false)
const detailId = ref(0), detail = ref(null), detailLoading = ref(false), detailError = ref('')
const allergens = { milk: '乳', egg: '蛋', fish: '鱼', crustacean: '甲壳类', mollusc: '软体动物', peanut: '花生', soy: '大豆', wheat: '小麦', tree_nut: '树坚果', sesame: '芝麻' }
let sequence = 0
async function load(reset = false) {
  if (reset) page.value = 1
  const keyword = q.value.trim()
  if ([...keyword].length > 100) { error.value = '搜索关键词去除首尾空白后最多 100 个字。'; return }
  const own = ++sequence
  loading.value = true; error.value = ''
  try {
    const query = new URLSearchParams({ page: String(page.value), page_size: '20' })
    if (keyword) query.set('q', keyword)
    const result = await api(`/food-catalog?${query}`)
    if (own === sequence && generation === auth.generation) { items.value = result.items; total.value = result.total }
  } catch (e) { if (own === sequence) error.value = e.message } finally { if (own === sequence) loading.value = false }
}
async function fetchDetail(id) {
  detailId.value = id; detail.value = null; detailError.value = ''; detailLoading.value = true
  try {
    const result = await api(`/foods/${id}`)
    if (detailId.value === id && generation === auth.generation) detail.value = result
  } catch (e) { if (detailId.value === id) detailError.value = e.message } finally { if (detailId.value === id) detailLoading.value = false }
}
function toggleDetail(food) {
  if (detailId.value === food.id) closeDetail()
  else fetchDetail(food.id)
}
function closeDetail() { detailId.value = 0; detail.value = null; detailError.value = '' }
// 以下展示约定：null=未知；0 是有来源证明的真实零值，正常显示 0。
function num(value, unit) { return value === null || value === undefined ? '未知' : `${value}${unit ? ' ' + unit : ''}` }
function text(value) { return value === null || value === undefined ? '未知' : value }
function names(list) { return (list || []).map(code => allergens[code] || code).join('、') }
// 过敏原三种状态文案：未知 / 已核对无 / 部分已知（含已核对且有标注项）。
function allergenState(food) {
  const known = (food.allergens || []).length > 0
  if (food.allergen_complete) return known ? `已核对：${names(food.allergens)}` : '已核对无（支持范围内无标注项）'
  return known ? `部分已知：${names(food.allergens)}（尚未完成核对）` : '未知（未核对）'
}
function conversion(value, unitText) { return value === null || value === undefined ? '无法换算' : unitText }
function time(value) { return new Date(typeof value === 'number' ? value * 1000 : value).toLocaleString('zh-CN') }
onMounted(() => load())
</script>
<template><section><p class="eyebrow">公共食材数据</p><h1>食材目录</h1><p class="muted">检索启用中的公共食材并查看营养、换算与过敏原标注。「未知」表示暂无数据来源，不会按 0 处理。</p><form class="filters card" @submit.prevent="load(true)"><div><label for="food-q">搜索名称</label><input id="food-q" v-model="q" autocomplete="off" placeholder="按名称字面包含搜索"></div><button :disabled="loading">查询</button></form><p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="loading" role="status">正在加载食材…</p><div class="table-wrap card"><table><caption>食材目录，共 {{ total }} 项</caption><thead><tr><th scope="col">名称</th><th scope="col">状态</th><th scope="col">版本</th><th scope="col">过敏原标记</th><th scope="col">操作</th></tr></thead><tbody><tr v-for="food in items" :key="food.id" @click="toggleDetail(food)"><td>{{ food.name }}<small>#{{ food.id }}</small></td><td>{{ food.status === 'active' ? '启用' : '停用' }}</td><td>v{{ food.version }}</td><td>{{ food.allergen_complete ? '已核对 ✓' : '未核对' }}</td><td><button class="quiet" @click.stop="toggleDetail(food)">{{ detailId === food.id ? '收起详情' : '详情' }}</button></td></tr><tr v-if="!items.length && !loading"><td colspan="5">没有匹配的食材。</td></tr></tbody></table></div><div class="pagination"><button class="quiet" :disabled="page <= 1 || loading" @click="page--; load()">上一页</button><span>第 {{ page }} 页</span><button class="quiet" :disabled="page * 20 >= total || loading" @click="page++; load()">下一页</button></div><section v-if="detailId" class="card" aria-label="食材详情"><h2>食材详情</h2><p v-if="detailLoading" role="status">正在加载详情…</p><p v-if="detailError" class="error" role="alert">{{ detailError }} <button type="button" class="quiet" @click="fetchDetail(detailId)">重试</button></p><template v-if="detail"><p class="muted">以上信息来自公共食材库；「未知」表示暂无可靠数据来源。</p><table><caption class="sr-only">{{ detail.name }} 的完整字段</caption><tbody><tr><th scope="row">名称 / 编号</th><td>{{ detail.name }}<small>#{{ detail.id }}</small></td></tr><tr><th scope="row">状态</th><td>{{ detail.status === 'active' ? '启用' : '停用（既有引用仍可查看此详情）' }}</td></tr><tr><th scope="row">版本</th><td>v{{ detail.version }}</td></tr><tr><th scope="row">能量</th><td>{{ num(detail.energy_kcal_100g, '千卡 / 100 克') }}</td></tr><tr><th scope="row">蛋白质</th><td>{{ num(detail.protein_g_100g, '克 / 100 克') }}</td></tr><tr><th scope="row">脂肪</th><td>{{ num(detail.fat_g_100g, '克 / 100 克') }}</td></tr><tr><th scope="row">碳水化合物</th><td>{{ num(detail.carbohydrate_g_100g, '克 / 100 克') }}</td></tr><tr><th scope="row">营养来源</th><td>{{ text(detail.nutrition_source) }}</td></tr><tr><th scope="row">营养核对条目</th><td>{{ text(detail.nutrition_reference) }}</td></tr><tr><th scope="row">毫升 → 克</th><td>{{ conversion(detail.density_g_ml, `1 毫升 ≈ ${detail.density_g_ml} 克`) }}</td></tr><tr><th scope="row">个 → 克</th><td>{{ conversion(detail.piece_weight_g, `1 个 ≈ ${detail.piece_weight_g} 克（可食部估算）`) }}</td></tr><tr><th scope="row">换算依据</th><td>{{ text(detail.conversion_source) }}</td></tr><tr><th scope="row">过敏原</th><td>{{ allergenState(detail) }}</td></tr><tr><th scope="row">过敏原标注依据</th><td>{{ text(detail.allergen_source) }}</td></tr><tr><th scope="row">更新时间</th><td>{{ time(detail.updated_at) }}</td></tr><tr><th scope="row">创建时间</th><td>{{ time(detail.created_at) }}</td></tr></tbody></table><div class="actions"><button type="button" class="quiet" @click="closeDetail">关闭详情</button></div></template></section></section></template>
