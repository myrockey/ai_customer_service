<template>
  <div>
    <!-- 统计卡片 -->
    <div class="stat-grid">
      <div class="card stat" v-for="s in stats" :key="s.label" :title="s.desc">
        <div class="stat-top">
          <span class="stat-icon" v-html="s.icon"></span>
          <span class="stat-label text-3">{{ s.label }}</span>
        </div>
        <div class="stat-value">{{ s.value }}</div>
        <div class="stat-desc">{{ s.desc }}</div>
      </div>
    </div>

    <div class="card" style="margin-top:16px;">
      <div class="head-row">
        <span style="font-weight:600;">会话数据</span>
      </div>

      <!-- 操作区 -->
      <div class="op-grid">
        <div class="op-item">
          <div class="op-label">清理过期会话</div>
          <div class="op-controls">
            <div class="op-field">
              <span class="op-hint">未活跃超过</span>
              <input v-model.number="cleanupDays" class="input" style="width:64px;" min="1" @change="loadCleanupCount" />
              <span class="op-hint">天的会话</span>
            </div>
            <button class="btn btn-sm btn-op" @click="cleanup">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2m3 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6h14z"/></svg>
              清理过期会话
            </button>
            <span class="op-count">预计清理 <b>{{ cleanupCount }}</b> 个会话</span>
          </div>
        </div>
        <div class="op-item">
          <div class="op-label">归档旧消息</div>
          <div class="op-controls">
            <div class="op-field">
              <span class="op-hint">归档</span>
              <input v-model.number="archiveDays" class="input" style="width:64px;" min="1" @change="loadArchiveCount" />
              <span class="op-hint">天前的消息</span>
            </div>
            <button class="btn btn-sm btn-op" @click="archive">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 8v13H3V8M1 3h22v5H1zM10 12h4"/></svg>
              归档旧消息
            </button>
            <span class="op-count">可归档 <b>{{ archiveCount }}</b> 条</span>
          </div>
        </div>
      </div>

      <div class="kv-grid">
        <div class="kv"><span class="kv-k">活跃会话数</span><span class="kv-v">{{ stats0.sessions ?? '-' }}</span></div>
        <div class="kv"><span class="kv-k">消息总数</span><span class="kv-v">{{ stats0.total_messages ?? '-' }}</span></div>
        <div class="kv"><span class="kv-k">最早活跃</span><span class="kv-v">{{ fmt(stats0.oldest_active) }}</span></div>
        <div class="kv"><span class="kv-k">最新活跃</span><span class="kv-v">{{ fmt(stats0.newest_active) }}</span></div>
        <div class="kv"><span class="kv-k">Checkpoint 行数</span><span class="kv-v">{{ stats0.checkpoint_rows ?? '-' }}</span></div>
        <div class="kv"><span class="kv-k">保留天数</span><span class="kv-v">{{ stats0.retention_days ?? '-' }}</span></div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { api, toast } from '../api'

const stats0 = ref({})
const cleanupDays = ref(30)
const archiveDays = ref(90)
const archiveCount = ref(0)
const cleanupCount = ref(0)

const ICONS = {
  sessions: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="9" cy="7" r="4"/><path d="M2 21v-2a4 4 0 0 1 4-4h6a4 4 0 0 1 4 4v2"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>',
  messages: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>',
  checkpoint: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>',
  retention: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/></svg>'
}

const stats = computed(() => [
  { label: '活跃会话数', value: stats0.value.sessions ?? '-', icon: ICONS.sessions, desc: '有对话记录的会话总数' },
  { label: '消息总数', value: stats0.value.total_messages ?? '-', icon: ICONS.messages, desc: '全部会话的消息条数' },
  { label: 'Checkpoint 行数', value: stats0.value.checkpoint_rows ?? '-', icon: ICONS.checkpoint, desc: 'AI 上下文持久化记录数' },
  { label: '保留天数', value: stats0.value.retention_days ?? '-', icon: ICONS.retention, desc: '超过该天数未活跃的会话会被自动回收' }
])

function fmt(t) { return t ? String(t).replace('T', ' ').slice(0, 19) : '-' }

async function load() {
  try {
    const r = await api('/api/admin/session/stats')
    if (r.code === 0) {
      stats0.value = r.data || {}
      if (r.data && r.data.retention_days) cleanupDays.value = r.data.retention_days
      cleanupCount.value = r.data?.expired_sessions ?? 0
    }
  } catch (e) {}
}
async function loadArchiveCount() {
  try {
    const r = await api('/api/admin/messages/archive-count', { params: { days: archiveDays.value || 90 } })
    if (r.code === 0) archiveCount.value = r.data?.count ?? 0
  } catch (e) {}
}
async function loadCleanupCount() {
  try {
    const r = await api('/api/admin/session/cleanup-count', { params: { days: cleanupDays.value || 30 } })
    if (r.code === 0) cleanupCount.value = r.data?.count ?? 0
  } catch (e) {}
}

async function cleanup() {
  try {
    const r = await api('/api/admin/session/cleanup', { method: 'POST', body: { days: cleanupDays.value || 30 } })
    toast(r.msg || (r.code === 0 ? '清理完成' : '清理失败'), r.code === 0 ? 'success' : 'error')
    load()
    loadArchiveCount()
  } catch (e) {}
}

async function archive() {
  try {
    const r = await api('/api/admin/messages/archive', { method: 'POST', params: { days: archiveDays.value || 90 }, body: {} })
    toast(r.msg || (r.code === 0 ? '归档完成' : '归档失败'), r.code === 0 ? 'success' : 'error')
    loadArchiveCount()
  } catch (e) {}
}

onMounted(() => { load(); loadArchiveCount() })
</script>

<style scoped>
.stat-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 14px; }
.stat { padding: 16px; }
.stat-top { display: flex; align-items: center; gap: 8px; }
.stat-icon { display: inline-flex; color: var(--c-primary); }
.stat-label { font-size: 12px; color: var(--c-text-2); }
.stat-value { font-size: 24px; font-weight: 600; margin-top: 8px; color: var(--c-primary); }
.stat-desc { font-size: 11px; color: var(--c-text-3, #999); margin-top: 4px; }
.head-row { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; flex-wrap: wrap; gap: 10px; }

.op-grid { display: flex; flex-direction: column; gap: 10px; margin-bottom: 14px; }
.op-item { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; padding: 10px 12px; background: var(--c-bg); border-radius: var(--radius); }
.op-label { font-size: 13px; font-weight: 600; min-width: 92px; }
.op-controls { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.op-field { display: inline-flex; align-items: center; gap: 6px; }
.op-hint { font-size: 12px; color: var(--c-text-2); }
.btn-op { display: inline-flex; align-items: center; gap: 5px; }
.op-count { font-size: 12px; color: var(--c-text-2); }
.op-count b { color: var(--c-primary); }

.kv-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 10px; }
.kv { display: flex; justify-content: space-between; padding: 10px 12px; background: var(--c-bg); border-radius: var(--radius); font-size: 13px; }
.kv-k { color: var(--c-text-2); }
.kv-v { font-weight: 500; font-family: monospace; }
</style>
