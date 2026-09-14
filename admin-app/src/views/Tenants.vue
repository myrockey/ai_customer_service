<template>
  <div>
    <div class="head-row">
      <div class="filters">
        <input v-model="filter.name" class="input" style="width:140px;" placeholder="名称" @keyup.enter="search" />
        <input v-model="filter.app_key" class="input" style="width:150px;" placeholder="app_key" @keyup.enter="search" />
        <select v-model="filter.status" class="select" style="width:110px;" @change="search">
          <option value="">全部状态</option>
          <option value="1">启用</option>
          <option value="0">禁用</option>
        </select>
        <button class="btn btn-sm btn-primary" @click="search">查询</button>
        <button class="btn btn-sm" @click="resetFilter">重置</button>
      </div>
      <button class="btn btn-primary btn-sm" @click="openCreate">+ 新建租户</button>
    </div>

    <div class="card" style="margin-top:16px;">
      <DataTable :columns="columns" :rows="list">
        <template #cell-name="{ row }">
          <b>{{ row.name }}</b>
        </template>
        <template #cell-app_key="{ row }">
          <span class="mono">{{ row.app_key }}</span>
        </template>
        <template #cell-status="{ row }">
          <span class="badge" :class="row.status === 1 ? 'badge-green' : 'badge-red'">{{ row.status === 1 ? '启用' : '禁用' }}</span>
        </template>
        <template #cell-model_provider="{ row }">
          {{ row.model_provider === 'custom' ? '租户自定义' : '平台默认' }}
        </template>
        <template #cell-quota="{ row }">
          <span class="quota-cell">
            <span class="q-item" :class="over(row.active_sessions, row.max_concurrent_sessions)">会 {{ row.active_sessions ?? 0 }}/{{ row.max_concurrent_sessions ?? '-' }}</span>
            <span class="q-sep">·</span>
            <span class="q-item" :class="over(row.today_messages, row.daily_message_limit)">消 {{ row.today_messages ?? 0 }}/{{ row.daily_message_limit ?? '-' }}</span>
            <span class="q-sep">·</span>
            <span class="q-item" :class="over(row.kb_docs, row.max_kb_docs)">文 {{ row.kb_docs ?? 0 }}/{{ row.max_kb_docs ?? '-' }}</span>
            <span class="q-sep">·</span>
            <span class="q-item" :class="over(row.kb_size_mb, row.max_kb_size_mb)">{{ (row.kb_size_mb ?? 0).toFixed(1) }}/{{ row.max_kb_size_mb ?? '-' }}MB</span>
          </span>
        </template>
        <template #cell-created_at="{ row }">
          <span class="text-2" style="font-size:12px;">{{ fmt(row.created_at) }}</span>
        </template>
        <template #cell-actions="{ row }">
          <button class="btn btn-sm" @click="openEdit(row)">编辑</button>
          <button class="btn btn-sm" @click="toggle(row)">{{ row.status === 1 ? '禁用' : '启用' }}</button>
          <button class="btn btn-sm" @click="resetSecret(row)">重置密钥</button>
          <button class="btn btn-sm btn-danger" @click="remove(row)">删除</button>
        </template>
      </DataTable>
      <div style="margin-top:12px;border-top:1px solid var(--c-border);padding-top:10px;">
        <Pagination :total="total" :page="page" :size="size" @change="onPage" />
      </div>
    </div>

    <!-- 新建/编辑 -->
    <Modal :visible="showModal" :title="editing ? '编辑租户' : '新建租户'" @close="showModal = false">
      <div class="form">
        <label class="field">
          <span class="f-label">租户名称</span>
          <input v-model="form.name" class="input" placeholder="如: 洛奇网络" />
        </label>
        <template v-if="!editing">
          <label class="field">
            <span class="f-label">app_key（留空自动生成）</span>
            <input v-model="form.app_key" class="input mono" placeholder="自动生成" />
          </label>
          <div class="tip">创建后将自动生成 app_secret（仅显示一次），请妥善保存。</div>
        </template>
        <label class="field" style="flex-direction:row;align-items:center;gap:8px;">
          <input v-model="form.is_platform_admin" type="checkbox" :disabled="!!editing" />
          <span class="f-label" style="margin:0;">平台管理员账号</span>
        </label>
        <div class="quota-box">
          <div class="quota-title">租户配额 <span class="quota-tip">0 表示不限制</span></div>
          <div class="quota-grid">
            <label class="field">
              <span class="f-label">并发会话数</span>
              <input v-model.number="form.max_concurrent_sessions" type="number" min="0" class="input" placeholder="0=不限制" />
            </label>
            <label class="field">
              <span class="f-label">每日消息数</span>
              <input v-model.number="form.daily_message_limit" type="number" min="0" class="input" placeholder="0=不限制" />
            </label>
            <label class="field">
              <span class="f-label">知识库文档数</span>
              <input v-model.number="form.max_kb_docs" type="number" min="0" class="input" placeholder="0=不限制" />
            </label>
            <label class="field">
              <span class="f-label">知识库容量（MB）</span>
              <input v-model.number="form.max_kb_size_mb" type="number" min="0" class="input" placeholder="0=不限制" />
            </label>
          </div>
        </div>
      </div>
      <template #footer>
        <button class="btn" @click="showModal = false">取消</button>
        <button class="btn btn-primary" @click="save">{{ editing ? '保存' : '创建' }}</button>
      </template>
    </Modal>

    <!-- 创建结果（secret 一次性展示） -->
    <Modal :visible="!!createdResult" title="租户创建成功" @close="createdResult = null">
      <div class="result-box">
        <p>请保存以下凭据（app_secret 仅此一次展示）：</p>
        <div class="kv"><span class="kv-k">tenant_id</span><span class="kv-v mono">{{ createdResult?.tenant_id }}</span></div>
        <div class="kv"><span class="kv-k">app_key</span><span class="kv-v mono">{{ createdResult?.app_key }}</span></div>
        <div class="kv"><span class="kv-k">app_secret</span><span class="kv-v mono">{{ createdResult?.app_secret }}</span></div>
      </div>
      <template #footer>
        <button class="btn btn-primary" @click="createdResult = null">我已保存</button>
      </template>
    </Modal>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { api, toast } from '../api'
