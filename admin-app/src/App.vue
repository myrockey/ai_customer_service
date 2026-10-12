<template>
  <div class="layout">
    <Toast />

    <!-- 登录页（无布局） -->
    <router-view v-if="isLogin" />

    <!-- 主布局 -->
    <template v-else>
      <header class="header">
        <div class="brand">
          <span class="logo">💬</span>
          <div class="brand-text">
            <div class="brand-name">智能客服控制台</div>
            <div class="brand-sub">CS Admin Console</div>
          </div>
        </div>
        <div class="header-right">
          <!-- 实时推送通道（仅租户管理员） -->
          <template v-if="!session.is_platform_admin">
            <span class="text-3" style="font-size:12px;">实时推送通道</span>
            <button v-if="!wsOn" class="btn btn-sm" @click="connect">开启推送</button>
            <button v-else class="btn btn-sm btn-primary" @click="disconnect">停止推送</button>
            <span class="badge" :class="wsOn ? 'badge-green' : 'badge-gray'">{{ wsState }}</span>
          </template>

          <ThemeSwitcher />
          <span class="badge" :class="session.is_platform_admin ? 'badge-blue' : 'badge-green'">
            {{ session.nickname || '成员' }} · {{ session.role_name || '客服' }} · {{ session.tenant_name || session.tenant_id }}
          </span>
          <a class="link" @click="goDocs">接入文档 ↗</a>
          <button v-if="!session.user_id" class="btn btn-sm" @click="openPwdModal">修改密码</button>
          <button class="btn btn-sm" @click="logout">登出</button>
        </div>
      </header>

      <div class="body">
        <aside class="sidebar">
          <template v-for="(group, gi) in menuGroups" :key="gi">
            <div v-if="group.items.length" class="menu-group">
              <div class="menu-group-title">{{ group.title }}</div>
              <template v-for="item in group.items" :key="item.path || item.title">
                <!-- 带子菜单的可展开父项 -->
                <div v-if="item.children" class="menu-sub">
                  <div
                    class="menu-item menu-parent" :class="{ open: expanded.includes(item.id) }"
                    @click="toggleExpand(item.id)"
                  >
                    <span class="menu-dot"></span>{{ item.title }}
                    <svg class="arrow" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 6l6 6-6 6"/></svg>
                  </div>
                  <div v-show="expanded.includes(item.id)" class="menu-sub-items">
                    <router-link
                      v-for="ch in item.children" :key="ch.path"
                      :to="ch.path" class="menu-item menu-child"
                      active-class="active"
                    >
                      <span class="menu-dot"></span>{{ ch.title }}
                    </router-link>
                  </div>
                </div>
                <!-- 普通菜单项 -->
                <router-link
                  v-else :to="item.path" class="menu-item"
                  active-class="active"
                >
                  <span class="menu-dot"></span>{{ item.title }}
                </router-link>
              </template>
            </div>
          </template>
        </aside>

        <main class="main">
          <router-view v-slot="{ Component }">
            <component :is="Component" />
          </router-view>
        </main>
      </div>
    </template>

    <!-- 修改密码弹窗 -->
    <Modal :visible="pwdVisible" title="修改密码" @close="pwdVisible = false">
      <div class="pwd-form">
        <div class="form-row">
          <label>旧密码（app_secret）</label>
          <input v-model="pwdForm.old_secret" type="password" placeholder="请输入当前登录密码" autocomplete="current-password" />
        </div>
        <div class="form-row">
          <label>新密码</label>
          <input v-model="pwdForm.new_secret" type="password" placeholder="至少 8 位" autocomplete="new-password" />
        </div>
        <div class="form-row">
          <label>确认新密码</label>
          <input v-model="pwdForm.confirm_secret" type="password" placeholder="再次输入新密码" autocomplete="new-password" />
        </div>
        <div class="form-tip">修改成功后旧登录状态将失效，需使用新密码重新登录。</div>
      </div>
      <template #footer>
        <button class="btn btn-sm" @click="pwdVisible = false">取消</button>
        <button class="btn btn-sm btn-primary" :disabled="pwdLoading" @click="submitPwd">
          {{ pwdLoading ? '提交中...' : '确认修改' }}
        </button>
      </template>
    </Modal>
  </div>
