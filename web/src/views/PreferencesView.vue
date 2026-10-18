<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api.js'
import { auth, verifyForm } from '../auth.js'
const expectedUserId = auth.user?.id
const generation = auth.generation
const retainedDisabled = ref([])
const form = ref(null), foods = ref([]), error = ref(''), message = ref(''), busy = ref(false), loading = ref(true)
const allergens = { milk: '乳', egg: '蛋', fish: '鱼', crustacean: '甲壳类', mollusc: '软体动物', peanut: '花生', soy: '大豆', wheat: '小麦', tree_nut: '树坚果', sesame: '芝麻' }
const preferences = { vegetarian: '素食', vegan: '纯素', low_salt: '低盐' }
const targets = [{ key: 'daily_energy_kcal', name: '能量（千卡）', max: 20000 }, { key: 'daily_protein_g', name: '蛋白质（克）', max: 2000 }, { key: 'daily_fat_g', name: '脂肪（克）', max: 2000 }, { key: 'daily_carbohydrate_g', name: '碳水化合物（克）', max: 2000 }]
async function load() {
  loading.value = true; error.value = ''
  try {
    const [settings, options] = await Promise.all([api('/me/preferences'), api('/foods')])
    if (generation !== auth.generation) return
    if (!settings || !Array.isArray(settings.allergens) || !Array.isArray(settings.excluded_food_ids) || !Array.isArray(settings.dietary_preferences) || !Array.isArray(settings.excluded_foods) || !Array.isArray(options) || targets.some(t => !(t.key in settings))) throw new Error('设置数据不完整，已暂停编辑。')
    if (settings.allergens.some(v => !(v in allergens)) || settings.dietary_preferences.some(v => !(v in preferences))) throw new Error('存在此版本无法识别的设置，请联系管理员，避免覆盖原有饮食限制。')
    foods.value = [...new Map([...options, ...settings.excluded_foods].map(food => [food.id, food])).values()]
    if (settings.excluded_food_ids.some(id => !foods.value.some(food => food.id === id))) throw new Error('忌口食材信息缺失，已暂停编辑。')
    if (foods.value.some(food => !['active', 'disabled'].includes(food.status))) throw new Error('无法识别食材状态，已暂停编辑。')
    retainedDisabled.value = settings.excluded_food_ids.filter(id => foods.value.some(food => food.id === id && food.status === 'disabled'))
    form.value = settings
  } catch (e) { if (generation === auth.generation) { form.value = null; error.value = e.message } }
  finally { loading.value = false }
}
async function save() {
  busy.value = true; error.value = ''; message.value = ''
  try {
    await verifyForm(expectedUserId)
    const body = { allergens: form.value.allergens, excluded_food_ids: form.value.excluded_food_ids, dietary_preferences: form.value.dietary_preferences }
    for (const { key } of targets) body[key] = form.value[key] === '' || form.value[key] === null ? null : Number(form.value[key])
    const result = await api('/me/preferences', { method: 'PUT', expectedUserId, body })
    if (generation !== auth.generation) return
    form.value = result; retainedDisabled.value = result.excluded_food_ids.filter(id => foods.value.some(food => food.id === id && food.status === 'disabled')); message.value = '已保存。本次成功保存将覆盖此前的设置。'
  } catch (e) { error.value = e.message } finally { busy.value = false }
}
onMounted(load)
</script>
<template><section><p class="eyebrow">你的饮食档案</p><h1>个人饮食设置</h1><p class="muted">过敏与忌口为强制限制，饮食偏好仅影响排序。未列出的特殊过敏需求暂不支持。</p><p v-if="loading" role="status">正在加载设置…</p><p v-if="error" class="error" role="alert">{{ error }}</p><button v-if="!form && !loading" @click="load" :disabled="auth.checking || !!auth.checkError">重新加载设置</button><form v-if="form" @submit.prevent="save"><fieldset :disabled="busy || auth.checking || !!auth.checkError"><legend class="sr-only">个人饮食设置</legend><div class="card"><h2>过敏原 · 强制限制</h2><div class="choices"><label v-for="(name, code) in allergens" :key="code"><input type="checkbox" v-model="form.allergens" :value="code">{{ name }}</label></div></div><div class="card"><h2>忌口食材 · 强制限制</h2><p class="muted">已选停用食材会保留限制，只有取消勾选并保存才会移除。</p><div class="choices"><label v-for="food in foods" :key="food.id"><input type="checkbox" v-model="form.excluded_food_ids" :value="food.id" :disabled="food.status === 'disabled' && !retainedDisabled.includes(food.id)">{{ food.name }}{{ food.status === 'disabled' ? '（已停用）' : '' }}</label></div><p v-if="!foods.length">暂无可选公共食材。</p></div><div class="card"><h2>饮食偏好 · 排序参考</h2><div class="choices"><label v-for="(name, code) in preferences" :key="code"><input type="checkbox" v-model="form.dietary_preferences" :value="code">{{ name }}</label></div></div><div class="card"><h2>每日营养参考目标</h2><p class="muted">留空表示不设置。数值范围用于输入保护，不代表医学建议。</p><div class="grid"><div v-for="target in targets" :key="target.key"><label :for="target.key">{{ target.name }}</label><input :id="target.key" v-model="form[target.key]" type="number" min="0.000001" :max="target.max" step="any" inputmode="decimal"></div></div></div><p v-if="message" class="success" role="status">{{ message }}</p><button type="submit">{{ busy ? '正在保存…' : '保存完整设置' }}</button></fieldset></form></section></template>