import DataTable from '../components/DataTable.vue'
import Modal from '../components/Modal.vue'
import Pagination from '../components/Pagination.vue'

const all = ref([])
const page = ref(1)
const size = ref(10)
const filter = reactive({ name: '', app_key: '', status: '' })
const showModal = ref(false)
const editing = ref(null)
const form = reactive({ name: '', app_key: '', is_platform_admin: false, max_concurrent_sessions: 100, daily_message_limit: 1000, max_kb_docs: 100, max_kb_size_mb: 100 })
const createdResult = ref(null)

const columns = [
  { key: 'name', title: '名称' },
  { key: 'app_key', title: 'app_key', width: '160px' },
  { key: 'status', title: '状态', width: '80px' },
  { key: 'model_provider', title: '模型配置', width: '120px' },
  { key: 'quota', title: '用量 / 配额（并发·消息·文档·容量）', width: '280px' },
  { key: 'created_at', title: '创建时间', width: '160px' },
  { key: 'actions', title: '操作', width: '240px' }
]

const fmt = t => t ? String(t).replace('T', ' ').slice(0, 19) : '-'
const over = (used, limit) => (limit > 0 && (used ?? 0) >= limit) ? 'q-over' : ''

const filtered = computed(() => {
  let rows = all.value
  if (filter.name) rows = rows.filter(r => (r.name || '').includes(filter.name))
  if (filter.app_key) rows = rows.filter(r => (r.app_key || '').includes(filter.app_key))
  if (filter.status !== '') rows = rows.filter(r => String(r.status) === filter.status)
  return rows
})
const total = computed(() => filtered.value.length)
const list = computed(() => {
  const start = (page.value - 1) * size.value
  return filtered.value.slice(start, start + size.value)
})

async function load() {
  try {
    const r = await api('/api/admin/tenants')
    if (r.code === 0) all.value = r.data || []
  } catch (e) {}
}
function onPage({ page: p, size: s }) { size.value = s; page.value = p }
function search() { page.value = 1 }
function resetFilter() {
  Object.assign(filter, { name: '', app_key: '', status: '' })
  page.value = 1
}

function openCreate() {
  editing.value = null
  Object.assign(form, { name: '', app_key: '', is_platform_admin: false, max_concurrent_sessions: 100, daily_message_limit: 1000, max_kb_docs: 100, max_kb_size_mb: 100 })
  showModal.value = true
}
function openEdit(t) {
  editing.value = t
  Object.assign(form, {
    name: t.name, app_key: t.app_key, is_platform_admin: !!t.is_platform_admin,
    max_concurrent_sessions: t.max_concurrent_sessions ?? 100, daily_message_limit: t.daily_message_limit ?? 1000,
    max_kb_docs: t.max_kb_docs ?? 100, max_kb_size_mb: t.max_kb_size_mb ?? 100
  })
  showModal.value = true
}

