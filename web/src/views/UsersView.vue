<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api.js'
import { auth, verifyForm } from '../auth.js'
import { passwordError } from '../validation.js'
const expectedUserId = auth.user?.id
const generation = auth.generation
const items = ref([]), total = ref(0), page = ref(1), username = ref(''), role = ref(''), status = ref(''), error = ref(''), message = ref(''), busy = ref(false), loading = ref(false)
const target = ref(null), temporary = ref('')
let listSequence = 0
async function load(reset = false) {
  if (reset) page.value = 1
  const sequence = ++listSequence
  loading.value = true; error.value = ''
  try {
    const query = new URLSearchParams({ page: String(page.value), page_size: '20', username: username.value, role: role.value, status: status.value })
    const result = await api(`/admin/users?${query}`)
    if (sequence === listSequence && generation === auth.generation) { items.value = result.items; total.value = result.total }
  } catch (e) { if (sequence === listSequence) error.value = e.message } finally { if (sequence === listSequence) loading.value = false }
}
async function change(user, field, value) {
  if (!window.confirm(`确认将用户“${user.username}”的${field === 'role' ? '角色' : '状态'}改为“${value}”？此操作可能撤销其会话。`)) return
  busy.value = true; error.value = ''; message.value = ''
  try { await verifyForm(expectedUserId); await api(`/admin/users/${user.id}`, { method: 'PATCH', expectedUserId, body: { [field]: value } }); message.value = '账号已更新。'; await load() }
  catch (e) { error.value = e.message } finally { busy.value = false }
}
async function reset() {
  error.value = ''
  if (passwordError(temporary.value)) { error.value = passwordError(temporary.value); return }
  if (!window.confirm(`确认重置“${target.value.username}”的密码？其全部会话将撤销，下次登录必须改密。请通过独立渠道交付临时密码。`)) return
  busy.value = true
  try { await verifyForm(expectedUserId); await api(`/admin/users/${target.value.id}/reset-password`, { method: 'POST', expectedUserId, body: { temporary_password: temporary.value } }); temporary.value = ''; target.value = null; message.value = '密码已重置，请通过独立渠道交付临时密码。'; await load() }
  catch (e) { error.value = e.message } finally { busy.value = false }
}
onMounted(() => load())
</script>
<template><section><p class="eyebrow">管理员工作台</p><h1>用户管理</h1><p class="muted">仅管理账号权限与状态；请在个人页面修改自己的密码。</p><form class="filters card" @submit.prevent="load(true)"><div><label for="filter-name">用户名</label><input id="filter-name" v-model="username" autocomplete="off"></div><div><label for="filter-role">角色</label><select id="filter-role" v-model="role"><option value="">全部</option><option value="user">用户</option><option value="admin">管理员</option></select></div><div><label for="filter-status">状态</label><select id="filter-status" v-model="status"><option value="">全部</option><option value="active">启用</option><option value="disabled">停用</option></select></div><button :disabled="loading || busy">查询</button></form><p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="message" class="success" role="status">{{ message }}</p><p v-if="loading" role="status">正在加载…</p><div class="table-wrap card"><table><caption>用户列表，共 {{ total }} 人</caption><thead><tr><th scope="col">用户</th><th scope="col">角色</th><th scope="col">状态</th><th scope="col">密码</th><th scope="col">操作</th></tr></thead><tbody><tr v-for="user in items" :key="user.id"><td>{{ user.username }}<small>#{{ user.id }}{{ user.id === expectedUserId ? ' · 当前账号' : '' }}</small></td><td>{{ user.role === 'admin' ? '管理员' : '用户' }}</td><td>{{ user.status === 'active' ? '启用' : '停用' }}</td><td>{{ user.must_change_password ? '待改密' : '正常' }}</td><td><div class="actions" v-if="user.id !== expectedUserId"><button class="quiet" :disabled="busy || loading || auth.checking || !!auth.checkError" @click="change(user, 'role', user.role === 'admin' ? 'user' : 'admin')">{{ user.role === 'admin' ? '降为用户' : '设为管理员' }}</button><button class="quiet" :disabled="busy || loading || auth.checking || !!auth.checkError" @click="change(user, 'status', user.status === 'active' ? 'disabled' : 'active')">{{ user.status === 'active' ? '停用' : '启用' }}</button><button class="quiet" :disabled="busy || loading || auth.checking || !!auth.checkError" @click="target = user; temporary = ''">重置密码</button></div><span v-else>请使用个人设置</span></td></tr><tr v-if="!items.length && !loading"><td colspan="5">没有匹配的用户。</td></tr></tbody></table></div><div class="pagination"><button class="quiet" :disabled="page <= 1 || loading || busy" @click="page--; load()">上一页</button><span>第 {{ page }} 页</span><button class="quiet" :disabled="page * 20 >= total || loading || busy" @click="page++; load()">下一页</button></div><section v-if="target" class="card"><h2>重置 {{ target.username }} 的密码</h2><form @submit.prevent="reset"><label for="temporary">临时密码</label><input id="temporary" v-model="temporary" type="password" autocomplete="new-password" required maxlength="256"><small>12–128 个字符，不要求特定字符组合。请通过独立渠道交付。</small><div class="actions"><button :disabled="busy || auth.checking || !!auth.checkError">确认重置</button><button type="button" class="quiet" :disabled="busy" @click="target = null; temporary = ''">取消</button></div></form></section></section></template>
