<script setup>
import { ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api.js'
import { passwordError } from '../validation.js'
import { auth, acceptLogin, checkIdentity } from '../auth.js'
import { router } from '../router.js'
import { safeReturn } from '../navigation.js'
const props = defineProps({ register: Boolean })
const route = useRoute()
const username = ref(''), password = ref(''), error = ref(''), message = ref(''), busy = ref(false)
async function submit() {
  error.value = ''; message.value = ''
  if (props.register && passwordError(password.value)) { error.value = passwordError(password.value); return }
  busy.value = true
  try {
    if (!await checkIdentity()) throw new Error('身份检查失败，请重试。')
    if (auth.user) { await router.replace('/settings'); return }
    const result = await api(props.register ? '/auth/register' : '/auth/login', { method: 'POST', body: { username: username.value, password: password.value } })
    password.value = ''
    if (props.register) { message.value = '注册成功，请登录。'; await router.push({ path: '/login', query: { registered: '1' } }) }
    else { acceptLogin(result); await router.replace(result.user.must_change_password ? '/password' : safeReturn(route.query.return)) }
  } catch (e) { error.value = e.message } finally { busy.value = false }
}
</script>
<template><section class="card account"><p class="eyebrow">欢迎来到 FRESHPLATE</p><h1>{{ register ? '创建账号' : '登录账号' }}</h1><p class="muted">保存你的饮食限制与个人偏好。</p><p v-if="route.query.registered" role="status">注册成功，请登录。</p><p v-if="route.query.changed" role="status">密码已修改，请重新登录。</p><form @submit.prevent="submit"><label for="username">用户名</label><input id="username" v-model.trim="username" autocomplete="username" required minlength="3" maxlength="32" pattern="[A-Za-z0-9_]{3,32}" aria-describedby="username-help"><small id="username-help">3–32 位英文字母、数字或下划线，不区分大小写。</small><label for="password">密码</label><input id="password" v-model="password" type="password" :autocomplete="register ? 'new-password' : 'current-password'" required maxlength="256"><small v-if="register">12–128 个字符，不要求特定字符组合。</small><p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="message" role="status">{{ message }}</p><button :disabled="busy || auth.checking || !!auth.checkError">{{ busy ? '正在提交…' : register ? '注册' : '登录' }}</button></form><p><RouterLink :to="register ? '/login' : '/register'">{{ register ? '已有账号？去登录' : '还没有账号？注册' }}</RouterLink></p></section></template>
