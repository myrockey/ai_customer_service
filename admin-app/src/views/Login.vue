<template>
  <div class="login-page">
    <div class="login-card">
      <div class="login-brand">
        <span style="font-size:40px;">💬</span>
        <div class="login-title">智能客服控制台</div>
        <div class="login-sub">多租户智能客服系统 · 管理后台</div>
      </div>
      <div class="login-form">
        <label class="field">
          <span class="field-label">app_key</span>
          <input v-model="form.app_key" class="input" placeholder="请输入 app_key" @keyup.enter="doLogin" />
        </label>
        <label class="field">
          <span class="field-label">app_secret</span>
          <input v-model="form.app_secret" type="password" class="input" placeholder="请输入 app_secret" @keyup.enter="doLogin" />
        </label>
        <div class="login-tip">演示账号：平台管理员 admin_key / admin_secret · 租户 demo_key / demo_secret</div>
        <button class="btn btn-primary btn-block" :disabled="loading" @click="doLogin">
          {{ loading ? '登录中...' : '登 录' }}
        </button>
      </div>
      <div class="login-foot text-3">© 2026 智能客服系统 · 多租户 SaaS 版</div>
    </div>
  </div>
</template>

<script setup>
import { reactive, ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api, setSession, toast } from '../api'

const router = useRouter()
const form = reactive({ app_key: '', app_secret: '' })
const loading = ref(false)
// RSA 公钥（登录时加密 app_secret 防明文传输；未取到则降级明文）
const publicKeyPem = ref('')

onMounted(async () => {
  try {
    const r = await api('/api/auth/public-key')
    if (r.code === 0 && r.data && r.data.public_key) {
      publicKeyPem.value = r.data.public_key
    }
  } catch (e) { /* 取不到公钥则明文降级，不影响登录 */ }
})

// 用 Web Crypto（RSA-OAEP/SHA-256）加密 app_secret，返回 base64 密文
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
  if (!form.app_key || !form.app_secret) return toast('请输入 app_key 和 app_secret', 'warning')
  loading.value = true
  try {
    let payload = { app_key: form.app_key, app_secret: form.app_secret, encrypted: false }
    if (publicKeyPem.value && window.crypto && crypto.subtle) {
      try {
        payload.app_secret = await encryptSecret(form.app_secret, publicKeyPem.value)
        payload.encrypted = true
      } catch (e) {
        console.warn('[login] RSA 加密失败，降级明文', e)
        payload = { app_key: form.app_key, app_secret: form.app_secret, encrypted: false }
      }
    }
    const r = await api('/api/auth/token', { method: 'POST', body: payload })
    if (r.code !== 0) return toast(r.msg || '登录失败', 'error')
    setSession(r.data)
    toast('登录成功，当前租户: ' + r.data.tenant_id, 'success')
    router.push(r.data.is_platform_admin ? '/tenants' : '/tickets')
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
.login-tip { font-size: 11px; color: var(--c-text-3); line-height: 1.5; background: var(--c-bg); padding: 8px 10px; border-radius: 6px; }
.login-foot { margin-top: 22px; font-size: 11px; }
</style>
