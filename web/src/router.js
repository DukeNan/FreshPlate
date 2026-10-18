import { createRouter, createWebHistory } from 'vue-router'
import { auth, initAuth } from './auth.js'
import AccountView from './views/AccountView.vue'
import PreferencesView from './views/PreferencesView.vue'
import PasswordView from './views/PasswordView.vue'
import UsersView from './views/UsersView.vue'
import AuditView from './views/AuditView.vue'
export const router = createRouter({ history: createWebHistory(), routes: [
  { path: '/', redirect: '/settings' },
  { path: '/login', component: AccountView, props: { register: false }, meta: { public: true } },
  { path: '/register', component: AccountView, props: { register: true }, meta: { public: true } },
  { path: '/settings', component: PreferencesView },
  { path: '/password', component: PasswordView },
  { path: '/admin/users', component: UsersView, meta: { admin: true } },
  { path: '/admin/audit', component: AuditView, meta: { admin: true } },
  { path: '/:pathMatch(.*)*', redirect: '/settings' },
] })
router.beforeEach(async to => {
  await initAuth()
  if (auth.user?.must_change_password && to.path !== '/password') return '/password'
  if (!to.meta.public && !auth.user) return { path: '/login', query: { return: to.fullPath } }
  if (to.meta.admin && auth.user?.role !== 'admin') return '/settings'
  if (to.meta.public && auth.user) return '/settings'
})
