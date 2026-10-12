<template>
  <div class="cd-mask" @click.self="$emit('close')">
    <div class="cd-box card">
      <div class="cd-head">
        <b>清理归档</b>
        <button class="btn btn-sm" @click="$emit('close')">关闭</button>
      </div>

      <!-- 存储概览 -->
      <div class="cd-stats">
        <div class="cd-stat"><span class="cd-k">活跃会话</span><b>{{ stats.sessions ?? '-' }}</b></div>
        <div class="cd-stat"><span class="cd-k">消息总数</span><b>{{ stats.total_messages ?? '-' }}</b></div>
        <div class="cd-stat"><span class="cd-k">保留天数</span><b>{{ stats.retention_days ?? '-' }}</b></div>
        <div class="cd-stat"><span class="cd-k">待清理会话</span><b>{{ cleanupCount }}</b></div>
      </div>

      <div class="cd-note text-3">超过保留天数未活跃的会话由后台任务自动回收，这里可立即执行并查看归档情况。</div>

      <!-- 操作区 -->
      <div class="cd-op">
        <div class="cd-op-label">清理过期会话</div>
        <div class="cd-op-ctrl">
          <span class="cd-hint">未活跃超过</span>
          <input v-model.number="cleanupDays" class="input" style="width:60px;height:26px;" min="1" @change="loadCleanupCount" />
          <span class="cd-hint">天</span>
          <button class="btn btn-sm btn-primary" :disabled="busy" @click="cleanup">立即清理</button>
          <span class="cd-hint">预计清理 <b>{{ cleanupCount }}</b> 个会话</span>
        </div>
      </div>

      <div class="cd-op">
        <div class="cd-op-label">归档旧消息</div>
        <div class="cd-op-ctrl">
          <span class="cd-hint">归档</span>
          <input v-model.number="archiveDays" class="input" style="width:60px;height:26px;" min="1" @change="loadArchiveCount" />
          <span class="cd-hint">天前的消息</span>
          <button class="btn btn-sm btn-primary" :disabled="busy" @click="archive">立即归档</button>
          <span class="cd-hint">可归档 <b>{{ archiveCount }}</b> 条</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api, toast } from '../api'

const emit = defineEmits(['close', 'done'])

const stats = ref({})
const cleanupDays = ref(30)
const archiveDays = ref(90)
const cleanupCount = ref(0)
const archiveCount = ref(0)
const busy = ref(false)

async function load() {
  try {
    const r = await api('/api/admin/session/stats')
    if (r.code === 0) {
      stats.value = r.data || {}
      if (r.data?.retention_days) cleanupDays.value = r.data.retention_days
      cleanupCount.value = r.data?.expired_sessions ?? 0
    }
  } catch (e) {}
}
async function loadCleanupCount() {
  try {
    const r = await api('/api/admin/session/cleanup-count', { params: { days: cleanupDays.value || 30 } })
    if (r.code === 0) cleanupCount.value = r.data?.count ?? 0
  } catch (e) {}
}
async function loadArchiveCount() {
  try {
    const r = await api('/api/admin/messages/archive-count', { params: { days: archiveDays.value || 90 } })
    if (r.code === 0) archiveCount.value = r.data?.count ?? 0
  } catch (e) {}
}

async function cleanup() {
  busy.value = true
  try {
    const r = await api('/api/admin/session/cleanup', { method: 'POST', body: { days: cleanupDays.value || 30 } })
    toast(r.msg || (r.code === 0 ? '清理完成' : '清理失败'), r.code === 0 ? 'success' : 'error')
    await load()
    emit('done')
  } catch (e) {}
  finally { busy.value = false }
}

async function archive() {
  busy.value = true
  try {
    const r = await api('/api/admin/messages/archive', { method: 'POST', params: { days: archiveDays.value || 90 }, body: {} })
    toast(r.msg || (r.code === 0 ? '归档完成' : '归档失败'), r.code === 0 ? 'success' : 'error')
    await loadArchiveCount()
    emit('done')
  } catch (e) {}
  finally { busy.value = false }
}

onMounted(() => { load(); loadArchiveCount() })
</script>

<style scoped>
.cd-mask { position: fixed; inset: 0; background: rgba(0,0,0,.4); display: flex; align-items: center; justify-content: center; z-index: 100; }
.cd-box { width: 560px; max-width: 92vw; padding: 16px; }
.cd-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; }
.cd-stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(110px, 1fr)); gap: 10px; margin-bottom: 10px; }
.cd-stat { padding: 10px 12px; background: var(--c-bg); border-radius: var(--radius); }
.cd-stat b { display: block; font-size: 18px; color: var(--c-primary); margin-top: 4px; }
.cd-k { font-size: 12px; color: var(--c-text-2); }
.cd-note { font-size: 12px; margin-bottom: 12px; }
.cd-op { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; padding: 10px 12px; background: var(--c-bg); border-radius: var(--radius); margin-bottom: 10px; }
.cd-op-label { font-size: 13px; font-weight: 600; min-width: 96px; }
.cd-op-ctrl { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.cd-hint { font-size: 12px; color: var(--c-text-2); }
.cd-hint b { color: var(--c-primary); }
</style>
