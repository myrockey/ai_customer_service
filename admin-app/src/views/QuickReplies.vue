<template>
  <div>
    <div class="head-row">
      <div class="filters">
        <input v-model="filter.q" class="input" style="width:180px;" placeholder="标题/内容关键词" @keyup.enter="search" />
        <input v-model="filter.category" class="input" style="width:120px;" placeholder="分类" @keyup.enter="search" />
        <select v-model="filter.active" class="select" style="width:110px;">
          <option :value="''">全部状态</option>
          <option value="1">已启用</option>
          <option value="0">已停用</option>
        </select>
        <button class="btn btn-sm btn-primary" @click="search">查询</button>
        <button class="btn btn-sm" @click="resetFilter">重置</button>
      </div>
      <button class="btn btn-primary btn-sm" v-perm="'quickreply:add'" @click="openCreate">+ 新增话术</button>
    </div>

    <div class="card" style="margin-top:16px;">
      <DataTable :columns="columns" :rows="filtered">
        <template #cell-category="{ row }">
          <span class="badge badge-blue">{{ row.category }}</span>
        </template>
        <template #cell-content="{ row }">
          <div class="text-2" style="max-width:420px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;">{{ row.content }}</div>
        </template>
        <template #cell-is_active="{ row }">
          <button class="btn btn-sm" :class="row.is_active === 1 ? 'btn-primary' : ''" @click="toggleActive(row)">
            {{ row.is_active === 1 ? '启用中' : '已停用' }}
          </button>
        </template>
        <template #cell-actions="{ row }">
          <button class="btn btn-sm" v-perm="'quickreply:edit'" @click="openEdit(row)">编辑</button>
          <button class="btn btn-sm btn-danger" v-perm="'quickreply:remove'" @click="del(row)">删除</button>
        </template>
      </DataTable>
    </div>

    <!-- 新增 / 编辑话术 -->
    <Modal :visible="editVisible" :title="editing ? '编辑话术 · ' + editing.title : '新增话术'" @close="editVisible = false">
      <div class="form">
        <label class="field">
          <span class="f-label">分类</span>
          <input v-model="form.category" class="input" placeholder="退款 / 物流 / 投诉 / 欢迎..." />
        </label>
        <label class="field">
          <span class="f-label">标题 <b style="color:#e5484d;">*</b></span>
          <input v-model="form.title" class="input" placeholder="例如：退款进度说明" />
        </label>
        <label class="field">
          <span class="f-label">内容 <b style="color:#e5484d;">*</b></span>
          <textarea v-model="form.content" class="textarea" style="min-height:120px;" placeholder="回复正文，客服在工单页插入后可再编辑"></textarea>
        </label>
        <label class="field">
          <span class="f-label">排序</span>
          <input v-model.number="form.sort" class="input" style="width:120px;" type="number" placeholder="0" />
        </label>
      </div>
      <template #footer>
        <button class="btn" @click="editVisible = false">取消</button>
        <button class="btn btn-primary" :disabled="saving" @click="submit">{{ saving ? '提交中...' : '保存' }}</button>
      </template>
    </Modal>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { api, toast } from '../api'
import DataTable from '../components/DataTable.vue'
import Modal from '../components/Modal.vue'

const columns = [
  { key: 'category', title: '分类', width: '100px' },
  { key: 'title', title: '标题', width: '160px' },
  { key: 'content', title: '内容' },
  { key: 'sort', title: '排序', width: '70px' },
  { key: 'is_active', title: '状态', width: '100px' },
  { key: 'actions', title: '操作', width: '140px' }
]

const list = ref([])
const editVisible = ref(false)
const saving = ref(false)
const editing = ref(null)
const filter = reactive({ q: '', category: '', active: '' })

const form = reactive({ category: '通用', title: '', content: '', sort: 0 })

const filtered = computed(() => {
  let rows = list.value
  const q = filter.q.trim().toLowerCase()
  if (q) rows = rows.filter(r => (r.title || '').toLowerCase().includes(q) || (r.content || '').toLowerCase().includes(q))
  const cat = filter.category.trim()
  if (cat) rows = rows.filter(r => r.category === cat)
  const act = filter.active
  if (act !== '') rows = rows.filter(r => String(r.is_active) === act)
  return rows
})

async function load() {
  const r = await api('/api/admin/quick-replies')
  if (r.code === 0) list.value = r.data || []
}

function search() { load() }
function resetFilter() {
  filter.q = ''
  filter.category = ''
  filter.active = ''
  load()
}

function openCreate() {
  editing.value = null
  Object.assign(form, { category: '通用', title: '', content: '', sort: 0 })
  editVisible.value = true
}

function openEdit(row) {
  editing.value = row
  Object.assign(form, { category: row.category, title: row.title, content: row.content, sort: row.sort })
  editVisible.value = true
}

async function submit() {
  if (!form.title.trim() || !form.content.trim()) {
    toast('标题和内容不能为空', 'error')
    return
  }
  saving.value = true
  try {
    const body = { category: form.category, title: form.title, content: form.content, sort: form.sort || 0 }
    const r = editing.value
      ? await api('/api/admin/quick-replies/' + editing.value.id, { method: 'PUT', body })
      : await api('/api/admin/quick-replies', { method: 'POST', body })
    if (r.code !== 0) return
    toast(editing.value ? '已更新' : '已新增', 'success')
    editVisible.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function toggleActive(row) {
  const r = await api('/api/admin/quick-replies/' + row.id, { method: 'PUT', body: { is_active: row.is_active === 1 ? 0 : 1 } })
  if (r.code !== 0) return
  toast(row.is_active === 1 ? '已停用' : '已启用', 'success')
  await load()
}

async function del(row) {
  if (!confirm('确认删除话术「' + row.title + '」？')) return
  const r = await api('/api/admin/quick-replies/' + row.id, { method: 'DELETE' })
  if (r.code !== 0) return
  toast('已删除', 'success')
  await load()
}

onMounted(load)
</script>

<style scoped>
.head-row { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; }
.filters { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
</style>
