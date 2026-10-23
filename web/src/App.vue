<script setup>
import { watch } from 'vue'
import { useRoute } from 'vue-router'
import { auth, checkIdentity, logoutCurrentSession } from './auth.js'
import { router } from './router.js'
import { ref } from 'vue'
const route = useRoute()
const error = ref('')
const busy = ref(false)
watch(() => [auth.user?.id, auth.user?.role, auth.user?.must_change_password], () => {
  if (auth.user?.must_change_password) router.replace('/password')
  else if (!auth.user && !route.meta.public) router.replace('/login')
  else if (route.meta.admin && auth.user?.role !== 'admin') router.replace('/settings')
  else if (auth.user && route.meta.public) router.replace('/settings')
})
async function logout() {
  busy.value = true; error.value = ''
  try { await logoutCurrentSession(); await router.replace('/login') }
  catch (e) { error.value = e.message }
  finally { busy.value = false }
}
</script>
<template>
  <header><RouterLink class="brand" to="/settings">FreshPlate<span>让每一餐更安心</span></RouterLink><nav aria-label="主导航" v-if="auth.user"><RouterLink v-if="!auth.user.must_change_password" to="/settings">饮食设置</RouterLink><RouterLink v-if="!auth.user.must_change_password" to="/foods">食材目录</RouterLink><RouterLink v-if="!auth.user.must_change_password" to="/inventory">我的库存</RouterLink><RouterLink v-if="!auth.user.must_change_password" to="/recipes">食谱</RouterLink><RouterLink v-if="!auth.user.must_change_password" to="/recommendations">食谱推荐</RouterLink><RouterLink to="/password">修改密码</RouterLink><template v-if="auth.user.role === 'admin' && !auth.user.must_change_password"><RouterLink to="/admin/users">用户管理</RouterLink><RouterLink to="/admin/audit">审计记录</RouterLink><RouterLink to="/admin/foods">食材管理</RouterLink><RouterLink to="/admin/recipes">公共食谱</RouterLink><RouterLink to="/admin/resource-audit">资源审计</RouterLink></template><button type="button" class="quiet" :disabled="busy || auth.checking || !!auth.checkError" @click="logout">退出 {{ auth.user.username }}</button></nav></header>
  <main><div class="notice" v-if="auth.checking" role="status">正在核对当前身份，提交暂时暂停…</div><div class="error" v-if="auth.checkError" role="alert">{{ auth.checkError }} <button type="button" :disabled="auth.checking" @click="checkIdentity">重试身份检查</button></div><p class="error" v-if="error" role="alert">{{ error }}</p><RouterView :key="`${route.path}:${auth.generation}`" /></main>
  <footer>过敏与忌口为强制限制；营养目标仅供个人参考。</footer>
</template>
