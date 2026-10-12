<template>
  <div class="login-page">
    <div class="login-card">
      <div class="login-brand">
        <span style="font-size:40px;">💬</span>
        <div class="login-title">智能客服控制台</div>
        <div class="login-sub">账号登录 · 租户内成员</div>
      </div>
      <div class="login-form">
        <label class="field">
          <span class="field-label">租户 ID</span>
          <div class="tid-input">
            <input v-model="form.tenant_id" class="input" placeholder="例如 t_demo" @keyup.enter="doLogin" :readonly="!!tidLocked" />
            <span v-if="tidLocked" class="tid-lock" title="租户已由登录链接指定">🔒</span>
          </div>
          <span v-if="tidLocked" class="tid-hint">该登录链接已绑定租户「{{ tidLocked }}」，如需切换请使用 AppKey 登录或修改链接参数</span>
        </label>
        <label class="field">
          <span class="field-label">用户名</span>
          <input v-model="form.username" class="input" placeholder="请输入用户名" @keyup.enter="doLogin" />
        </label>
        <label class="field">
          <span class="field-label">密码</span>
          <input v-model="form.password" type="password" class="input" placeholder="请输入密码" @keyup.enter="doLogin" />
        </label>
        <div class="login-tip">账号由租户管理员在「成员管理」中创建；角色决定可访问的菜单与操作。</div>
        <button class="btn btn-primary btn-block" :disabled="loading" @click="doLogin">
          {{ loading ? '登录中...' : '登 录' }}
        </button>
        <div class="login-switch">
          <router-link to="/login" class="link">使用 AppKey 登录 →</router-link>
        </div>
      </div>
      <div class="login-foot text-3">© 2026 智能客服系统 · 多租户 SaaS 版</div>
    </div>
  </div>
</template>

<script setup>
import { reactive, ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, setSession, toast } from '../api'

const route = useRoute()
const router = useRouter()
const form = reactive({ tenant_id: '', username: '', password: '' })
const loading = ref(false)
const publicKeyPem = ref('')
// 登录链接携带租户（#/login-account?tid=demo_key）：不同租户分发专属登录地址，免手输租户 ID
const tidLocked = ref('')

onMounted(async () => {
  // 1) 优先取登录链接中的租户（URL 参数）→ 2) 上次登录租户记忆（localStorage）
  const q = (route.query && route.query.tid) || ''
  const remembered = localStorage.getItem('cs_login_tid') || ''
  const initTid = String(q || remembered || '')
  form.tenant_id = initTid
  if (q) tidLocked.value = String(q)
  try {
    const r = await api('/api/auth/public-key', { silent: true })
    if (r.code === 0 && r.data && r.data.public_key) {
      publicKeyPem.value = r.data.public_key
    }
  } catch (e) { /* 取不到公钥则明文降级 */ }
})

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

async function doLogin() {
  if (!form.tenant_id || !form.username || !form.password) return toast('请填写租户 ID、用户名和密码', 'warning')
  loading.value = true
  try {
    let payload = { tenant_id: form.tenant_id, username: form.username, password: form.password, encrypted: false }
    if (publicKeyPem.value && window.crypto && crypto.subtle) {
      try {
        payload.password = await encryptSecret(form.password, publicKeyPem.value)
        payload.encrypted = true
      } catch (e) {
        console.warn('[login-account] RSA 加密失败，降级明文', e)
        payload = { tenant_id: form.tenant_id, username: form.username, password: form.password, encrypted: false }
      }
    }
    const r = await api('/api/auth/user-token', { method: 'POST', body: payload })
    if (r.code !== 0) return
    // 记住本机最近登录租户，下次进入账号登录页自动填充
    try { localStorage.setItem('cs_login_tid', form.tenant_id) } catch (e) { }
    setSession(r.data)
    toast('登录成功，角色: ' + (r.data.role || '成员'), 'success')
    // 默认首页按角色分流：管理员 → 运营看板；客服 → 工单列表
    router.push('/')
  } catch (e) {
    // api.js 已 toast
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-page {
  height: 100%; display: flex; align-items: center; justify-content: center;
  background: linear-gradient(135deg, var(--c-primary-weak) 0%, var(--c-bg) 55%);
}
.login-card {
  width: 380px; background: var(--c-card); border-radius: 16px;
  border: 1px solid var(--c-border); box-shadow: 0 24px 64px var(--c-shadow);
  padding: 36px 34px 26px; text-align: center;
}
.login-brand { margin-bottom: 26px; }
.login-title { font-size: 20px; font-weight: 600; margin-top: 10px; }
.login-sub { font-size: 12px; color: var(--c-text-3); margin-top: 4px; }
.login-form { text-align: left; display: flex; flex-direction: column; gap: 14px; }
.field { display: flex; flex-direction: column; gap: 6px; }
.field-label { font-size: 12px; color: var(--c-text-2); }
.tid-input { position: relative; }
.tid-input .input[readonly] { background: var(--c-bg); color: var(--c-text-2); cursor: not-allowed; }
.tid-lock { position: absolute; right: 10px; top: 50%; transform: translateY(-50%); font-size: 12px; opacity: .7; }
.tid-hint { font-size: 11px; color: var(--c-warning, #d97706); line-height: 1.5; }
.login-tip { font-size: 11px; color: var(--c-text-3); line-height: 1.5; background: var(--c-bg); padding: 8px 10px; border-radius: 6px; }
.login-switch { text-align: center; margin-top: 10px; font-size: 12px; }
.login-switch .link { color: var(--c-primary, #3b82f6); text-decoration: none; }
.login-switch .link:hover { text-decoration: underline; }
.login-foot { margin-top: 22px; font-size: 11px; }
</style>
