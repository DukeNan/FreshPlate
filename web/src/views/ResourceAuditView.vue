<script setup>
// 资源审计（管理员）：food/recipe 公共资源管理事件只读展示，按 id 倒序。
// 账号审计仍在审计记录页，两者互不混用。
import { ref, onMounted } from 'vue'
import { api } from '../api.js'
import { auth } from '../auth.js'
const generation = auth.generation
const items = ref([]), total = ref(0), page = ref(1), resourceType = ref(''), resourceId = ref(''), action = ref(''), error = ref(''), loading = ref(false)
let sequence = 0
async function load(reset = false) {
  if (reset) page.value = 1
  const idText = String(resourceId.value).trim()
  if (idText !== '' && (!/^\d+$/.test(idText) || Number(idText) < 1 || !Number.isSafeInteger(Number(idText)))) { error.value = '资源 ID 必须为正整数。'; return }
  const own = ++sequence
  loading.value = true; error.value = ''
  try {
    const query = new URLSearchParams({ page: String(page.value), page_size: '20' })
    if (resourceType.value) query.set('resource_type', resourceType.value)
    if (idText !== '') query.set('resource_id', idText)
    if (action.value.trim()) query.set('action', action.value.trim())
    const result = await api(`/admin/resource-audit-logs?${query}`)
    if (own === sequence && generation === auth.generation) { items.value = result.items; total.value = result.total }
  } catch (e) { if (own === sequence) error.value = e.message } finally { if (own === sequence) loading.value = false }
}
function time(value) { return new Date(typeof value === 'number' ? value * 1000 : value).toLocaleString('zh-CN') }
onMounted(() => load())
</script>
<template><section><p class="eyebrow">管理员工作台</p><h1>资源审计</h1><p class="muted">公共食材与食谱的管理事件只读展示，按最新记录排序。账号管理事件请查看审计记录页。</p><form class="filters card" @submit.prevent="load(true)"><div><label for="resource-type">资源类型</label><select id="resource-type" v-model="resourceType"><option value="">全部</option><option value="food">食材</option><option value="recipe">食谱</option></select></div><div><label for="resource-id">资源 ID</label><input id="resource-id" v-model="resourceId" type="number" min="1" step="1" inputmode="numeric"></div><div><label for="resource-action">操作编码</label><input id="resource-action" v-model="action" placeholder="留空查询全部，如 food_created" autocomplete="off"></div><button :disabled="loading">查询</button></form><p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="loading" role="status">正在加载记录…</p><div class="table-wrap card"><table><caption>资源审计列表，共 {{ total }} 条</caption><thead><tr><th scope="col">时间 / ID</th><th scope="col">操作者</th><th scope="col">资源类型 / 资源 ID</th><th scope="col">操作</th><th scope="col">详情</th></tr></thead><tbody><tr v-for="item in items" :key="item.id"><td>{{ time(item.created_at) }}<small>#{{ item.id }}</small></td><td>{{ item.actor_user_id }}</td><td>{{ item.resource_type === 'food' ? '食材' : item.resource_type }}<small>#{{ item.resource_id }}</small></td><td>{{ item.action }}</td><td><code>{{ JSON.stringify(item.details) }}</code></td></tr><tr v-if="!items.length && !loading"><td colspan="5">没有匹配的资源审计记录。</td></tr></tbody></table></div><div class="pagination"><button class="quiet" :disabled="page <= 1 || loading" @click="page--; load()">上一页</button><span>第 {{ page }} 页</span><button class="quiet" :disabled="page * 20 >= total || loading" @click="page++; load()">下一页</button></div></section></template>
