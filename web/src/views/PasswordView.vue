<script setup>
import { ref } from 'vue'
import { passwordError } from '../validation.js'
import { auth, changePassword } from '../auth.js'
import { router } from '../router.js'
const expectedUserId = auth.user?.id
const current = ref(''), next = ref(''), repeat = ref(''), busy = ref(false), error = ref('')
async function submit() {
  error.value = ''
  if (passwordError(next.value)) { error.value = passwordError(next.value); return }
  if (next.value !== repeat.value) { error.value = '两次输入的新密码不一致。'; return }
  busy.value = true
  try { await changePassword(expectedUserId, { current_password: current.value, new_password: next.value }); current.value = ''; next.value = ''; repeat.value = ''; await router.replace({ path: '/login', query: { changed: '1' } }) }
  catch (e) { error.value = e.message } finally { busy.value = false }
}
</script>
<template><section class="card account"><h1>修改密码</h1><p v-if="auth.user?.must_change_password" class="notice">请先更换管理员提供的临时密码，再使用其他功能。</p><p class="muted">修改成功后将退出所有设备，需要重新登录。</p><form @submit.prevent="submit"><label for="current">当前密码</label><input id="current" v-model="current" type="password" autocomplete="current-password" required maxlength="256"><label for="next">新密码</label><input id="next" v-model="next" type="password" autocomplete="new-password" required maxlength="256"><small>12–128 个字符，不要求特定字符组合，不能沿用当前密码。</small><label for="repeat">再次输入新密码</label><input id="repeat" v-model="repeat" type="password" autocomplete="new-password" required maxlength="256"><p v-if="error" class="error" role="alert">{{ error }}</p><button :disabled="busy || auth.checking || !!auth.checkError">{{ busy ? '正在修改…' : '修改并重新登录' }}</button></form></section></template>
