<template>
  <div>
    <div class="card">
      <div class="head-row">
        <div class="left">
          <b>客户列表</b>
          <span class="text-3" style="font-size:12px;">共 {{ total }} 位客户（含已归档会话，数据不会因清理/归档丢失）</span>
        </div>
        <div class="right">
          <input v-model="keyword" class="input search-input" placeholder="搜索 姓名/电话/邮箱/地址/备注" @keyup.enter="search" />
          <button class="btn btn-sm" @click="search">搜索</button>
          <button class="btn btn-sm" @click="reset">重置</button>
        </div>
      </div>

      <table class="table">
        <thead>
          <tr>
            <th>客户</th>
            <th>电话</th>
            <th>邮箱</th>
            <th>地址</th>
            <th>备注</th>
            <th>消息数</th>
            <th>最后活跃</th>
            <th>来源</th>
            <th style="width:80px;">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!list.length">
            <td colspan="9" class="empty">暂无客户（客户发消息时自动提取电话/邮箱后才会出现在这里）</td>
          </tr>
          <tr v-for="c in list" :key="c.thread_id">
            <td>
              <div class="mono" style="font-size:12px;" :title="c.thread_id">{{ c.name || '-' }}</div>
              <div class="text-3" style="font-size:11px;">{{ shortThread(c.thread_id) }}</div>
            </td>
            <td class="mono">{{ c.phone || '-' }}</td>
            <td class="mono">{{ c.email || '-' }}</td>
            <td>{{ c.address || '-' }}</td>
            <td class="text-2">{{ c.remark || '-' }}</td>
            <td>{{ c.message_count || 0 }}</td>
            <td class="text-2">{{ fmtTime(c.last_active_at) }}</td>
            <td>
              <span class="badge" :class="c.extract_status === 1 ? 'badge-blue' : 'badge-green'">
                {{ c.extract_status === 1 ? '自动提取' : '人工填写' }}
              </span>
            </td>
            <td>
              <button class="btn btn-sm" @click="openSession(c)">看会话</button>
            </td>
          </tr>
        </tbody>
      </table>

      <div class="pager">
        <button class="btn btn-sm" :disabled="page <= 1" @click="turn(-1)">上一页</button>
        <span class="text-3" style="font-size:12px;">{{ page }}/{{ pages }}</span>
        <button class="btn btn-sm" :disabled="page >= pages" @click="turn(1)">下一页</button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api, toast } from '../api'

const router = useRouter()
const list = ref([])
const total = ref(0)
const page = ref(1)
const size = 20
const keyword = ref('')
const lastQuery = ref('')

const pages = computed(() => Math.max(1, Math.ceil(total.value / size)))

function fmtTime(t) { return t ? String(t).replace('T', ' ').slice(0, 19) : '-' }
function shortThread(t) { return t ? (t.length > 24 ? t.slice(0, 22) + '…' : t) : '-' }

async function load() {
  try {
    const r = await api('/api/admin/customers', { params: { page: page.value, size, q: lastQuery.value } })
    if (r.code !== 0) return
    list.value = r.data.list || []
    total.value = r.data.total || 0
  } catch (e) {
  }
}

function search() {
  page.value = 1
  lastQuery.value = keyword.value.trim()
  load()
}

function reset() {
  keyword.value = ''
  lastQuery.value = ''
  page.value = 1
  load()
}

function turn(d) {
  const np = page.value + d
  if (np < 1) return
  page.value = np
  load()
}

function openSession(c) {
  router.push({ path: '/sessions', query: { thread: c.thread_id } })
}

onMounted(load)
</script>

<style scoped>
.card { padding: 16px; }
.head-row { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; flex-wrap: wrap; gap: 10px; }
.left { display: flex; align-items: baseline; gap: 10px; }
.right { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.search-input { width: 260px; padding: 7px 10px; border: 1px solid var(--c-border); border-radius: var(--radius); background: var(--c-bg-1); color: var(--c-text-1); font-size: 13px; }
.table { width: 100%; border-collapse: collapse; font-size: 13px; }
.table th { text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--c-border); color: var(--c-text-2); font-weight: 500; white-space: nowrap; }
.table td { padding: 9px 10px; border-bottom: 1px solid var(--c-border); vertical-align: middle; }
.table tr:hover td { background: var(--c-hover); }
.empty { text-align: center; color: var(--c-text-3); padding: 24px 0; }
.pager { display: flex; align-items: center; justify-content: flex-end; gap: 8px; margin-top: 12px; }
.text-2 { color: var(--c-text-2); }
.text-3 { color: var(--c-text-3, #999); }
</style>