async function save() {
  if (!form.name) return toast('请填写租户名称', 'warning')
  const quotaBody = {
    max_concurrent_sessions: form.max_concurrent_sessions || 0,
    daily_message_limit: form.daily_message_limit || 0,
    max_kb_docs: form.max_kb_docs || 0,
    max_kb_size_mb: form.max_kb_size_mb || 0
  }
  try {
    if (editing.value) {
      const r = await api('/api/admin/tenants/' + encodeURIComponent(editing.value.tenant_id), { method: 'PUT', body: { name: form.name } })
      if (r.code !== 0) return toast(r.msg || '保存失败', 'error')
      const q = await api('/api/admin/tenants/' + encodeURIComponent(editing.value.tenant_id) + '/quota', { method: 'PUT', body: quotaBody })
      if (q.code !== 0) return toast(q.msg || '配额保存失败', 'error')
      toast('保存成功', 'success')
    } else {
      const r = await api('/api/admin/tenants', { method: 'POST', body: { name: form.name, app_key: form.app_key || undefined, is_platform_admin: form.is_platform_admin } })
      if (r.code !== 0) return toast(r.msg || '创建失败', 'error')
      const tid = r.data?.tenant_id
      if (tid) {
        await api('/api/admin/tenants/' + encodeURIComponent(tid) + '/quota', { method: 'PUT', body: quotaBody })
      }
      if (r.data && r.data.app_secret) {
        createdResult.value = r.data
      } else {
        toast(r.msg || '保存成功', 'success')
      }
    }
    showModal.value = false
    load()
  } catch (e) {}
}

async function toggle(t) {
  try {
    const r = await api('/api/admin/tenants/' + encodeURIComponent(t.tenant_id) + '/toggle', { method: 'POST', body: {} })
    toast(r.msg || (r.code === 0 ? '操作成功' : '操作失败'), r.code === 0 ? 'success' : 'error')
    load()
  } catch (e) {}
}

async function resetSecret(t) {
  if (!confirm('确认重置租户「' + t.name + '」的 app_secret？旧密钥立即失效。')) return
  try {
    const r = await api('/api/admin/tenants/' + encodeURIComponent(t.tenant_id) + '/reset-secret', { method: 'POST', body: {} })
    if (r.code === 0 && r.data && r.data.app_secret) {
      createdResult.value = { tenant_id: t.tenant_id, app_key: t.app_key, app_secret: r.data.app_secret }
    } else {
      toast(r.msg || '重置成功', 'success')
    }
  } catch (e) {}
}

async function remove(t) {
  if (!confirm('确认删除租户「' + t.name + '」？该操作不可恢复。')) return
  try {
    const r = await api('/api/admin/tenants/' + encodeURIComponent(t.tenant_id), { method: 'DELETE' })
    toast(r.msg || '删除成功', r.code === 0 ? 'success' : 'error')
    load()
  } catch (e) {}
}

onMounted(() => load())
</script>

<style scoped>
.head-row { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; }
.filters { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.form { display: flex; flex-direction: column; gap: 14px; }
.field { display: flex; flex-direction: column; gap: 6px; }
.f-label { font-size: 12px; color: var(--c-text-2); }
.tip { font-size: 11px; color: var(--c-warning); }
.quota-box { border: 1px dashed var(--c-border-strong); border-radius: var(--radius); padding: 12px; display: flex; flex-direction: column; gap: 10px; }
.quota-title { font-size: 12px; font-weight: 600; }
.quota-tip { font-size: 11px; color: var(--c-text-2); font-weight: 400; margin-left: 6px; }
.quota-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 10px; }
.quota-cell { display: flex; gap: 4px; flex-wrap: wrap; font-size: 12px; }
.q-item { color: var(--c-text-2); }
.q-item.q-over { color: var(--c-danger, #e74c3c); font-weight: 600; }
.q-sep { color: var(--c-border-strong); }
.result-box { display: flex; flex-direction: column; gap: 10px; font-size: 13px; }
.kv { display: flex; justify-content: space-between; padding: 8px 10px; background: var(--c-bg); border-radius: 6px; }
.kv-k { color: var(--c-text-2); font-size: 12px; }
.kv-v { font-weight: 500; word-break: break-all; }
</style>
