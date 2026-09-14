// ============ API 封装 ============
import axios from 'axios'
import { reactive } from 'vue'

// 全局会话状态（登录/登出）
export const session = reactive({
  token: localStorage.getItem('cs_admin_token') || '',
  tenant_id: localStorage.getItem('cs_admin_tenant') || '',
  is_platform_admin: localStorage.getItem('cs_admin_plat') === '1'
})

export function setSession(data) {
  session.token = data.token
  session.tenant_id = data.tenant_id
  session.is_platform_admin = !!data.is_platform_admin
  localStorage.setItem('cs_admin_token', data.token)
  localStorage.setItem('cs_admin_tenant', data.tenant_id)
  localStorage.setItem('cs_admin_plat', data.is_platform_admin ? '1' : '0')
}

export function clearSession() {
  session.token = ''
  session.tenant_id = ''
  session.is_platform_admin = false
  localStorage.removeItem('cs_admin_token')
  localStorage.removeItem('cs_admin_tenant')
  localStorage.removeItem('cs_admin_plat')
}

// 轻量 toast（全局事件）
export function toast(msg, type = 'info') {
  window.dispatchEvent(new CustomEvent('cs-toast', { detail: { msg, type } }))
}

// axios 实例：url 已含 /api 前缀（nginx 反代），baseURL 置空避免双重拼接
const http = axios.create({ baseURL: '', timeout: 30000 })

http.interceptors.request.use(cfg => {
  if (session.token) cfg.headers.Authorization = 'Bearer ' + session.token
  return cfg
})

http.interceptors.response.use(
  resp => {
    const d = resp.data
    if (d && typeof d === 'object' && 'code' in d) return d   // {code,msg,data}
    return { code: 0, msg: 'ok', data: d }
  },
  err => {
    const status = err.response && err.response.status
    const data = err.response && err.response.data
    const msg = (data && data.msg) || '请求失败 (' + (status || '网络错误') + ')'
    if (status === 401) {
      clearSession()
      toast('登录已失效，请重新登录', 'error')
      // 路由守卫会拦截到未登录状态跳转登录页
      if (location.hash && !location.hash.includes('/login')) {
        setTimeout(() => { location.hash = '#/login' }, 300)
      }
    } else {
      toast(msg, 'error')
    }
    return Promise.reject({ code: status || -1, msg })
  }
)

// 便捷方法
export function api(path, { method = 'GET', body = null, params = null, form = null } = {}) {
  const cfg = { method }
  if (params) cfg.params = params
  if (form) {
    // 不手动设置 Content-Type：由浏览器自动生成带 boundary 的 multipart 头，
    // 手动设置会丢失 boundary 导致后端解析失败
    cfg.data = form
  } else if (body !== null) {
    cfg.data = body
  }
  try {
    return http.request({ url: path, ...cfg })
  } catch (e) {
    // 同步抛错（如请求配置非法）时透出，便于排查
    console.error('[api] sync error', path, e)
    return Promise.reject({ code: -2, msg: '请求配置错误: ' + (e && e.message || e) })
  }
}

export default http
