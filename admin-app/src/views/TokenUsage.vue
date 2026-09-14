<template>
  <div>
    <div class="head-row">
      <div class="filters">
        <span class="text-3" style="font-size:12px;">统计范围</span>
        <select v-model="days" class="select" style="width:110px;" @change="load">
          <option :value="1">今天</option>
          <option :value="7">近 7 天</option>
          <option :value="30">近 30 天</option>
        </select>
        <select v-model="groupBy" class="select" style="width:130px;" @change="load">
          <option value="day">按天</option>
          <option value="model">按模型</option>
          <option value="tenant">按租户</option>
        </select>
        <select v-model="model" class="select" style="width:180px;" @change="load">
          <option value="">全部模型</option>
          <option v-for="m in modelNames" :key="m" :value="m">{{ m }}</option>
        </select>
        <select v-model="source" class="select" style="width:130px;" @change="load">
          <option value="">全部来源</option>
          <option value="platform">平台模型</option>
          <option value="custom">自定义模型</option>
        </select>
      </div>
      <button class="btn btn-sm" @click="load">刷新</button>
    </div>

    <!-- 汇总卡片 -->
    <div class="stat-grid" style="margin-top:16px;">
      <div class="card stat">
        <div class="stat-label text-3">调用次数</div>
        <div class="stat-value">{{ fmtNum(summary.calls) }}</div>
      </div>
      <div class="card stat">
        <div class="stat-label text-3">输入 Tokens</div>
        <div class="stat-value">{{ fmtNum(summary.input_tokens) }}</div>
      </div>
      <div class="card stat">
        <div class="stat-label text-3">输出 Tokens</div>
        <div class="stat-value">{{ fmtNum(summary.output_tokens) }}</div>
      </div>
      <div class="card stat">
        <div class="stat-label text-3">总 Tokens</div>
        <div class="stat-value">{{ fmtNum(summary.total_tokens) }}</div>
      </div>
      <div class="card stat">
        <div class="stat-label text-3">总费用（元）</div>
        <div class="stat-value" style="color:var(--c-primary);">{{ fmtMoney(summary.total_cost) }}</div>
      </div>
    </div>

    <div class="card" style="margin-top:16px;">
      <DataTable :columns="columns" :rows="rows">
        <template #cell-dim="{ row }"><span class="mono">{{ row.dim }}</span></template>
        <template #cell-date="{ row }"><span class="mono">{{ row.date }}</span></template>
        <template #cell-calls="{ row }">{{ fmtNum(row.calls) }}</template>
        <template #cell-input_tokens="{ row }">{{ fmtNum(row.input_tokens) }}</template>
        <template #cell-output_tokens="{ row }">{{ fmtNum(row.output_tokens) }}</template>
        <template #cell-total_tokens="{ row }"><b>{{ fmtNum(row.total_tokens) }}</b></template>
        <template #cell-cost="{ row }">{{ fmtMoney(row.cost) }}</template>
      </DataTable>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api'
import DataTable from '../components/DataTable.vue'

const days = ref(7)
const groupBy = ref('day')
const model = ref('')
const source = ref('')
const rows = ref([])
const summary = ref({})
const modelNames = ref([])

const columns = [
  { key: 'date', title: '日期', width: '130px' },
  { key: 'dim', title: '维度', width: '220px' },
  { key: 'calls', title: '调用次数', align: 'right' },
  { key: 'input_tokens', title: '输入 Tokens', align: 'right' },
  { key: 'output_tokens', title: '输出 Tokens', align: 'right' },
  { key: 'total_tokens', title: '总 Tokens', align: 'right' },
  { key: 'cost', title: '费用（元）', align: 'right' }
]

const fmtNum = n => (n ?? 0).toLocaleString()
// 费用：0=未定价显示 —；有值显示 ¥xx.xxxx（快照，价格调整不影响历史）
const fmtMoney = n => {
  const v = Number(n ?? 0)
  return v > 0 ? '¥' + v.toFixed(4) : '—'
}

async function load() {
  try {
    const params = { days: days.value, group_by: groupBy.value }
    if (model.value) params.model = model.value
    if (source.value) params.source = source.value
    const r = await api('/api/admin/token-usage', { params })
    if (r.code === 0) {
      rows.value = r.data.rows || []
      summary.value = r.data.summary || {}
      // 按模型分组时顺带刷新模型下拉名单（含 model 筛选时后端已按模型过滤，仅当未筛选时收集）
      if (groupBy.value === 'model' && !model.value) {
        modelNames.value = [...new Set((r.data.rows || []).map(x => x.dim).filter(Boolean))]
      }
    }
  } catch (e) {}
}

// 模型下拉名单：初始化时单独拉一次（按模型聚合），避免每次刷新重复调用
async function loadModelNames() {
  try {
    const mr = await api('/api/admin/token-usage', { params: { days: days.value, group_by: 'model' } })
    if (mr.code === 0 && mr.data) {
      const names = [...new Set((mr.data.rows || []).map(x => x.dim).filter(Boolean))]
      if (names.length) modelNames.value = names
    }
  } catch (e) {}
}

onMounted(() => { load(); loadModelNames() })
</script>

<style scoped>
.head-row { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; }
.filters { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.stat-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 14px; }
.stat { padding: 16px; }
.stat-label { font-size: 12px; }
.stat-value { font-size: 22px; font-weight: 600; margin-top: 6px; color: var(--c-primary); font-family: monospace; }
</style>
