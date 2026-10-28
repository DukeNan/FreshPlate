<script setup>
import { chinaDate } from '../date.js'
import { onMounted, ref } from 'vue'
import { api } from '../api.js'
import { auth } from '../auth.js'

const generation = auth.generation
const end = ref(chinaDate()), days = ref([]), loading = ref(false), error = ref('')
let listSequence = 0
function display(value) { return value == null ? '未知' : Number(value).toLocaleString('zh-CN', { maximumFractionDigits: 2 }) }
async function load() {
  const sequence = ++listSequence
  loading.value = true; error.value = ''; days.value = []
  try {
    const result = await api(`/nutrition?${new URLSearchParams({ end: end.value })}`)
    if (sequence === listSequence && generation === auth.generation) days.value = result.days
  } catch (e) { if (sequence === listSequence && generation === auth.generation) error.value = e.message } finally { if (sequence === listSequence) loading.value = false }
}
onMounted(load)
</script>
<template><section>
  <p class="eyebrow">我的饮食</p><h1>营养概览</h1>
  <p>按中国标准时间日期显示截止日期及之前六天的每日估算，仅供日常参考。数据或食材换算不完整时显示未知，不按零计算。</p>
  <form class="filters card" @submit.prevent="load"><div><label for="nutrition-end">截止日期（中国标准时间）</label><input id="nutrition-end" v-model="end" type="date" required></div><button :disabled="loading">查询</button></form>
  <p v-if="error" class="error" role="alert">{{ error }} <button type="button" class="quiet" @click="load">重试</button></p>
  <p v-if="loading" role="status">正在加载营养概览…</p>
  <div v-if="days.length" class="table-wrap card"><table><caption>每日营养估算与近七天趋势</caption><thead><tr><th scope="col">日期（中国标准时间）</th><th scope="col">餐数</th><th scope="col">能量（千卡）</th><th scope="col">蛋白质（克）</th><th scope="col">脂肪（克）</th><th scope="col">碳水化合物（克）</th></tr></thead><tbody>
    <tr v-for="day in days" :key="day.date"><td>{{ day.date }}</td><td>{{ day.meals_count }}</td><td>{{ display(day.energy_kcal) }}</td><td>{{ display(day.protein_g) }}</td><td>{{ display(day.fat_g) }}</td><td>{{ display(day.carbs_g) }}</td></tr>
  </tbody></table></div>
</section></template>