</template>

<script setup>
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { session, clearSession, toast, api } from './api'
import { resetDynamicRoutes } from './router'
import Toast from './components/Toast.vue'
import ThemeSwitcher from './components/ThemeSwitcher.vue'
import Modal from './components/Modal.vue'

const route = useRoute()
const router = useRouter()

const isLogin = computed(() => route.name === 'login' || route.name === 'login-account')

// 菜单：DB 菜单树驱动（session.menus，登录后由路由守卫拉取 /api/admin/menus/tree）
// 结构：分组 → 功能域目录(type=1，可展开，子页面为链接) → 页面(type=2，直接链接)；type=3 按钮仅作权限不渲染
const menuGroups = computed(() => {
  const groups = []
  const map = new Map()
  for (const root of session.menus || []) {
    for (const node of root.children || []) {
      if (!node || node.type === 3) continue
      const g = node.group_name || '菜单'
      if (!map.has(g)) { const arr = []; map.set(g, arr); groups.push({ title: g, items: arr }) }
      if (node.type === 1) {
        const kids = (node.children || []).filter(c => c && c.type === 2)
        if (kids.length) map.get(g).push({ id: node.id, title: node.name, children: kids.map(k => ({ path: k.path, title: k.name })) })
      } else if (node.type === 2 && node.path) {
        map.get(g).push({ path: node.path, title: node.name })
      }
    }
  }
  return groups
})

// 子菜单展开状态：支持多个目录同时展开（用菜单 id 定位，避免不同分组同名目录冲突）
const expanded = ref([])
function toggleExpand(id) {
  const i = expanded.value.indexOf(id)
  if (i >= 0) expanded.value = expanded.value.filter(x => x !== id)
  else expanded.value = [...expanded.value, id]
}
// 菜单为异步加载（守卫拉取后写入 session.menus），用 watch 在菜单就绪后自动展开当前路由所在目录
watch(menuGroups, (gs) => {
  if (!gs.length || expanded.value.length) return
  const list = []
  for (const g of gs) {
    const cur = g.items.find(m => m.children && m.children.some(c => c.path === route.path))
    if (cur) list.push(cur.id)
  }
  if (list.length) expanded.value = list
}, { immediate: true })

// ============ 实时推送通道（/ws/admin） ============
const wsOn = ref(false)
const wsState = ref('未连接')
let ws = null
let reconnectTimer = null
let heartbeatTimer = null
// 手动停止标志：用户点"停止推送"后不再自动重连（否则关闭后 5s 又自动连上，按钮形同虚设）
let manualStop = false

function wsUrl() {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  const q = session.token ? ('?token=' + encodeURIComponent(session.token)) : ''
  return `${proto}://${location.host}/ws/admin${q}`
}

function connect() {
  if (wsOn.value) return
  manualStop = false
  try {
    ws = new WebSocket(wsUrl())
    ws.onopen = () => {
      wsOn.value = true
      wsState.value = '推送中'
      toast('实时推送已开启', 'success')
      refreshAfterConnect()
      // 心跳保活：每 25s 发 ping（服务端 60s 无消息判离线，用于客服在线状态）
      if (heartbeatTimer) clearInterval(heartbeatTimer)
      heartbeatTimer = setInterval(() => { try { ws.send('ping') } catch (e) {} }, 25000)
    }
    ws.onclose = () => {
      wsOn.value = false
      if (heartbeatTimer) { clearInterval(heartbeatTimer); heartbeatTimer = null }
      // 手动停止时保留"已停止"状态且不自动重连；异常断开才显示"未连接"并重连
      if (!manualStop) {
        wsState.value = '未连接'
        scheduleReconnect()
      }
    }
    ws.onerror = () => { try { ws.close() } catch (e) {} }
    ws.onmessage = (ev) => { handleWsMsg(ev.data) }
  } catch (e) {
    toast('推送通道连接失败: ' + e.message, 'error')
  }
}

