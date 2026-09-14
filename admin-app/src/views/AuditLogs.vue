<template>
  <div>
    <div class="head-row">
      <span style="font-weight:600;">审计日志</span>
      <div class="filters">
        <input v-model="timeFrom" type="datetime-local" class="input" style="width:190px;" title="开始时间" />
        <span class="text-3">至</span>
        <input v-model="timeTo" type="datetime-local" class="input" style="width:190px;" title="结束时间" />
        <select v-model="action" class="select" style="width:100px;" @change="load(1)">
          <option value="">全部操作</option>
          <option value="create">新建</option>
          <option value="update">修改</option>
          <option value="delete">删除</option>
        </select>
        <select v-model="objectType" class="select" style="width:110px;" @change="load(1)">
          <option value="">全部对象</option>
          <option value="ticket">工单</option>
          <option value="prompt">Prompt</option>
          <option value="knowledge">知识库</option>
          <option value="tenant">租户</option>
          <option value="model_config">模型配置</option>
          <option value="model_provider">模型提供商</option>
          <option value="session">会话</option>
          <option value="auth">登录认证</option>
          <option value="other">其他</option>
        </select>
        <input v-model="operator" class="input" style="width:130px;" placeholder="操作人" @keyup.enter="load(1)" />
        <button class="btn btn-sm" @click="load(1)">查询</button>
        <button class="btn btn-sm" @click="reset">重置</button>
        <span class="text-3" style="font-size:12px;">共 {{ total }} 条</span>
      </div>
    </div>

    <div class="card" style="margin-top:16px;">
      <DataTable :columns="columns" :rows="list">
        <template #cell-created_at="{ row }">
          <span class="mono">{{ fmt(row.created_at) }}</span>
        </template>
        <template #cell-tenant_id="{ row }">
          <span class="mono">{{ row.tenant_id }}</span>
        </template>
        <template #cell-action="{ row }">
          <span class="badge" :class="actionClass(row.action)">{{ actionText(row.action) }}</span>
        </template>
        <template #cell-object_type="{ row }">
          {{ objectText(row.object_type) }}
        </template>
        <template #cell-object_id="{ row }">
          <span class="mono">{{ row.object_id || '-' }}</span>
        </template>
        <template #cell-ip="{ row }">
          <span class="mono">{{ row.ip || '-' }}</span>
        </template>
      </DataTable>
      <div style="margin-top:12px;border-top:1px solid var(--c-border);padding-top:10px;">
        <Pagination :total="total" :page="page" :size="size" @change="onPage" />
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api'
import DataTable from '../components/DataTable.vue'
import Pagination from '../components/Pagination.vue'

const list = ref([])
const total = ref(0)
const page = ref(1)
const size = ref(10)
const action = ref('')
const objectType = ref('')
const operator = ref('')
const timeFrom = ref('')
const timeTo = ref('')

const ACTION = { create: ['新建', 'badge-blue'], update: ['修改', 'badge-orange'], delete: ['删除', 'badge-red'] }
const OBJECT = { ticket: '工单', prompt: 'Prompt', knowledge: '知识库', tenant: '租户', model_config: '模型配置', model_provider: '模型提供商', platform_config: '平台配置', session: '会话', other: '其他' }
const actionText = a => (ACTION[a] || [a])[0]
const actionClass = a => (ACTION[a] || [null, 'badge-gray'])[1]
const objectText = o => OBJECT[o] || o
const fmt = t => t ? String(t).replace('T', ' ').slice(0, 19) : '-'

const columns = [
  { key: 'created_at', title: '时间', width: '170px' },
  { key: 'tenant_id', title: '租户', width: '140px' },
  { key: 'action', title: '操作', width: '90px' },
  { key: 'object_type', title: '对象', width: '110px' },
  { key: 'object_id', title: '对象ID' },
  { key: 'ip', title: 'IP', width: '140px' }
]

async function load(pg) {
  page.value = pg || 1
  try {
    const params = { page: page.value, page_size: size.value }
    if (action.value) params.action = action.value
    if (objectType.value) params.object_type = objectType.value
    if (operator.value.trim()) params.operator = operator.value.trim()
    if (timeFrom.value) params.time_from = timeFrom.value.replace('T', ' ')
    if (timeTo.value) params.time_to = timeTo.value.replace('T', ' ')
    const r = await api('/api/admin/audit-logs', { params })
    if (r.code === 0) {
      list.value = r.data.list || []
      total.value = r.data.total || 0
    }
  } catch (e) {}
}
function onPage({ page: p, size: s }) { size.value = s; load(p) }
function reset() {
  action.value = ''; objectType.value = ''; operator.value = ''; timeFrom.value = ''; timeTo.value = ''
  load(1)
}

onMounted(() => load(1))
</script>

<style scoped>
.head-row { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; }
.filters { display: flex; gap: 8px; align-items: center; }
</style>
