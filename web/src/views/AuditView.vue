<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api.js'
import { auth } from '../auth.js'
const generation = auth.generation
const items = ref([]), total = ref(0), page = ref(1), target = ref(''), action = ref(''), error = ref(''), loading = ref(false)
let sequence = 0
async function load(reset = false) {
  if (reset) page.value = 1
  const ownSequence = ++sequence
  loading.value = true; error.value = ''
  try {
    const query = new URLSearchParams({ page: String(page.value), page_size: '20' })
    if (target.value) query.set('target_user_id', target.value)
    if (action.value) query.set('action', action.value)
    const result = await api(`/admin/audit-logs?${query}`)
    if (ownSequence === sequence && generation === auth.generation) { items.value = result.items; total.value = result.total }
  } catch (e) { if (ownSequence === sequence) error.value = e.message } finally { if (ownSequence === sequence) loading.value = false }
}
function time(value) { return new Date(typeof value === 'number' ? value * 1000 : value).toLocaleString('zh-CN') }
onMounted(() => load())
</script>
<template><section><p class="eyebrow">管理员工作台</p><h1>审计记录</h1><p class="muted">账号管理事件只读展示，按最新记录排序。跨页出现新增记录时可刷新查询。</p><form class="filters card" @submit.prevent="load(true)"><div><label for="audit-target">目标用户 ID</label><input id="audit-target" v-model="target" type="number" min="1" step="1" inputmode="numeric"></div><div><label for="audit-action">操作编码</label><input id="audit-action" v-model="action" placeholder="留空查询全部" autocomplete="off"></div><button :disabled="loading">查询</button></form><p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="loading" role="status">正在加载记录…</p><div class="table-wrap card"><table><caption>审计列表，共 {{ total }} 条</caption><thead><tr><th scope="col">时间 / ID</th><th scope="col">来源 / 操作者</th><th scope="col">操作</th><th scope="col">目标用户</th><th scope="col">详情</th></tr></thead><tbody><tr v-for="item in items" :key="item.id"><td>{{ time(item.created_at) }}<small>#{{ item.id }}</small></td><td>{{ item.actor_source }} / {{ item.actor_user_id ?? '本地命令' }}</td><td>{{ item.action }}</td><td>{{ item.target_user_id ?? '—' }}</td><td><code>{{ JSON.stringify(item.details) }}</code></td></tr><tr v-if="!items.length && !loading"><td colspan="5">没有匹配的审计记录。</td></tr></tbody></table></div><div class="pagination"><button class="quiet" :disabled="page <= 1 || loading" @click="page--; load()">上一页</button><span>第 {{ page }} 页</span><button class="quiet" :disabled="page * 20 >= total || loading" @click="page++; load()">下一页</button></div></section></template>
