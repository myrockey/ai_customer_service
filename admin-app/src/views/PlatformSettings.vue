<template>
  <div>
    <div class="notice-bar">
      ⚙️ 平台级动态配置，对所有租户生效。修改保存后即时生效，无需重启服务。
    </div>

    <div class="card">
      <h3>知识库上传大小限制</h3>
      <div class="form-row">
        <label>kb_max_file_mb</label>
        <input v-model.number="kbMaxFileMb" type="number" min="1" max="100" />
        <span class="unit">MB（范围 1-100）</span>
      </div>
      <button class="btn primary" :disabled="saving" @click="save">
        {{ saving ? '保存中...' : '保存' }}
      </button>
    </div>

    <div class="card" style="margin-top:16px;">
      <h3>客户信息提取规则（会话中自动提取联系电话 / 邮箱）</h3>
      <div class="form-row" v-for="r in rules" :key="r.field">
        <label>{{ fieldLabel(r.field) }}</label>
        <input v-model="r.pattern" class="pattern-input" :placeholder="defaultPlaceholder(r.field)" />
      </div>
      <div class="unit" style="margin-bottom:12px;">留空 = 不提取该字段；正则在所有租户的用户消息上生效，保存后新消息自动按新规则提取（历史会话可在「会话管理」中手动重新提取）。</div>
      <button class="btn primary" :disabled="rulesSaving" @click="saveRules">
        {{ rulesSaving ? '保存中...' : '保存规则' }}
      </button>
      <span v-if="rulesMsg" class="unit" style="margin-left:10px;">{{ rulesMsg }}</span>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { api, toast } from '../api'

const settings = ref({})
const kbMaxFileMb = ref(10)
const saving = ref(false)

const DEFAULT_PATTERNS = {
  phone: '(?:\\+?86[\\s-]?)?1[3-9]\\d[\\s-]?\\d{4}[\\s-]?\\d{4}',
  email: '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Za-z]{2,}'
}
const rules = ref([
  { field: 'phone', pattern: '' },
  { field: 'email', pattern: '' }
])
const rulesSaving = ref(false)
const rulesMsg = ref('')

const fieldLabel = f => ({ phone: '电话 phone', email: '邮箱 email' }[f] || f)
const defaultPlaceholder = f => DEFAULT_PATTERNS[f] || ''

async function load() {
  try {
    const r = await api('/api/admin/platform-settings')
    if (r.code === 0) {
      const map = {}
      for (const it of (r.data || [])) map[it.key] = it.value
      settings.value = map
      if (map.kb_max_file_mb) kbMaxFileMb.value = Number(map.kb_max_file_mb)
    }
  } catch (e) {
  }
  try {
    const rr = await api('/api/admin/extract-rules')
    if (rr.code === 0) {
      const list = rr.data || []
      for (const r of rules.value) {
        const hit = list.find(x => x.field === r.field)
        r.pattern = hit ? (hit.pattern || '') : ''
      }
    }
  } catch (e) {}
}

async function save() {
  if (!kbMaxFileMb.value || kbMaxFileMb.value < 1 || kbMaxFileMb.value > 100) {
    toast('上传大小限制需为 1-100 的整数（MB）', 'error')
    return
  }
  saving.value = true
  try {
    const r = await api('/api/admin/platform-settings', {
      method: 'PUT',
      body: { key: 'kb_max_file_mb', value: String(kbMaxFileMb.value) }
    })
    if (r.code === 0) {
      toast('保存成功')
      await load()
    } else {
      toast(r.msg || '保存失败', 'error')
    }
  } catch (e) {
  } finally {
    saving.value = false
  }
}

async function saveRules() {
  // 校验：正则合法且至少留一个字段
  const payload = rules.value.filter(r => r.pattern && r.pattern.trim())
  if (!payload.length) {
    toast('至少保留一个字段的正则，否则提取不会生效', 'error')
    return
  }
  for (const r of payload) {
    try { new RegExp(r.pattern) } catch (e) { toast('正则非法: ' + r.field, 'error'); return }
  }
  rulesSaving.value = true
  rulesMsg.value = ''
  try {
    const r = await api('/api/admin/extract-rules', { method: 'PUT', body: payload })
    if (r.code === 0) {
      toast('提取规则已保存')
      rulesMsg.value = '✓ 已生效（历史会话可在会话管理手动重新提取）'
    } else {
      toast(r.msg || '保存规则失败', 'error')
    }
  } catch (e) {
  } finally {
    rulesSaving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.notice-bar {
  background: var(--c-primary-weak); color: var(--c-primary); border: 1px solid var(--c-border);
  border-radius: var(--radius); padding: 10px 14px; font-size: 13px; margin-bottom: 16px;
}
.card {
  background: var(--c-bg-2); border: 1px solid var(--c-border); border-radius: var(--radius);
  padding: 20px; max-width: 560px;
}
.card h3 { margin: 0 0 16px; font-size: 15px; }
.form-row {
  display: flex; align-items: center; gap: 10px; margin-bottom: 16px; flex-wrap: wrap;
}
.form-row label { font-size: 13px; color: var(--c-text-2); min-width: 120px; }
.form-row input {
  width: 120px; padding: 8px 10px; border: 1px solid var(--c-border); border-radius: var(--radius);
  background: var(--c-bg-1); color: var(--c-text-1); font-size: 14px;
}
.form-row .pattern-input { width: 320px; font-family: monospace; font-size: 13px; }
.unit { font-size: 12px; color: var(--c-text-2); }
.btn.primary {
  background: var(--c-primary); color: #fff; border: none; border-radius: var(--radius);
  padding: 9px 22px; font-size: 14px; cursor: pointer;
}
.btn.primary:disabled { opacity: .6; cursor: not-allowed; }
</style>
