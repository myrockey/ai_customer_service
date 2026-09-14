<template>
  <div>
    <div class="head-row">
      <div class="filters">
        <template v-if="isPlat">
          <div class="scope-tabs">
            <!-- <button class="scope-tab" :class="scope === 'platform' ? 'active' : ''" @click="switchScope('platform')">平台级（全局）</button>
            <button class="scope-tab" :class="scope === 'tenant' ? 'active' : ''" @click="switchScope('tenant')">本租户</button> -->
          </div>
        </template>
        <input v-model="filter.title" class="input" style="width:180px;" placeholder="搜索标题/内容" @keyup.enter="load()" />
        <button class="btn btn-primary btn-sm" @click="load()">搜索</button>
        <button class="btn btn-sm" @click="reset">重置</button>
      </div>
      <div style="display:flex;align-items:center;gap:10px;">
        <button class="btn btn-primary btn-sm" @click="openCreate">+ 新建 Prompt</button>
      </div>
    </div>
    <div class="grid2" style="padding:10px;"><span v-if="scope === 'platform'" class="text-3" style="font-size:12px;color:#000;">未配置自有 Prompt 的租户将使用此平台级（全局）Prompt</span></div>
    <div class="card" style="margin-top:16px;">
      <DataTable :columns="columns" :rows="list">
        <template #cell-title="{ row }">
          <b>{{ row.title }}</b>
          <span v-if="row.is_active" class="badge badge-green" style="margin-left:8px;">生效中</span>
        </template>
        <template #cell-content="{ row }">
          <span class="text-2" style="font-size:12px;">{{ (row.content || '').slice(0, 60) }}</span>
        </template>
        <template #cell-updated_at="{ row }">
          <span class="text-2" style="font-size:12px;">{{ fmt(row.updated_at) }}</span>
        </template>
        <template #cell-actions="{ row }">
          <button class="btn btn-sm" @click="openEdit(row)">编辑</button>
          <button v-if="!row.is_active" class="btn btn-sm btn-primary" @click="activate(row)">激活</button>
          <button class="btn btn-sm" @click="showVersions(row)">版本</button>
          <button class="btn btn-sm btn-danger" @click="remove(row)">删除</button>
        </template>
      </DataTable>
      <Pagination :total="total" :page="page" :size="size" @change="onPage" />
    </div>

    <!-- 新建/编辑 -->
    <Modal :visible="showModal" :title="editing ? '编辑 Prompt' : '新建 Prompt'" @close="showModal = false">
      <div class="form">
        <label class="field">
          <span class="f-label">Key（唯一标识）</span>
          <input v-model="form.key" class="input" :disabled="!!editing" placeholder="如: default" />
        </label>
        <label class="field">
          <span class="f-label">标题</span>
          <input v-model="form.title" class="input" placeholder="如: 默认客服" />
        </label>
        <label class="field">
          <span class="f-label">Prompt 内容</span>
          <textarea v-model="form.content" class="textarea" style="min-height:160px;" placeholder="系统提示词..."></textarea>
        </label>
        <label class="field" style="flex-direction:row;align-items:center;gap:8px;">
          <input v-model="form.is_active" type="checkbox" />
          <span class="f-label" style="margin:0;">保存后立即激活</span>
        </label>
      </div>
      <template #footer>
        <button class="btn" @click="showModal = false">取消</button>
        <button class="btn btn-primary" @click="save">{{ editing ? '保存' : '创建' }}</button>
      </template>
    </Modal>

    <!-- 版本 -->
    <Modal :visible="showVer" :title="'Prompt 版本 - ' + (verKey || '')" width="640px" @close="showVer = false">
      <div class="ver-list">
        <div v-for="v in versions" :key="v.id" class="ver-item">
          <div class="ver-head">
            <b>v{{ v.version }}</b>
            <span class="text-3" style="font-size:12px;">{{ fmt(v.created_at) }}</span>
            <span class="text-3" style="font-size:12px;">{{ v.remark || '' }}</span>
            <button class="btn btn-sm" style="margin-left:auto;" @click="rollback(v)">回滚</button>
          </div>
          <div class="ver-content">{{ v.content }}</div>
        </div>
        <div v-if="!versions.length" class="empty">暂无版本</div>
      </div>
      <template #footer>
        <input v-model="verRemark" class="input" style="flex:1;margin-right:8px;" placeholder="版本备注（可选）" />
        <button class="btn btn-sm" @click="createVersion">创建版本</button>
        <button class="btn" @click="showVer = false">关闭</button>
      </template>
    </Modal>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { api, toast, session } from '../api'
import DataTable from '../components/DataTable.vue'
import Modal from '../components/Modal.vue'
import Pagination from '../components/Pagination.vue'

const isPlat = session.is_platform_admin
// 平台管理员可切换：platform=平台级（全局，tenant_id=''）/ tenant=本租户；普通租户固定本租户
const scope = ref(isPlat ? 'platform' : 'tenant')
function switchScope(s) { scope.value = s; filter.title = ''; load() }
function scopeParams(extra = {}) {
  const p = { ...extra }
  if (scope.value === 'platform') p.scope = 'platform'
  return p
}

