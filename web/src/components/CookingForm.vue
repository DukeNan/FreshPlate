<script setup>
import { ref } from 'vue'
import { api } from '../api.js'
import { auth, verifyForm } from '../auth.js'
import { compareQuantity, quantityMilliString } from '../quantity.js'
import { unitLabel } from '../recommendations.js'

const props = defineProps({ recipe: { type: Object, required: true } })
const generation = auth.generation, expectedUserId = auth.user?.id
const open = ref(false), loading = ref(false), saving = ref(false), batches = ref([])
const error = ref(''), message = ref(''), pending = ref(null), uncertain = ref(false), loadFailed = ref(false)
const cooked = ref(String(props.recipe.servings)), eaten = ref('1')
const ateAt = ref(new Date(Date.now() + 8 * 60 * 60 * 1000).toISOString().slice(0, 16))
async function show() {
  open.value = true; loading.value = true; error.value = ''; loadFailed.value = false
  try {
    const all = []
    for (let page = 1; ; page++) {
      const result = await api(`/inventory-items?page=${page}&page_size=100`)
      if (generation !== auth.generation) return
      all.push(...result.items)
      if (all.length >= result.total || result.items.length === 0) break
    }
    const ids = new Set(props.recipe.ingredients.map(item => item.food.id))
    batches.value = all.filter(item => ids.has(item.food.id) && item.food.status === 'active' && item.expiry_state !== 'expired' && item.expiry_state !== 'depleted').map(item => ({ ...item, used: '' }))
  } catch (e) { loadFailed.value = true; error.value = e.message } finally { loading.value = false }
}
async function submit() {
  error.value = ''; message.value = ''
  if (!pending.value) {
    const c = quantityMilliString(cooked.value), e = quantityMilliString(eaten.value)
    if (!c || c === '0' || !e || e === '0' || compareQuantity(e, c) > 0) { error.value = '请输入有效份数，食用份数不能超过烹饪份数。'; return }
    const items = []
    for (const batch of batches.value) {
      if (!batch.used) continue
      const quantity = quantityMilliString(batch.used)
      if (!quantity || quantity === '0' || compareQuantity(quantity, quantityMilliString(batch.quantity)) > 0) { error.value = `请检查 ${batch.food.name} 的使用量。`; return }
      items.push({ inventory_id: batch.id, version: batch.version, quantity: batch.used })
    }
    if (!items.length) { error.value = '请至少填写一个库存批次的实际使用量。'; return }
    const selected = new Set(batches.value.filter(batch => batch.used).map(batch => batch.food.id))
    if (props.recipe.ingredients.some(item => !selected.has(item.food.id))) { error.value = '请为食谱中的每种食材填写至少一个库存批次的实际使用量。'; return }
    const date = new Date(`${ateAt.value}+08:00`)
    if (Number.isNaN(date.getTime())) { error.value = '请输入有效的就餐时间。'; return }
    pending.value = { request_key: crypto.randomUUID(), recipe_id: props.recipe.id, recipe_version: props.recipe.version, cooked_servings: cooked.value, eaten_servings: eaten.value, ate_at: date.toISOString().replace('.000Z', 'Z'), items }
  }
  saving.value = true
  try {
    await verifyForm(expectedUserId)
    if (generation !== auth.generation) return
    await api('/meals', { method: 'POST', expectedUserId, body: pending.value })
    if (generation !== auth.generation) return
    pending.value = null; uncertain.value = false; open.value = false
    message.value = '烹饪已确认，库存和饮食记录已保存。'
  } catch (e) {
    // This code is returned only after the idempotency check and a monotonic version change.
    if (uncertain.value && e.code === 'VERSION_CONFLICT') {
      pending.value = null; uncertain.value = false; error.value = e.message
    } else if (uncertain.value || e.code === 'NETWORK_ERROR' || e.code === 'INVALID_RESPONSE' || e.status >= 500) {
      uncertain.value = true
      error.value = '保存结果暂时未知。请点击重试以使用同一请求，或到饮食记录核对；不要另建相同记录。'
    } else { pending.value = null; error.value = e.message }
  } finally { saving.value = false }
}
</script>
<template>
  <p v-if="message" class="success" role="status">{{ message }} <RouterLink to="/meals">查看饮食记录</RouterLink></p>
  <section class="card" aria-label="烹饪确认">
    <h2>烹饪确认</h2>
    <p>填写实际使用的库存批次和份数，确认后将同时扣减库存并记录饮食。</p>
    <button v-if="!open" type="button" @click="show">填写烹饪记录</button>
    <p v-if="loading" role="status">正在加载库存…</p>
    <p v-if="loadFailed" class="error" role="alert">{{ error }} <button type="button" class="quiet" @click="show">重试加载库存</button></p>
    <form v-if="open && !loading && !loadFailed" @submit.prevent="submit">
      <fieldset :disabled="saving || !!pending || auth.checking || !!auth.checkError">
        <legend>实际份量和用料</legend>
        <label for="cooked-servings">烹饪份数</label><input id="cooked-servings" v-model="cooked" inputmode="decimal" required>
        <label for="eaten-servings">食用份数</label><input id="eaten-servings" v-model="eaten" inputmode="decimal" required>
        <label for="ate-at">就餐时间（中国标准时间）</label><input id="ate-at" v-model="ateAt" type="datetime-local" required>
        <p>每种食谱原料都须填写至少一个库存批次的实际使用量；留空表示不使用该批次。只显示未过期且启用的食材，数量单位与库存一致。提交时会重新检查饮食限制与过敏原数据。</p>
        <div v-for="batch in batches" :key="batch.id">
          <label :for="`used-${batch.id}`">{{ batch.food.name }} · 批次 #{{ batch.id }} · 可用 {{ batch.quantity }} {{ unitLabel(batch.unit) }} · 到期 {{ batch.expires_on || '未知' }}</label>
          <input :id="`used-${batch.id}`" v-model="batch.used" inputmode="decimal" :placeholder="`使用量（${unitLabel(batch.unit)}）`">
        </div>
        <p v-if="!batches.length">没有可用的食谱原料库存。<RouterLink to="/inventory">管理库存</RouterLink></p>
      </fieldset>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <div class="actions"><button type="submit" :disabled="saving || auth.checking || !!auth.checkError || !batches.length">{{ saving ? '正在保存…' : pending ? '重试保存' : '确认烹饪并记餐' }}</button><button v-if="!pending" type="button" class="quiet" :disabled="saving" @click="open = false">取消</button></div>
    </form>
  </section>
</template>