function disconnect() {
  manualStop = true
  if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null }
  if (ws) { try { ws.close() } catch (e) {} }
  ws = null
  wsOn.value = false
  wsState.value = '已停止'
}

function scheduleReconnect() {
  if (reconnectTimer) clearTimeout(reconnectTimer)
  reconnectTimer = setTimeout(connect, 5000)
}

// 推送事件：各页面组件通过自定义事件刷新
function handleWsMsg(raw) {
  try {
    const msg = JSON.parse(raw)
    window.dispatchEvent(new CustomEvent('cs-ws-msg', { detail: msg }))
  } catch (e) { /* 忽略非 JSON */ }
}

// 连接后拉一次最新数据（旧版行为：connectAdmin → refreshAfterConnect）
function refreshAfterConnect() {
  window.dispatchEvent(new CustomEvent('cs-ws-connected'))
}

// ============ 登出 / 文档 ============
function logout() {
  resetDynamicRoutes()
  clearSession()
  router.push('/login')
}
function goDocs() {
  window.open('/sdk/', '_blank')
}

// ============ 修改密码（平台管理员 / 租户管理员通用） ============
const pwdVisible = ref(false)
const pwdLoading = ref(false)
const publicKeyPem = ref('')
const pwdForm = ref({ old_secret: '', new_secret: '', confirm_secret: '' })

// 取 RSA 公钥（与登录一致；取不到则明文降级）
async function loadPubKey() {
  try {
    const r = await api('/api/auth/public-key')
    if (r.code === 0 && r.data && r.data.public_key) publicKeyPem.value = r.data.public_key
  } catch (e) { /* 降级明文 */ }
}

// Web Crypto RSA-OAEP/SHA-256 加密（与 Login.vue 一致）
async function encryptSecret(secret, pem) {
  const body = pem.replace(/-----BEGIN PUBLIC KEY-----/, '').replace(/-----END PUBLIC KEY-----/, '').replace(/\s+/g, '')
  const der = Uint8Array.from(atob(body), c => c.charCodeAt(0))
  const key = await crypto.subtle.importKey('spki', der, { name: 'RSA-OAEP', hash: 'SHA-256' }, false, ['encrypt'])
  const enc = await crypto.subtle.encrypt({ name: 'RSA-OAEP' }, key, new TextEncoder().encode(secret))
  const bytes = new Uint8Array(enc)
  let bin = ''
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i])
  return btoa(bin)
}

function openPwdModal() {
  pwdForm.value = { old_secret: '', new_secret: '', confirm_secret: '' }
  pwdVisible.value = true
  if (!publicKeyPem.value) loadPubKey()
}

async function submitPwd() {
  const f = pwdForm.value
  if (!f.old_secret || !f.new_secret) return toast('请填写旧密码和新密码', 'warning')
  if (f.new_secret.length < 8) return toast('新密码长度至少 8 位', 'warning')
  if (f.new_secret !== f.confirm_secret) return toast('两次输入的新密码不一致', 'warning')
  pwdLoading.value = true
  try {
    let payload = { old_app_secret: f.old_secret, new_app_secret: f.new_secret, encrypted: false }
    if (publicKeyPem.value && window.crypto && crypto.subtle) {
      try {
        payload.old_app_secret = await encryptSecret(f.old_secret, publicKeyPem.value)
        payload.new_app_secret = await encryptSecret(f.new_secret, publicKeyPem.value)
        payload.encrypted = true
      } catch (e) {
        console.warn('[pwd] RSA 加密失败，降级明文', e)
        payload = { old_app_secret: f.old_secret, new_app_secret: f.new_secret, encrypted: false }
      }
    }
    const r = await api('/api/auth/password', { method: 'PUT', body: payload })
    if (r.code === 0) {
      toast('密码修改成功，请重新登录', 'success')
      pwdVisible.value = false
      // 重置动态路由状态，确保重新登录时重新拉取菜单（与登出 logout() 一致，否则 dynamicRoutesReady 残留导致重登后菜单为空被弹回登录页）
      setTimeout(() => { resetDynamicRoutes(); clearSession(); router.push('/login') }, 600)
    } else {
      toast(r.msg || '修改失败', 'error')
    }
  } catch (e) {
    toast('修改失败: ' + (e.message || e), 'error')
  } finally {
    pwdLoading.value = false
  }
}

