<script setup>
// 食谱详情（方案 §5/§7）：只读展示份数、耗时、原料（position + 数量单位 +
// 停用标记）、步骤与来源。全部内容使用文本插值渲染，绝不用 v-html，也不解析
// Markdown；来源按纯文本显示，避免链接协议风险。未发布/已撤回公共、他人
// 私有与不存在一律 404，页面统一提示，不区分原因。仅本人私有食谱显示编辑
// 入口（公共食谱不可编辑）；不提供任何烹饪或库存扣减按钮。
import { ref, computed, onMounted } from 'vue'
import { api } from '../api.js'
import { auth } from '../auth.js'

const props = defineProps({ id: { type: String, required: true } })
const generation = auth.generation
const recipeId = Number(props.id)
const detail = ref(null), error = ref(''), notFound = ref(false), loading = ref(false)
const allergens = { milk: '乳', egg: '蛋', fish: '鱼', crustacean: '甲壳类', mollusc: '软体动物', peanut: '花生', soy: '大豆', wheat: '小麦', tree_nut: '树坚果', sesame: '芝麻' }
let sequence = 0
const invalid = computed(() => !Number.isSafeInteger(recipeId) || recipeId < 1)
// 仅私有食谱可编辑：普通详情只返回已发布公共与本人私有，private 即本人所有。
const editable = computed(() => !!detail.value && detail.value.visibility === 'private')
function statusText(d) {
  if (d.status === 'published') return d.visibility === 'private' ? '已完成' : '已发布'
  return d.status === 'draft' ? '草稿' : d.status === 'archived' ? '已归档' : '未知'
}
function names(list) { return (list || []).map(code => allergens[code] || code).join('、') }
function time(value) { return new Date(typeof value === 'number' ? value * 1000 : value).toLocaleString('zh-CN') }
async function load() {
  if (invalid.value) { error.value = '食谱编号无效。'; return }
  const own = ++sequence
  loading.value = true; error.value = ''; notFound.value = false
  try {
    const result = await api(`/recipes/${recipeId}`)
    if (own === sequence && generation === auth.generation) detail.value = result
  } catch (e) {
    if (own === sequence) {
      // 404 不区分原因（不存在/未发布/他人私有），按方案 §5 统一提示。
      if (e.status === 404) notFound.value = true
      else error.value = e.message
    }
  } finally { if (own === sequence) loading.value = false }
}
onMounted(load)
</script>
<template><section>
  <p class="eyebrow">食谱</p>
  <template v-if="invalid"><p class="error" role="alert">食谱编号无效。</p><p><RouterLink to="/recipes">返回食谱列表</RouterLink></p></template>
  <template v-else>
    <p v-if="loading" role="status">正在加载食谱…</p>
    <template v-if="notFound">
      <h1>未找到该食谱</h1>
      <p class="error" role="alert">未找到该食谱：它可能不存在、尚未发布，或不属于当前账号。</p>
      <p><RouterLink to="/recipes">返回食谱列表</RouterLink></p>
    </template>
    <p v-else-if="error" class="error" role="alert">{{ error }} <button type="button" class="quiet" @click="load">重试</button></p>
    <template v-else-if="detail">
      <h1>{{ detail.name }}</h1>
      <p class="muted">#{{ detail.id }} · v{{ detail.version }} · 更新于 {{ time(detail.updated_at) }} · {{ detail.visibility === 'private' ? '我的私有食谱' : '公共食谱' }} · {{ statusText(detail) }}</p>

      <!-- 读取时派生的安全提示（方案 §4）：先于内容展示，避免「已发布=始终安全」误读。 -->
      <p v-if="!detail.restriction_metadata_complete" class="error" role="alert">过敏原数据不完整，不能作为安全依据：部分食材的过敏原标注尚未核对完整。</p>
      <p v-if="detail.has_disabled_foods" class="notice" role="alert">该食谱引用了已停用的食材：请核对原料列表后自行判断是否适用。</p>
      <p class="muted">已发布 ≠ 始终安全；本页面仅为数据展示，不构成推荐，也不代表适合当前账号食用。</p>

      <div class="actions"><RouterLink v-if="editable" class="linklike" :to="`/recipes/${detail.id}/edit`">编辑</RouterLink><RouterLink class="linklike" to="/recipes">返回列表</RouterLink></div>

      <div class="card">
        <h2>基本信息</h2>
        <table><caption class="sr-only">食谱基本信息</caption><tbody>
          <tr><th scope="row">份数</th><td>{{ detail.servings }} 份</td></tr>
          <tr><th scope="row">预计总耗时</th><td>{{ detail.duration_minutes }} 分钟</td></tr>
          <tr><th scope="row">来源（纯文本）</th><td>{{ detail.source || '未填写' }}</td></tr>
        </tbody></table>
      </div>

      <div class="card">
        <h2>原料（{{ detail.ingredients.length }}）</h2>
        <div class="table-wrap"><table><caption class="sr-only">原料列表，按加入顺序排列</caption>
          <thead><tr><th scope="col">顺序</th><th scope="col">食材</th><th scope="col">数量 / 单位</th></tr></thead>
          <tbody>
            <tr v-for="item in detail.ingredients" :key="item.position">
              <td>{{ item.position }}</td>
              <td>{{ item.food.name }}<small>#{{ item.food.id }}{{ item.food.status === 'disabled' ? ' · 已停用' : '' }}</small></td>
              <td>{{ item.quantity }} {{ item.unit === 'g' ? '克' : item.unit === 'ml' ? '毫升' : item.unit === 'piece' ? '个' : item.unit }}</td>
            </tr>
            <tr v-if="!detail.ingredients.length"><td colspan="3">未记录原料。</td></tr>
          </tbody>
        </table></div>
      </div>

      <div class="card">
        <h2>步骤（{{ detail.steps.length }}）</h2>
        <ol class="steps">
          <li v-for="(step, index) in detail.steps" :key="index">{{ step }}</li>
        </ol>
        <p v-if="!detail.steps.length" class="muted">未记录步骤。</p>
      </div>

      <div class="card">
        <h2>过敏原标注</h2>
        <table><caption class="sr-only">过敏原标注信息</caption><tbody>
          <tr><th scope="row">作者附加过敏原</th><td>{{ detail.additional_allergens.length ? names(detail.additional_allergens) : '无附加标注' }}</td></tr>
          <tr><th scope="row">有效过敏原（食材当前标注 ∪ 附加标注）</th><td>{{ detail.effective_allergens.length ? names(detail.effective_allergens) : '支持范围内无标注项' }}</td></tr>
          <tr><th scope="row">作者核对状态</th><td>{{ detail.allergen_complete ? '已核对' : '未核对' }}</td></tr>
          <tr><th scope="row">核对依据</th><td>{{ detail.allergen_source || '未填写' }}</td></tr>
        </tbody></table>
        <p class="muted">未列出的特殊过敏风险不在支持范围内；「未核对」表示作者尚未完成食谱层面的额外风险核对。</p>
      </div>
    </template>
  </template>
</section></template>
<style scoped>
/* 本页补充样式：步骤保留换行；编辑入口链接沿用按钮观感。 */
ol.steps li { white-space: pre-wrap; overflow-wrap: anywhere; margin: 8px 0; }
.linklike { font-size: 14px; font-weight: 600; }
th[scope='row'] { text-align: left; white-space: nowrap; width: 1%; }
</style>
