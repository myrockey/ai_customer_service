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
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { api, toast } from '../api'

const settings = ref({})
const kbMaxFileMb = ref(10)
const saving = ref(false)

async function load() {
  try {
    const r = await api('/api/admin/platform-settings')
    if (r.code === 0) {
      const map = {}
      for (const it of (r.data || [])) map[it.key] = it.value
      settings.value = map
      if (map.kb_max_file_mb) kbMaxFileMb.value = Number(map.kb_max_file_mb)
    } else {
      toast(r.msg || '加载平台设置失败', 'error')
    }
  } catch (e) {
    toast('加载平台设置失败: ' + (e && e.message || e), 'error')
  }
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
    toast('保存失败: ' + (e && e.message || e), 'error')
  } finally {
    saving.value = false
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
.unit { font-size: 12px; color: var(--c-text-2); }
.btn.primary {
  background: var(--c-primary); color: #fff; border: none; border-radius: var(--radius);
  padding: 9px 22px; font-size: 14px; cursor: pointer;
}
.btn.primary:disabled { opacity: .6; cursor: not-allowed; }
</style>