// 登录/登出自动联动推送通道：
// 登录成功（session.token 从空变有）→ 租户管理员自动连接；登出（token 清空）→ 断开
watch(
  () => session.token,
  () => {
    if (session.token && !session.is_platform_admin) {
      connect()
    } else if (!session.token) {
      disconnect()
    }
  },
  { immediate: true }
)
onUnmounted(() => disconnect())
</script>

<style scoped>
.layout { height: 100%; display: flex; flex-direction: column; }
.header {
  height: 52px; background: var(--c-header-bg); border-bottom: 1px solid var(--c-border);
  display: flex; align-items: center; justify-content: space-between; padding: 0 18px;
  position: relative; z-index: 10;
}
.brand { display: flex; align-items: center; gap: 10px; }
.logo { font-size: 24px; }
.brand-name { font-size: 15px; font-weight: 600; line-height: 1.2; }
.brand-sub { font-size: 10px; color: var(--c-text-3); letter-spacing: .5px; }
.header-right { display: flex; align-items: center; gap: 12px; }
.body { flex: 1; display: flex; min-height: 0; }
.sidebar {
  width: 190px; background: var(--c-sidebar-bg); border-right: 1px solid var(--c-border);
  padding: 14px 10px; overflow-y: auto; flex-shrink: 0;
}
.menu-group { margin-bottom: 18px; }
.menu-group-title { font-size: 11px; color: var(--c-text-3); padding: 0 10px; margin-bottom: 6px; }
.menu-item {
  display: flex; align-items: center; gap: 8px; padding: 9px 10px; border-radius: var(--radius);
  color: var(--c-sidebar-text); font-size: 13px; text-decoration: none; transition: all .12s;
}
.menu-item:hover { background: var(--c-hover); }
.menu-item.active { background: var(--c-sidebar-active); color: var(--c-sidebar-active-text); font-weight: 500; }
.menu-dot { width: 6px; height: 6px; border-radius: 50%; background: currentColor; opacity: .4; }
.menu-item.active .menu-dot { opacity: 1; }
.menu-parent { cursor: pointer; user-select: none; }
.menu-parent .arrow { width: 13px; height: 13px; margin-left: auto; opacity: .5; transition: transform .15s; }
.menu-parent.open .arrow { transform: rotate(90deg); }
.menu-sub-items { padding-left: 10px; }
.menu-child { padding-top: 7px; padding-bottom: 7px; font-size: 12.5px; }
.main { flex: 1; overflow-y: auto; padding: 20px; min-width: 0; }

/* 修改密码表单 */
.pwd-form { display: flex; flex-direction: column; gap: 14px; }
.form-row { display: flex; flex-direction: column; gap: 6px; }
.form-row label { font-size: 13px; color: var(--c-text-2); }
.form-row input {
  padding: 9px 12px; border: 1px solid var(--c-border); border-radius: var(--radius);
  background: var(--c-input-bg, var(--c-card)); color: var(--c-text); font-size: 14px; outline: none;
}
.form-row input:focus { border-color: var(--c-primary, #3b82f6); }
.form-tip { font-size: 12px; color: var(--c-text-3); background: var(--c-hover); padding: 8px 10px; border-radius: var(--radius); }
</style>
