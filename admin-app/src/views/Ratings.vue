<template>
  <div>
    <!-- 统计卡片 -->
    <div class="stat-grid" style="display:flex;gap:16px;flex-wrap:wrap;margin-bottom:16px;">
      <div class="card" style="flex:1 1 160px;min-width:150px;padding:16px;">
        <div class="text-2" style="font-size:12px;">评价总数</div>
        <div class="stat-num" style="font-size:24px;font-weight:600;margin-top:6px;">{{ stats.total || 0 }}</div>
      </div>
      <div class="card" style="flex:1 1 160px;min-width:150px;padding:16px;">
        <div class="text-2" style="font-size:12px;">平均分</div>
        <div class="stat-num" style="font-size:24px;font-weight:600;margin-top:6px;color:#f5a623;">{{ stats.avg_score ? stats.avg_score.toFixed(2) : '-' }}<span style="font-size:13px;color:#999;"> / 5</span></div>
      </div>
      <div class="card" style="flex:1 1 160px;min-width:150px;padding:16px;">
        <div class="text-2" style="font-size:12px;">满意率（4-5星）</div>
        <div class="stat-num" style="font-size:24px;font-weight:600;margin-top:6px;color:#52c41a;">{{ stats.satisfy_rate ? stats.satisfy_rate.toFixed(1) : 0 }}%</div>
      </div>
      <div class="card" style="flex:1 1 160px;min-width:150px;padding:16px;">
        <div class="text-2" style="font-size:12px;">近7天评价</div>
        <div class="stat-num" style="font-size:24px;font-weight:600;margin-top:6px;">{{ weekTotal }}</div>
      </div>
    </div>

    <!-- 星级分布 + 近7天 -->
    <div style="display:flex;gap:16px;flex-wrap:wrap;margin-bottom:16px;">
      <div class="card" style="flex:1 1 300px;min-width:280px;padding:16px;">
        <div style="font-size:13px;font-weight:600;margin-bottom:12px;">星级分布</div>
        <div v-for="s in 5" :key="s" style="display:flex;align-items:center;gap:8px;margin-bottom:8px;">
          <span style="width:46px;font-size:12px;color:#666;">{{ s }} 星</span>
          <div style="flex:1;height:10px;background:#f0f0f0;border-radius:5px;overflow:hidden;">
            <div :style="{ width: distPct(6 - s) + '%', background: distColor(6 - s), height: '100%', borderRadius: '5px' }"></div>
          </div>
          <span style="width:34px;font-size:12px;color:#999;text-align:right;">{{ stats.dist ? stats.dist[6 - s] : 0 }}</span>
        </div>
      </div>
      <div class="card" style="flex:1 1 340px;min-width:300px;padding:16px;">
        <div style="font-size:13px;font-weight:600;margin-bottom:12px;">近 7 天趋势（条数）</div>
        <div style="display:flex;align-items:flex-end;gap:8px;height:120px;padding-top:8px;">
          <div v-for="d in stats.last7days || []" :key="d.date" style="flex:1;display:flex;flex-direction:column;align-items:center;gap:4px;">
            <span style="font-size:11px;color:#666;">{{ d.count }}</span>
            <div :style="{ width: '100%', maxWidth: '34px', height: Math.max(4, Math.min(80, (d.count / weekMax) * 80)) + 'px', background: '#8BC8EA', borderRadius: '4px 4px 0 0' }"></div>
            <span style="font-size:10px;color:#999;">{{ d.date.slice(5) }}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 列表 -->
    <div class="head-row" style="margin-top:4px;">
      <div class="filters">
        <input v-model="filter.q" class="input" style="width:180px;" placeholder="会话ID / 标签 / 留言" @keyup.enter="loadList" />
        <select v-model="filter.score" class="select" style="width:110px;" @change="loadList">
          <option :value="''">全部星级</option>
          <option v-for="s in 5" :key="s" :value="String(s)">{{ s }} 星</option>
        </select>
        <input v-model="filter.start" class="input" style="width:140px;" type="date" @change="loadList" />
        <span class="text-2">至</span>
        <input v-model="filter.end" class="input" style="width:140px;" type="date" @change="loadList" />
        <button class="btn btn-sm btn-primary" @click="loadList">查询</button>
        <button class="btn btn-sm" @click="resetFilter">重置</button>
      </div>
    </div>

    <div class="card" style="margin-top:16px;">
      <DataTable :columns="columns" :rows="list">
        <template #cell-score="{ row }">
          <span :style="{ color: row.score >= 4 ? '#52c41a' : row.score === 3 ? '#f5a623' : '#e5484d', fontWeight: 600 }">★ {{ row.score }}</span>
        </template>
        <template #cell-thread_id="{ row }">
          <span style="font-family:monospace;font-size:12px;color:#666;">{{ row.thread_id }}</span>
        </template>
        <template #cell-tags="{ row }">
          <span v-if="row.tags">
            <span v-for="t in row.tags.split(',')" :key="t" class="badge badge-blue" style="margin-right:4px;">{{ t }}</span>
          </span>
          <span v-else class="text-2">-</span>
        </template>
        <template #cell-comment="{ row }">
          <div class="text-2" style="max-width:300px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;" :title="row.comment">{{ row.comment || '-' }}</div>
        </template>
        <template #cell-created_at="{ row }">
          <span class="text-2">{{ fmtTime(row.created_at) }}</span>
        </template>
      </DataTable>
      <div class="pager" style="display:flex;justify-content:space-between;align-items:center;padding:12px 16px;">
        <span class="text-2">共 {{ total }} 条</span>
        <div>
          <button class="btn btn-sm" :disabled="page <= 1" @click="pageChange(page - 1)">上一页</button>
          <span style="margin:0 10px;font-size:13px;color:#666;">{{ page }} / {{ totalPages || 1 }}</span>
          <button class="btn btn-sm" :disabled="page >= totalPages" @click="pageChange(page + 1)">下一页</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { api } from '../api'
