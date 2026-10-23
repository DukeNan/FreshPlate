<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api } from '../api.js'
import { auth } from '../auth.js'
import { filteredCount, missingSummary, normalizeRecommendationQuery, recommendationEmpty, recommendationQueryError, rememberRecommendation } from '../recommendations.js'

const generation = auth.generation
const items = ref([]), total = ref(0), stats = ref(null), page = ref(1)
const q = ref(''), availability = ref('all'), appliedQuery = ref(''), appliedAvailability = ref('all')
const loading = ref(false), error = ref('')
let sequence = 0
const empty = computed(() => stats.value && recommendationEmpty(stats.value, total.value, appliedQuery.value, appliedAvailability.value))
async function load(reset = false, targetPage = page.value) {
  if (loading.value) return
  const query = normalizeRecommendationQuery(q.value), filter = availability.value
  const queryError = recommendationQueryError(query)
  if (queryError) { error.value = queryError; return }
  page.value = reset ? 1 : targetPage
  const own = ++sequence
  loading.value = true; error.value = ''
  // A failed refresh must not leave an earlier recommendation looking current.
  items.value = []; stats.value = null; total.value = 0
  try {
    const params = new URLSearchParams({ q: query, availability: filter, page: String(page.value), page_size: '20' })
    const result = await api(`/recommendations?${params}`)
    if (own === sequence && generation === auth.generation) {
      items.value = result.items; total.value = result.total; stats.value = result.stats
      appliedQuery.value = query; appliedAvailability.value = filter
    }
  } catch (e) {
    if (own === sequence && generation === auth.generation) error.value = e.message
  } finally { if (own === sequence && generation === auth.generation) loading.value = false }
}
function changePage(delta) { if (!loading.value) load(false, page.value + delta) }
onMounted(() => load())
onUnmounted(() => { sequence++ })
</script>
<template><section>
  <p class="eyebrow">按本人库存与饮食限制推荐</p><h1>食谱推荐</h1>
  <p class="muted">按缺料数、临期食材和耗时排序。推荐为加载时的库存与偏好快照；库存或饮食设置变化后请刷新。</p>
  <form class="filters card" @submit.prevent="load(true)">
    <div><label for="recommendation-q">食谱名称</label><input id="recommendation-q" v-model="q" :disabled="loading" @change="load(true)"></div>
    <div><label for="recommendation-availability">库存筛选</label><select id="recommendation-availability" v-model="availability" :disabled="loading" @change="load(true)"><option value="all">全部</option><option value="can_cook">仅可立即做</option></select></div>
    <button :disabled="loading">查询 / 刷新推荐</button>
  </form>
  <p v-if="loading" role="status">正在加载推荐…</p>
  <p v-if="error" class="error" role="alert">{{ error }} <button class="quiet" :disabled="loading" @click="load()">重试</button></p>
  <template v-if="stats">
    <p v-if="!stats.has_usable_inventory" class="notice" role="status">当前没有可用于推荐的库存。<RouterLink to="/inventory">前往我的库存</RouterLink></p>
    <p v-if="filteredCount(stats)" class="muted">另有 {{ filteredCount(stats) }} 个食谱因过敏原/忌口、信息不完整或停用引用未列入。</p>
    <div v-if="empty" class="card" role="status"><h2>{{ empty.text }}</h2><p>{{ empty.hint }} <RouterLink v-if="empty.to" :to="empty.to">前往食谱页</RouterLink></p></div>
    <template v-else>
      <p>共 {{ total }} 个推荐</p>
      <article v-for="item in items" :key="item.recipe.id" class="card">
        <h2><RouterLink :to="`/recipes/${item.recipe.id}`" @click="rememberRecommendation(item)">{{ item.recipe.name }}</RouterLink></h2>
        <p><span :class="item.can_cook ? 'success' : 'notice'">{{ item.can_cook ? '可立即做' : `缺 ${item.missing_count} 样` }}</span> <span v-if="item.uses_expiring" class="notice">可用临期食材</span></p>
        <p>{{ item.recipe.duration_minutes }} 分钟 · {{ item.recipe.servings }} 份 · {{ item.recipe.visibility === 'private' ? '我的私有食谱' : '公共食谱' }}</p>
        <p v-if="!item.can_cook">{{ missingSummary(item.ingredients) }}</p>
      </article>
      <p v-if="!items.length">本页暂无条目，请返回上一页或刷新推荐。</p>
      <div class="pagination"><button class="quiet" :disabled="loading || page <= 1" @click="changePage(-1)">上一页</button><span>第 {{ page }} 页</span><button class="quiet" :disabled="loading || page * 20 >= total" @click="changePage(1)">下一页</button></div>
    </template>
  </template>
</section></template>
