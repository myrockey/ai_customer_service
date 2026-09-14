import { createRouter, createWebHashHistory } from 'vue-router'
import { session } from './api'

// 平台管理员菜单（系统管理）
const platformRoutes = [
  { path: '/tenants', name: 'tenants', meta: { title: '租户管理', group: '系统管理' }, component: () => import('./views/Tenants.vue') },
  { path: '/model-providers', name: 'model-providers', meta: { title: '模型提供商', group: '系统管理' }, component: () => import('./views/ModelProviders.vue') },
  { path: '/platform-model', name: 'platform-model', meta: { title: '平台模型配置', group: '系统管理' }, component: () => import('./views/PlatformModelConfig.vue') },
  { path: '/platform-settings', name: 'platform-settings', meta: { title: '平台设置', group: '系统管理' }, component: () => import('./views/PlatformSettings.vue') }
]

// 租户管理员菜单（业务中心）
const tenantRoutes = [
  { path: '/tickets', name: 'tickets', meta: { title: '工单管理', group: '业务中心' }, component: () => import('./views/Tickets.vue') },
  { path: '/prompts', name: 'prompts', meta: { title: 'Prompt 管理', group: '业务中心' }, component: () => import('./views/Prompt.vue') },
  { path: '/knowledge', name: 'knowledge', meta: { title: '知识库', group: '业务中心' }, component: () => import('./views/Knowledge.vue') },
  { path: '/sessions', name: 'sessions', meta: { title: '会话管理', group: '业务中心' }, component: () => import('./views/Sessions.vue') },
  { path: '/model-config', name: 'model-config', meta: { title: '模型配置', group: '业务中心' }, component: () => import('./views/TenantModelConfig.vue') },
  { path: '/token-usage', name: 'token-usage', meta: { title: '用量统计', group: '业务中心' }, component: () => import('./views/TokenUsage.vue') },
  { path: '/audit-logs', name: 'audit-logs', meta: { title: '审计日志', group: '业务中心' }, component: () => import('./views/AuditLogs.vue') }
]

const router = createRouter({
  history: createWebHashHistory('/admin/'),
  routes: [
    { path: '/login', name: 'login', meta: { title: '登录' }, component: () => import('./views/Login.vue') },
    { path: '/', redirect: () => (session.is_platform_admin ? '/tenants' : '/tickets') },
    ...platformRoutes,
    ...tenantRoutes,
    { path: '/:pathMatch(.*)*', redirect: '/' }
  ]
})

// 登录守卫
router.beforeEach((to) => {
  if (to.name === 'login') return true
  if (!session.token) return { name: 'login' }
  return true
})

export default router
