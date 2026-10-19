<script setup>
// 食谱列表（方案 §5/§7）：普通模式提供「公共食谱/我的食谱」两个范围切换、
// q 检索与分页；我的范围可按状态筛选（公共范围按契约不接受 status）。
// 管理员模式由路由 props（admin=true）指明，复用同一视图走 /admin/recipes
// 全部状态公共食谱链路，客户端不提交 visibility。
// 列表行携带读取时派生的警示字段：已发布 ≠ 始终安全（食材可能后来被停用
// 或过敏原标注不完整），警示在列表与详情均可见。
import { ref, onMounted } from 'vue'
import { api } from '../api.js'
import { auth } from '../auth.js'

const props = defineProps({ admin: { type: Boolean, default: false } })
const generation = auth.generation
const PAGE_SIZE = 20
const items = ref([]), total = ref(0), page = ref(1)
const scope = ref('public'), q = ref(''), status = ref('')
const error = ref(''), loading = ref(false)
let sequence = 0

// 状态文案：私有 published 按方案 §2 显示「已完成」，公共显示「已发布」。
function statusText(item) {
  if (item.status === 'published') return item.visibility === 'private' ? '已完成' : '已发布'
  return item.status === 'draft' ? '草稿' : item.status === 'archived' ? '已归档' : '未知'
}
async function load(reset = false) {
  const keyword = q.value.trim()
  if ([...keyword].length > 100) { error.value = '搜索关键词去除首尾空白后最多 100 个字。'; return }
  if (reset) page.value = 1
  const own = ++sequence
  loading.value = true; error.value = ''
  try {
    const query = new URLSearchParams({ page: String(page.value), page_size: String(PAGE_SIZE) })
    if (keyword) query.set('q', keyword)
    if (props.admin) {
      if (status.value) query.set('status', status.value)
    } else {
      query.set('scope', scope.value)
      if (scope.value === 'mine' && status.value) query.set('status', status.value)
    }
    const result = await api(`${props.admin ? '/admin/recipes' : '/recipes'}?${query}`)
    if (own === sequence && generation === auth.generation) { items.value = result.items; total.value = result.total }
  } catch (e) { if (own === sequence) error.value = e.message } finally { if (own === sequence) loading.value = false }
}
// 切换范围时清空状态筛选（公共范围不接受 status）并回到第一页。
function switchScope(value) {
  if (scope.value === value) return
  scope.value = value
  status.value = ''
  load(true)
}
// 查看入口：详情走普通 /recipes/:id（仅已发布公共或本人私有可见）；
// 管理员模式下的草稿/归档行不提供查看，直接走编辑。
function canView(item) { return !props.admin || item.status === 'published' }
function canEdit(item) { return props.admin || scope.value === 'mine' }
onMounted(() => load())
</script>
<template><section>
  <p class="eyebrow">{{ admin ? '管理员工作台' : '食谱' }}</p>
  <h1>{{ admin ? '公共食谱管理' : '食谱' }}</h1>
  <p class="muted" v-if="admin">维护全部状态的公共食谱；公共食谱不属于任何个人账号。已发布 ≠ 始终安全：食材可能后来被停用或标注不完整，警示以读取时派生为准。</p>
  <p class="muted" v-else>浏览已发布的公共食谱，或管理自己的私有食谱。已发布 ≠ 始终安全：食材可能后来被停用或过敏原标注不完整，请以列表与详情页的警示为准。</p>

  <div v-if="!admin" class="scope-tabs" role="group" aria-label="食谱范围">
    <button type="button" :aria-pressed="scope === 'public'" @click="switchScope('public')">公共食谱</button>
    <button type="button" :aria-pressed="scope === 'mine'" @click="switchScope('mine')">我的食谱</button>
  </div>

  <form class="filters card" @submit.prevent="load(true)">
    <div><label for="recipe-q">名称</label><input id="recipe-q" v-model="q" autocomplete="off" placeholder="按名称字面包含搜索"></div>
    <div v-if="admin || scope === 'mine'"><label for="recipe-status">状态</label><select id="recipe-status" v-model="status"><option value="">全部</option><option value="draft">草稿</option><option value="published">{{ admin ? '已发布' : '已完成' }}</option><option value="archived">已归档</option></select></div>
    <button :disabled="loading">查询</button>
    <button v-if="admin" type="button" class="quiet" @click="$router.push('/admin/recipes/new')">新建公共食谱</button>
    <button v-else type="button" class="quiet" @click="$router.push('/recipes/new')">新建我的食谱</button>
  </form>

  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p v-if="loading" role="status">正在加载食谱…</p>

  <div class="table-wrap card">
    <table>
      <caption>{{ admin ? '公共食谱' : scope === 'mine' ? '我的食谱' : '公共食谱' }}，共 {{ total }} 个</caption>
      <thead><tr><th scope="col">名称</th><th scope="col">份数</th><th scope="col">耗时</th><th scope="col">状态</th><th scope="col">警示</th><th scope="col">操作</th></tr></thead>
      <tbody>
        <tr v-for="item in items" :key="item.id">
          <td>{{ item.name }}<small>#{{ item.id }} · v{{ item.version }}</small></td>
          <td>{{ item.servings }} 份</td>
          <td>{{ item.duration_minutes }} 分钟</td>
          <td><span class="badge" :class="`st-${item.status}`">{{ statusText(item) }}</span></td>
          <td>
            <template v-if="!item.restriction_metadata_complete || item.has_disabled_foods">
              <span v-if="!item.restriction_metadata_complete" class="badge warn">过敏原数据不完整</span>
              <span v-if="item.has_disabled_foods" class="badge stop">含停用食材</span>
            </template>
            <span v-else>—</span>
          </td>
          <td>
            <div class="actions">
              <RouterLink v-if="canView(item)" class="linklike" :to="`/recipes/${item.id}`">查看</RouterLink>
              <RouterLink v-if="canEdit(item)" class="linklike" :to="admin ? `/admin/recipes/${item.id}/edit` : `/recipes/${item.id}/edit`">编辑</RouterLink>
            </div>
          </td>
        </tr>
        <tr v-if="!items.length && !loading"><td colspan="6">没有匹配的食谱。{{ !admin && scope === 'mine' ? '可用上方「新建我的食谱」创建。' : '' }}</td></tr>
      </tbody>
    </table>
  </div>

  <div class="pagination">
    <button class="quiet" :disabled="page <= 1 || loading" @click="page--; load()">上一页</button>
    <span>第 {{ page }} 页</span>
    <button class="quiet" :disabled="page * PAGE_SIZE >= total || loading" @click="page++; load()">下一页</button>
  </div>
</section></template>
<style scoped>
/* 本页补充样式：范围切换按钮与警示徽标（文字 + 颜色，不只靠颜色表达）。 */
.scope-tabs { display: flex; gap: 8px; margin: 4px 0 14px; flex-wrap: wrap; }
.scope-tabs button { background: transparent; color: #285c41; border-color: #b6c5b7; }
.scope-tabs button[aria-pressed='true'] { background: #285c41; color: #fff; border-color: #285c41; }
.linklike { font-size: 14px; font-weight: 600; }
.badge { display: inline-block; padding: 2px 10px; border-radius: 999px; font-size: 12px; font-weight: 700; border: 1px solid currentColor; white-space: nowrap; margin: 2px 6px 2px 0; }
.badge.st-draft { color: #627467; background: #f0f2ef; }
.badge.st-published { color: #236344; background: #e9f4e6; }
.badge.st-archived { color: #3f5764; background: #e7eef2; }
.badge.warn { color: #8a5a00; background: #f1ecd9; }
.badge.stop { color: #9c2d24; background: #fff0eb; }
</style>
