<script setup>
import { onMounted, ref } from 'vue'
import { api } from '../api.js'
import { auth } from '../auth.js'

const generation = auth.generation
const items = ref([]), total = ref(0), page = ref(1), loading = ref(false), error = ref('')
let listSequence = 0
async function load() {
  const sequence = ++listSequence
  loading.value = true; error.value = ''
  try {
    const result = await api(`/meals?page=${page.value}&page_size=20`)
    if (sequence === listSequence && generation === auth.generation) { items.value = result.items; total.value = result.total }
  } catch (e) { if (sequence === listSequence && generation === auth.generation) error.value = e.message } finally { if (sequence === listSequence) loading.value = false }
}
onMounted(load)
</script>
<template><section>
  <p class="eyebrow">我的饮食</p><h1>饮食记录</h1>
  <p>在食谱详情中确认烹饪后，这里会显示实际食用记录。</p>
  <p v-if="error" class="error" role="alert">{{ error }} <button type="button" class="quiet" @click="load">重试</button></p>
  <p v-if="loading" role="status">正在加载饮食记录…</p>
  <div class="table-wrap card"><table><caption>饮食记录，共 {{ total }} 条</caption><thead><tr><th scope="col">就餐时间</th><th scope="col">食谱</th><th scope="col">烹饪份数</th><th scope="col">食用份数</th></tr></thead><tbody>
    <tr v-for="item in items" :key="item.id"><td>{{ new Date(item.ate_at).toLocaleString('zh-CN') }}</td><td>{{ item.recipe_name }}</td><td>{{ item.cooked_servings }}</td><td>{{ item.eaten_servings }}</td></tr>
    <tr v-if="!loading && !items.length"><td colspan="4">暂无饮食记录。</td></tr>
  </tbody></table></div>
  <div class="pagination"><button class="quiet" :disabled="loading || page <= 1" @click="page--; load()">上一页</button><span>第 {{ page }} 页</span><button class="quiet" :disabled="loading || page * 20 >= total" @click="page++; load()">下一页</button></div>
</section></template>
