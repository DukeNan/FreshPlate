import { createRouter, createWebHistory } from 'vue-router'
import { auth, initAuth } from './auth.js'
import AccountView from './views/AccountView.vue'
import PreferencesView from './views/PreferencesView.vue'
import PasswordView from './views/PasswordView.vue'
import UsersView from './views/UsersView.vue'
import AuditView from './views/AuditView.vue'
import FoodCatalogView from './views/FoodCatalogView.vue'
import InventoryView from './views/InventoryView.vue'
import FoodsView from './views/FoodsView.vue'
import ResourceAuditView from './views/ResourceAuditView.vue'
import RecipesView from './views/RecipesView.vue'
import RecipeDetailView from './views/RecipeDetailView.vue'
import RecipeEditView from './views/RecipeEditView.vue'
export const router = createRouter({ history: createWebHistory(), routes: [
  { path: '/', redirect: '/settings' },
  { path: '/login', component: AccountView, props: { register: false }, meta: { public: true } },
  { path: '/register', component: AccountView, props: { register: true }, meta: { public: true } },
  { path: '/settings', component: PreferencesView },
  { path: '/password', component: PasswordView },
  { path: '/foods', component: FoodCatalogView },
  { path: '/inventory', component: InventoryView },
  // 食谱模块（方案 §7）：普通私有链路与管理员公共链路复用同一组视图，
  // 由路由 props 区分模式；visibility/owner 由模式决定，不由表单提交。
  { path: '/recipes', component: RecipesView, props: { admin: false } },
  { path: '/recipes/new', component: RecipeEditView, props: { admin: false, recipeId: 0 } },
  { path: '/recipes/:id', component: RecipeDetailView, props: true },
  { path: '/recipes/:id/edit', component: RecipeEditView, props: route => ({ admin: false, recipeId: Number(route.params.id) }) },
  { path: '/admin/recipes', component: RecipesView, props: { admin: true }, meta: { admin: true } },
  { path: '/admin/recipes/new', component: RecipeEditView, props: { admin: true, recipeId: 0 }, meta: { admin: true } },
  { path: '/admin/recipes/:id/edit', component: RecipeEditView, props: route => ({ admin: true, recipeId: Number(route.params.id) }), meta: { admin: true } },
  { path: '/admin/users', component: UsersView, meta: { admin: true } },
  { path: '/admin/audit', component: AuditView, meta: { admin: true } },
  { path: '/admin/foods', component: FoodsView, meta: { admin: true } },
  { path: '/admin/resource-audit', component: ResourceAuditView, meta: { admin: true } },
  { path: '/:pathMatch(.*)*', redirect: '/settings' },
] })
router.beforeEach(async to => {
  await initAuth()
  if (auth.user?.must_change_password && to.path !== '/password') return '/password'
  if (!to.meta.public && !auth.user) return { path: '/login', query: { return: to.fullPath } }
  if (to.meta.admin && auth.user?.role !== 'admin') return '/settings'
  if (to.meta.public && auth.user) return '/settings'
})
