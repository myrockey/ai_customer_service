import { createRouter, createWebHashHistory } from 'vue-router'
import { session, loadMenus } from './api'

// 静态路由：登录页
const staticRoutes = [
  { path: '/login', name: 'login', meta: { title: '登录' }, component: () => import('./views/Login.vue') },
  { path: '/login-account', name: 'login-account', meta: { title: '账号登录' }, component: () => import('./views/LoginAccount.vue') }
]

const router = createRouter({
  history: createWebHashHistory('/admin/'),
  routes: staticRoutes
})

// ============ 动态路由（DB 菜单驱动，若依模式） ============
// component 字段形如 views/Ops.vue，映射到 import.meta.glob 的 key：./views/Ops.vue
const viewModules = import.meta.glob('./views/**/*.vue')
const dynamicNames = [] // 已注册动态路由 name，登出时移除

function collectMenu(node, seen) {
  if (!node || node.type === 3) return // 按钮仅作权限码，不建路由
  if (node.type === 2 && node.path && node.component) {
    // 业务中心与系统管理存在同 path 页面（如 /prompts），组件相同，去重只注册一次
    if (seen.has(node.path)) return
    seen.add(node.path)
    const key = './' + node.component
    const comp = viewModules[key]
    if (!comp) return
    const name = 'dyn-' + node.path.replace(/\//g, '_')
    router.addRoute({ path: node.path, name, meta: { title: node.name, perm: node.perm || '' }, component: comp })
    dynamicNames.push(name)
    return
  }
  for (const c of (node.children || [])) collectMenu(c, seen)
}

// 注册当前用户可见的动态路由（每次调用先清旧，避免权限残留）
export function registerDynamicRoutes() {
  resetDynamicRoutes()
  const seen = new Set()
  for (const root of session.menus || []) {
    for (const node of root.children || []) collectMenu(node, seen)
  }
}

// 登出 / 切换账号时移除全部动态路由
export function resetDynamicRoutes() {
  for (const n of dynamicNames) { try { router.removeRoute(n) } catch (e) {} }
  dynamicNames.length = 0
  dynamicRoutesReady = false
}

// 首页：菜单里第一个可访问页面（默认运营看板 /ops）
function findFirstPage(node) {
  if (node.type === 2 && node.path) return node.path
  for (const c of (node.children || [])) {
    const p = findFirstPage(c)
    if (p) return p
  }
  return ''
}
function firstPage() {
  for (const root of session.menus || []) {
    for (const node of root.children || []) {
      const p = findFirstPage(node)
      if (p) return p
    }
  }
  return '/login'
}

let dynamicRoutesReady = false

// 登录守卫：未登录跳登录页；确保菜单已加载 + 动态路由已注册。
// 菜单为 DB 驱动，任何整页刷新/首次进入都重新拉取最新菜单树（loadMenus 静默失败时保留旧缓存兜底），
// 因此「菜单权限」修改后无需退出重新登录——F5 刷新或重新进入即同步最新菜单与路由。
// 注意：不能在 / 路由上用 redirect 指向 firstPage() —— Vue Router 对 redirect 的求值发生在全局守卫
// 之前，此时 session.menus 尚未加载（登录响应不含菜单），firstPage() 会返回 /login 把用户弹回登录页。
// 因此根路径的“跳首页”统一由本守卫在菜单加载完成后处理。
router.beforeEach(async (to) => {
  if (to.name === 'login' || to.name === 'login-account') return true
  if (!session.token) return { name: 'login' }
  let justRegistered = false
  // 菜单未就绪（首次进入 / 登出重登 / 改密重登）时总是拉取最新菜单树；
  // dynamicRoutesReady 为模块级标志，任何清 session 而未重置它的路径都会导致 menus 为空，
  // 故显式检查 menus 为空也强制重拉（双保险），避免守卫把用户弹回登录页
  if (!dynamicRoutesReady || !session.menus || session.menus.length === 0) {
    await loadMenus()
    registerDynamicRoutes()
    dynamicRoutesReady = true
    justRegistered = true
  }
  // 访问根路径（登录成功 / 手动输入 #/ / 刷新）：跳转到当前用户可见菜单的首个页面
  if (!to.path || to.path === '/') {
    const home = firstPage()
    if (home && home !== '/login') return { path: home, replace: true }
    // 菜单加载失败兜底：回到登录页（loadMenus 内部已有 toast/拦截器提示）
    return { name: 'login' }
  }
  // 动态路由是本次守卫执行期间才注册的：重新导航一次让匹配器命中新路由
  if (justRegistered) {
    return { path: to.path, query: to.query, hash: to.hash, replace: true }
  }
  return true
})

export default router