import DataTable from '../components/DataTable.vue'

const columns = [
  { key: 'score', title: '评分', width: '70px' },
  { key: 'thread_id', title: '会话ID', width: '200px' },
  { key: 'tags', title: '标签', width: '180px' },
  { key: 'comment', title: '留言' },
  { key: 'created_at', title: '评价时间', width: '160px' }
]

const stats = ref({})
const list = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = 10
const filter = reactive({ q: '', score: '', start: '', end: '' })

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const weekTotal = computed(() => (stats.value.last7days || []).reduce((s, d) => s + d.count, 0))
const weekMax = computed(() => Math.max(1, ...(stats.value.last7days || []).map(d => d.count)))

function distPct(score) {
  const n = stats.value.dist ? stats.value.dist[score] : 0
  const tot = stats.value.total || 0
  return tot ? (n / tot) * 100 : 0
}
function distColor(score) {
  return score >= 4 ? '#52c41a' : score === 3 ? '#f5a623' : '#e5484d'
}
function fmtTime(t) {
  if (!t) return '-'
  return String(t).replace('T', ' ').slice(0, 19)
}

async function loadStats() {
  const r = await api('/api/admin/ratings/stats')
  if (r.code === 0) stats.value = r.data || {}
}
async function loadList() {
  const params = new URLSearchParams({ page: page.value, page_size: pageSize })
  if (filter.q.trim()) params.set('q', filter.q.trim())
  if (filter.score) params.set('score', filter.score)
  if (filter.start) params.set('start', filter.start)
  if (filter.end) params.set('end', filter.end)
  const r = await api('/api/admin/ratings?' + params.toString())
  if (r.code === 0) {
    list.value = r.data.list || []
    total.value = r.data.total || 0
  }
}
function pageChange(p) {
  page.value = p
  loadList()
}
function resetFilter() {
  filter.q = ''
  filter.score = ''
  filter.start = ''
  filter.end = ''
  page.value = 1
  loadList()
}

onMounted(() => {
  loadStats()
  loadList()
})
</script>

<style scoped>
.head-row { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; }
.filters { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
</style>