const list = ref([])
const total = ref(0)
const page = ref(1)
const size = ref(10)
const filter = reactive({ title: '' })
const showModal = ref(false)
const editing = ref(null)
const form = reactive({ key: '', title: '', content: '', is_active: false })
const showVer = ref(false)
const versions = ref([])
const verKey = ref('')
const verRemark = ref('')

const columns = [
  { key: 'title', title: '标题' },
  { key: 'content', title: '内容预览' },
  { key: 'updated_at', title: '更新时间', width: '150px' },
  { key: 'actions', title: '操作', width: '200px' }
]

function fmt(t) { return t ? String(t).replace('T', ' ').slice(0, 19) : '-' }

async function load(pg) {
  page.value = pg || 1
  try {
    const params = scopeParams({ page: page.value, page_size: size.value })
    if (filter.title.trim()) params.keyword = filter.title.trim()
    const r = await api('/api/admin/prompts', { params })
    if (r.code === 0) {
      list.value = (r.data && r.data.list) || []
      total.value = (r.data && r.data.total) || 0
    }
  } catch (e) {}
}
function onPage({ page: p, size: s }) { size.value = s; load(p) }
function reset() { filter.title = ''; load(1) }
function openCreate() {
  editing.value = null
  Object.assign(form, { key: '', title: '', content: '', is_active: false })
  showModal.value = true
}
function openEdit(p) {
  editing.value = p
  Object.assign(form, { key: p.key, title: p.title, content: p.content, is_active: !!p.is_active })
  showModal.value = true
}

async function save() {
  if (!form.key || !form.title) return toast('请填写 Key 和标题', 'warning')
  try {
    // upsert 语义：编辑与新建均走 POST /prompts（key 为主键，按 key 覆盖）
    const r = await api('/api/admin/prompts', { method: 'POST', body: { key: form.key, title: form.title, content: form.content, is_active: form.is_active }, params: scopeParams() })
    if (r.code !== 0) return toast(r.msg || '保存失败', 'error')
    toast(r.msg || '保存成功', 'success')
    showModal.value = false
    load()
  } catch (e) {}
}

async function activate(p) {
  try {
    const r = await api('/api/admin/prompts/' + encodeURIComponent(p.key) + '/activate', { method: 'POST', body: {}, params: scopeParams() })
    toast(r.msg || (r.code === 0 ? '已激活' : '激活失败'), r.code === 0 ? 'success' : 'error')
    load()
  } catch (e) {}
}

async function remove(p) {
  if (!confirm('确认删除 Prompt「' + p.title + '」？')) return
  try {
    const r = await api('/api/admin/prompts/' + encodeURIComponent(p.key), { method: 'DELETE', params: scopeParams() })
    toast(r.msg || '删除成功', r.code === 0 ? 'success' : 'error')
    load()
  } catch (e) {}
}

async function showVersions(p) {
  verKey.value = p.key
  verRemark.value = ''
  try {
    const r = await api('/api/admin/prompts/' + encodeURIComponent(p.key) + '/versions', { params: scopeParams() })
    if (r.code === 0) { versions.value = r.data || []; showVer.value = true }
  } catch (e) {}
}

async function createVersion() {
  try {
    const r = await api('/api/admin/prompts/' + encodeURIComponent(verKey.value) + '/versions', {
      method: 'POST', body: { remark: verRemark.value || '' }, params: scopeParams()
    })
    if (r.code !== 0) return toast(r.msg || '创建版本失败', 'error')
    toast('已创建 v' + r.data.version + ' 版本', 'success')
    const lr = await api('/api/admin/prompts/' + encodeURIComponent(verKey.value) + '/versions', { params: scopeParams() })
    if (lr.code === 0) versions.value = lr.data || []
  } catch (e) {}
}

async function rollback(v) {
  try {
    const r = await api('/api/admin/prompts/' + encodeURIComponent(verKey.value) + '/versions/' + v.id + '/rollback', { method: 'POST', body: {}, params: scopeParams() })
    toast(r.msg || '回滚成功', r.code === 0 ? 'success' : 'error')
    showVer.value = false
    load()
  } catch (e) {}
}

onMounted(load)
</script>

<style scoped>
.head-row { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; }
.filters { display: flex; gap: 8px; align-items: center; }
.scope-tabs { display: inline-flex; border: 1px solid var(--c-border); border-radius: var(--radius); overflow: hidden; }
.scope-tab { border: none; background: transparent; padding: 6px 12px; font-size: 12px; color: var(--c-text-2); cursor: pointer; }
.scope-tab.active { background: var(--c-primary); color: #fff; }
.form { display: flex; flex-direction: column; gap: 14px; }
.field { display: flex; flex-direction: column; gap: 6px; }
.f-label { font-size: 12px; color: var(--c-text-2); }
.ver-list { display: flex; flex-direction: column; gap: 10px; max-height: 50vh; overflow-y: auto; }
.ver-item { border: 1px solid var(--c-border); border-radius: var(--radius); padding: 10px 12px; }
.ver-head { display: flex; align-items: center; gap: 12px; margin-bottom: 6px; }
.ver-content { font-size: 12px; color: var(--c-text-2); white-space: pre-wrap; max-height: 80px; overflow-y: auto; }
.empty { text-align: center; color: var(--c-text-3); padding: 20px; }
</style>
