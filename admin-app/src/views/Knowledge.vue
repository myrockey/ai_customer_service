<template>
  <div>
    <div class="head-row">
      <div class="filters">
        <select v-model="category" class="select" style="width:150px;" @change="load()">
          <option value="">全部分类</option>
          <option v-for="c in categories" :key="c.category" :value="c.category">{{ c.category }} ({{ c.count }})</option>
        </select>
        <input v-model="keyword" class="input" style="width:180px;" placeholder="搜索文档名/内容" @keyup.enter="load()" />
        <button class="btn btn-sm" @click="load()">搜索</button>
        <button class="btn btn-sm" @click="reset">重置</button>
      </div>
      <div style="display:flex;gap:8px;">
        <button v-if="progressActive && !showIndexProgress" class="btn btn-sm" @click="showIndexProgress = true">查看索引进度</button>
        <button v-if="session.is_platform_admin" class="btn btn-sm" @click="showSetting = true">上传大小设置</button>
        <button class="btn btn-primary btn-sm" @click="openUpload">上传文档</button>
      </div>
    </div>

    <div class="card" style="margin-top:16px;">
      <div class="batch-bar">
        <span class="text-2" style="font-size:12px;">已选 {{ selected.length }} 项</span>
        <button class="btn btn-sm" @click="selectAll">全选本页</button>
        <button class="btn btn-sm" @click="selectAllAll">全选全部</button>
        <template v-if="selected.length">
          <button class="btn btn-sm btn-primary" @click="startIndex">开始索引</button>
          <button class="btn btn-sm" @click="batchCategory">批量分类</button>
          <button class="btn btn-sm" @click="batchReindex">批量重索引</button>
          <button class="btn btn-sm btn-danger" @click="batchDelete">批量删除</button>
          <button class="btn btn-sm" @click="clearSelected">取消选择</button>
        </template>
      </div>
      <DataTable :columns="columns" :rows="list">
        <template #cell-select="{ row }">
          <input type="checkbox" :checked="selected.includes(row.doc_id)" @change="toggle(row.doc_id, $event)" />
        </template>
        <template #cell-title="{ row }">
          <b>{{ row.title }}</b>
          <span class="badge" :style="statusStyle(row.status)">{{ statusText(row.status) }}</span>
        </template>
        <template #cell-meta="{ row }">
          <span class="text-2 mono" style="font-size:12px;">
            分类: {{ row.category || '默认' }} · 分片: {{ row.chunk_count ?? '-' }} · {{ fileSize(row.file_size) }}
          </span>
          <div v-if="row.status === 2 && row.error_msg" class="err-msg" :title="row.error_msg">{{ row.error_msg.slice(0, 50) }}</div>
        </template>
        <template #cell-updated_at="{ row }">
          <span class="text-2" style="font-size:12px;">{{ fmt(row.updated_at || row.created_at) }}</span>
        </template>
        <template #cell-actions="{ row }">
          <button class="btn btn-sm" @click="preview(row)">预览</button>
          <button class="btn btn-sm" @click="download(row)">下载</button>
          <button class="btn btn-sm" @click="setCategory(row)">分类</button>
          <button class="btn btn-sm" @click="reindex(row)">重索引</button>
          <button class="btn btn-sm btn-danger" @click="remove(row)">删除</button>
        </template>
      </DataTable>
      <Pagination :total="total" :page="page" :size="size" @change="onPage" />
    </div>

    <!-- 上传 -->
    <Modal :visible="showUpload" title="上传知识库文档" @close="showUpload = false">
      <div class="form">
        <label class="field">
          <span class="f-label">分类</span>
          <select v-model="upForm.category" class="select">
            <option v-for="c in categories" :key="c.category" :value="c.category">{{ c.category }}</option>
            <option value="__new__">➕ 新建分类...</option>
          </select>
          <input v-if="upForm.category === '__new__'" v-model="upNewCat" class="input" placeholder="输入新分类名" style="margin-top:4px;" />
        </label>
        <div class="up-tabs">
          <button class="btn btn-sm" :class="{ 'btn-primary': upType === 'text' }" @click="upType = 'text'">文本粘贴</button>
          <button class="btn btn-sm" :class="{ 'btn-primary': upType === 'file' }" @click="upType = 'file'">文件上传</button>
        </div>
        <template v-if="upType === 'text'">
          <label class="field">
            <span class="f-label">标题</span>
            <input v-model="upForm.title" class="input" placeholder="文档标题" />
          </label>
          <label class="field">
            <span class="f-label">内容</span>
            <textarea v-model="upForm.content" class="textarea" style="min-height:150px;" placeholder="文档内容（txt/md）"></textarea>
          </label>
          <button class="btn btn-primary btn-sm" :disabled="uploading" @click="uploadText">{{ uploading ? '上传中...' : '上传' }}</button>
        </template>
        <template v-else>
          <label class="field">
            <span class="f-label">选择文件（可多选：txt/md/pdf/docx/xlsx/csv）</span>
            <input type="file" ref="fileInput" multiple accept=".txt,.md,.pdf,.docx,.xlsx,.csv,text/plain,text/markdown,application/pdf" />
          </label>
          <button class="btn btn-primary btn-sm" :disabled="uploading" @click="uploadFile">{{ uploading ? ('上传中 ' + upDone + '/' + upTotal) : '上传' }}</button>
        </template>
      </div>
    </Modal>

    <!-- 分类设置（行内/批量共用） -->
    <Modal :visible="showCatModal" :title="catMode === 'batch' ? '批量设置分类' : '设置文档分类'" @close="showCatModal = false">
      <div class="form">
        <label class="field">
          <span class="f-label">选择分类</span>
          <select v-model="catForm.value" class="select">
            <option v-for="c in categories" :key="c.category" :value="c.category">{{ c.category }}</option>
            <option value="__new__">➕ 新建分类...</option>
          </select>
          <input v-if="catForm.value === '__new__'" v-model="catForm.newCat" class="input" placeholder="输入新分类名" style="margin-top:4px;" />
        </label>
        <button class="btn btn-primary btn-sm" @click="saveCategory">保存</button>
      </div>
    </Modal>

    <!-- 上传大小设置（平台管理员） -->
    <Modal :visible="showSetting" title="知识库上传大小限制" @close="showSetting = false">
      <div class="form">
        <label class="field">
          <span class="f-label">单文件大小上限（MB，1-100）</span>
          <input v-model.number="maxFileMb" class="input" type="number" min="1" max="100" />
        </label>
        <button class="btn btn-primary btn-sm" @click="saveSetting">保存</button>
      </div>
    </Modal>

    <!-- 索引进度弹窗（手动触发后轮询展示） -->
    <Modal :visible="showIndexProgress" title="向量索引进度" @close="closeProgress">
      <div class="form">
        <div style="font-size:13px;color:var(--c-text-2);margin-bottom:10px;">
          共 {{ prog.total }} 个文档：待索引 {{ prog.pending }} · 索引中 {{ prog.indexing }} · 已索引 {{ prog.done }} · 失败 {{ prog.failed }}
        </div>
        <div style="height:10px;background:var(--c-bg);border:1px solid var(--c-border);border-radius:5px;overflow:hidden;">
          <div :style="{ height:'100%', width: progPercent + '%', background:'#2563eb', transition:'width .5s' }"></div>
        </div>
        <div style="font-size:12px;color:var(--c-text-3);margin-top:8px;">{{ progPercent }}%（已索引+失败 / 总数），{{ progressActive ? '后台轮询中，可关闭弹窗后点「查看索引进度」重新打开' : '轮询中，全部完成后自动关闭' }}</div>
        <button class="btn btn-sm" style="margin-top:12px;" @click="closeProgress">后台执行，关闭弹窗</button>
      </div>
    </Modal>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { api, toast, session } from '../api'
import DataTable from '../components/DataTable.vue'
import Modal from '../components/Modal.vue'
import Pagination from '../components/Pagination.vue'

const list = ref([])
const total = ref(0)
const page = ref(1)
const size = ref(10)
const selected = ref([])
const categories = ref([])
const category = ref('')
const keyword = ref('')
const showUpload = ref(false)
const uploading = ref(false)
const upDone = ref(0)
const upTotal = ref(0)
const upType = ref('text')
const upForm = reactive({ category: '', title: '', content: '' })
const upNewCat = ref('')
const fileInput = ref(null)
// 分类设置 Modal（行内/批量共用）
const showCatModal = ref(false)
const catMode = ref('single')   // single | batch
const catTarget = ref(null)     // single 时目标文档
const catForm = reactive({ value: '默认', newCat: '' })
// 上传大小设置
const showSetting = ref(false)
const maxFileMb = ref(10)

const STATUS = { 1: ['已索引', '#2fa84f'], 2: ['索引失败', '#e5484d'], 0: ['待索引', '#d97706'], 3: ['索引中', '#2563eb'] }
const statusText = s => (STATUS[s] || ['未知', '#8f959e'])[0]
const statusStyle = s => ({ background: (STATUS[s] || [null, '#8f959e'])[1], color: '#fff', borderRadius: 4, padding: '1px 5px', fontSize: 11, marginLeft: 8 })
const fileSize = b => b ? (b / 1024).toFixed(1) + 'KB' : '-'
const fmt = t => t ? String(t).replace('T', ' ').slice(0, 19) : '-'

const columns = [
  { key: 'select', title: '选', width: '44px' },
  { key: 'title', title: '文档' },
  { key: 'meta', title: '元信息' },
  { key: 'updated_at', title: '更新时间', width: '160px' },
  { key: 'actions', title: '操作', width: '300px' }
]

// ============ 全选 / 批量选择 ============
// 原有 toggle/clearSelected/selectAll（本页切换）见「批量操作」段

// ============ 索引进度弹窗 ============
const showIndexProgress = ref(false)
const progressActive = ref(false)   // 后台轮询是否活跃（关闭弹窗后仍可重新打开查看）
const prog = ref({ total: 0, pending: 0, indexing: 0, done: 0, failed: 0 })
const progPercent = computed(() => {
  const t = prog.value.total || 0
  if (!t) return 0
  return Math.round(((prog.value.done + prog.value.failed) / t) * 100)
})
let progressTimer = null

function resolveCategory(form) {
  // '__new__' → 用户输入的新分类名（去空格，为空回退默认）
  return (form.value === '__new__' ? (form.newCat || '').trim() : form.value) || '默认'
}

async function fetchAllDocs() {
  // 拉取当前筛选条件下全部文档（每页最大 100）用于进度统计
  const params = { page: 1, page_size: 100 }
  if (category.value) params.category = category.value
  if (keyword.value.trim()) params.keyword = keyword.value.trim()
  const all = []
  const r = await api('/api/admin/kb/documents', { params })
  if (r.code !== 0) return all
  all.push(...((r.data && r.data.list) || []))
  const totalN = (r.data && r.data.total) || 0
  const pages = Math.ceil(totalN / 100)
  for (let p = 2; p <= pages; p++) {
    const rp = await api('/api/admin/kb/documents', { params: { ...params, page: p, page_size: 100 } })
    if (rp.code === 0) all.push(...((rp.data && rp.data.list) || []))
  }
  return all
}

async function refreshProgress() {
  const all = await fetchAllDocs()
  const s = { total: all.length, pending: 0, indexing: 0, done: 0, failed: 0 }
  all.forEach(x => {
    if (x.status === 0) s.pending++
    else if (x.status === 3) s.indexing++
    else if (x.status === 1) s.done++
    else if (x.status === 2) s.failed++
  })
  prog.value = s
  // 全部处理完（无待索引/索引中）自动关闭并刷新列表
  if (s.total > 0 && s.pending === 0 && s.indexing === 0) {
    closeProgress(true)
    load()
  }
}

async function startIndex() {
  // 手动触发索引（勾选或全部待索引/失败）：选中项或（未选中时）空 body = 全部
  const body = selected.value.length ? { doc_ids: selected.value } : {}
  try {
    const r = await api('/api/admin/kb/index', { method: 'POST', body })
    if (r.code !== 0) return toast(r.msg || '提交失败', 'error')
    toast(r.msg || '已提交索引入队', 'success')
    showIndexProgress.value = true
    progressActive.value = true
    refreshProgress()
    clearInterval(progressTimer)
    progressTimer = setInterval(refreshProgress, 2000)
    load()
  } catch (e) { toast('提交失败', 'error') }
}

function closeProgress(completed = false) {
  if (completed) {
    // 全部完成：停止后台轮询
    if (progressTimer) { clearInterval(progressTimer); progressTimer = null }
    progressActive.value = false
    toast('索引完成', 'success')
  }
  // 用户点「后台执行」：仅隐藏弹窗，后台轮询继续（可随时点「查看索引进度」重开）
  showIndexProgress.value = false
  load()
}

onMounted(() => {
  load()
  loadMaxFileMb()
  window.addEventListener('beforeunload', () => { if (progressTimer) clearInterval(progressTimer) })
})

// ============ 预览 / 下载（文件为唯一数据源，全部走 nginx 静态 file_url） ============
function preview(row) {
  if (row.file_url) { window.open(row.file_url, '_blank'); return }
  toast('原文件缺失，请重新上传', 'warning')
}

function download(row) {
  if (row.file_url) {
    const a = document.createElement('a')
    a.href = row.file_url
    a.download = row.file_name || 'document'
    document.body.appendChild(a)
    a.click()
    a.remove()
    return
  }
  toast('原文件缺失，请重新上传', 'warning')
}

// ============ 上传大小限制（平台管理员） ============
async function loadMaxFileMb() {
  try {
    const r = await api('/api/admin/platform-settings')
    if (r.code === 0 && Array.isArray(r.data)) {
      const item = r.data.find(x => x.key === 'kb_max_file_mb')
      if (item && item.value) maxFileMb.value = parseInt(item.value) || 10
    }
  } catch (e) {}
}

async function saveSetting() {
  const v = Math.round(maxFileMb.value || 0)
  if (!(v >= 1 && v <= 100)) return toast('请输入 1-100 的整数（MB）', 'warning')
  try {
    const r = await api('/api/admin/platform-settings', { method: 'PUT', body: { key: 'kb_max_file_mb', value: String(v) } })
    toast(r.msg || '保存成功', r.code === 0 ? 'success' : 'error')
    if (r.code === 0) showSetting.value = false
  } catch (e) {}
}

async function load(pg) {
  page.value = pg || 1
  try {
    const params = { page: page.value, page_size: size.value }
    if (category.value) params.category = category.value
    if (keyword.value.trim()) params.keyword = keyword.value.trim()
    const r = await api('/api/admin/kb/documents', { params })
    if (r.code === 0) {
      list.value = (r.data && r.data.list) || []
      total.value = (r.data && r.data.total) || 0
    }
    const rc = await api('/api/admin/kb/categories')
    if (rc.code === 0) categories.value = rc.data || []
  } catch (e) {}
}
function onPage({ page: p, size: s }) { size.value = s; load(p) }
function reset() { category.value = ''; keyword.value = ''; load(1) }

async function setCategory(row) {
  catMode.value = 'single'
  catTarget.value = row
  catForm.value = row.category || '默认'
  catForm.newCat = ''
  showCatModal.value = true
}

async function saveCategory() {
  const c = resolveCategory(catForm)
  if (catMode.value === 'batch') {
    batchRun('category', { category: c })
    showCatModal.value = false
    return
  }
  const row = catTarget.value
  if (!row) return
  try {
    const r = await api('/api/admin/kb/documents/' + encodeURIComponent(row.doc_id) + '/category', { method: 'POST', body: { category: c } })
    toast(r.msg || '设置成功', r.code === 0 ? 'success' : 'error')
    showCatModal.value = false
    load()
  } catch (e) {}
}

async function reindex(row) {
  // 行内重索引：与批量保持一致（先确认，再走 /kb/index 入队 + 进度弹窗；worker 幂等重建）
  if (!confirm('确认重索引文档「' + row.title + '」？（会重新分片并更新向量）')) return
  try {
    const r = await api('/api/admin/kb/index', { method: 'POST', body: { doc_ids: [row.doc_id] } })
    if (r.code !== 0) return toast(r.msg || '提交失败', 'error')
    toast(r.msg || '已提交重索引', 'success')
    showIndexProgress.value = true
    progressActive.value = true
    refreshProgress()
    clearInterval(progressTimer)
    progressTimer = setInterval(refreshProgress, 2000)
    load()
  } catch (e) { toast('提交失败', 'error') }
}

async function remove(row) {
  if (!confirm('确认删除文档「' + row.title + '」？')) return
  try {
    const r = await api('/api/admin/kb/documents/' + encodeURIComponent(row.doc_id), { method: 'DELETE' })
    toast(r.msg || '删除成功', r.code === 0 ? 'success' : 'error')
    load()
  } catch (e) {}
}

// ============ 批量操作 ============
function toggle(docId, e) {
  const checked = e && e.target ? e.target.checked : e
  const i = selected.value.indexOf(docId)
  if (checked && i === -1) selected.value.push(docId)
  if (!checked && i > -1) selected.value.splice(i, 1)
}
function clearSelected() { selected.value = [] }
function selectAll() {
  if (selected.value.length === list.value.length) { clearSelected(); return }
  selected.value = list.value.map(r => r.doc_id)
}
async function selectAllAll() {
  // 全选全部（跨页）：拉取当前筛选条件下全部文档
  try {
    const params = { page: 1, page_size: 100 }
    if (category.value) params.category = category.value
    if (keyword.value.trim()) params.keyword = keyword.value.trim()
    const r = await api('/api/admin/kb/documents', { params })
    const all = (r.data && r.data.list) || []
    const totalN = (r.data && r.data.total) || 0
    const pages = Math.ceil(totalN / 100)
    for (let p = 2; p <= pages; p++) {
      const rp = await api('/api/admin/kb/documents', { params: { ...params, page: p, page_size: 100 } })
      if (rp.code === 0) all.push(...((rp.data && rp.data.list) || []))
    }
    all.forEach(x => { if (!selected.value.includes(x.doc_id)) selected.value.push(x.doc_id) })
    toast('已选 ' + all.length + ' 项', 'success')
  } catch (e) {}
}
async function batchRun(action, extra = {}) {
  const ids = selected.value.slice()
  if (!ids.length) return toast('请先勾选文档', 'warning')
  try {
    const r = await api('/api/admin/kb/batch', { method: 'POST', body: { action, doc_ids: ids, ...extra } })
    toast(r.msg || '操作完成', r.code === 0 ? 'success' : 'error')
    clearSelected()
    load()
  } catch (e) { toast('批量操作失败', 'error') }
}
function batchCategory() {
  catMode.value = 'batch'
  catTarget.value = null
  catForm.value = '默认'
  catForm.newCat = ''
  showCatModal.value = true
}
function batchReindex() {
  // 批量重索引 = 与「开始索引」同一入口（worker 幂等重建），带进度弹窗
  if (!selected.value.length) return toast('请先勾选文档', 'warning')
  if (!confirm('确认批量重索引 ' + selected.value.length + ' 个文档？（会重新分片并更新向量）')) return
  startIndex()
}
function batchDelete() {
  if (!confirm('确认批量删除 ' + selected.value.length + ' 个文档？')) return
  batchRun('delete')
}

async function openUpload() {
  // 打开上传弹窗：默认选中第一个分类（无分类则默认）
  upForm.category = categories.value.length ? categories.value[0].category : '默认'
  upNewCat.value = ''
  if (fileInput.value) fileInput.value.value = ''
  showUpload.value = true
}

async function uploadText() {
  if (!upForm.title || !upForm.content) return toast('请填写标题和内容', 'warning')
  uploading.value = true
  try {
    const r = await api('/api/admin/kb/upload', { method: 'POST', body: { title: upForm.title, content: upForm.content, category: resolveCategory(upForm) } })
    toast(r.code === 0 ? '已上传，待手动索引' : (r.msg || '上传失败'), r.code === 0 ? 'success' : 'error')
    if (r.code === 0) { showUpload.value = false; upForm.category = ''; upNewCat.value = ''; load() }
  } catch (e) {
  } finally { uploading.value = false }
}

async function uploadFile() {
  const files = fileInput.value && fileInput.value.files
  if (!files || !files.length) return toast('请选择文件', 'warning')
  const arr = Array.from(files)
  for (const f of arr) {
    if (f.size > maxFileMb.value * 1024 * 1024) {
      return toast('文件「' + f.name + '」超过大小限制（' + maxFileMb.value + ' MB）', 'warning')
    }
  }
  uploading.value = true
  upTotal.value = arr.length
  upDone.value = 0
  let ok = 0
  try {
    for (let i = 0; i < arr.length; i++) {
      const fd = new FormData()
      fd.append('file', arr[i])
      fd.append('category', resolveCategory(upForm))
      const r = await api('/api/admin/kb/upload', { method: 'POST', form: fd })
      if (r.code === 0) ok++
      upDone.value = i + 1
    }
    if (ok) toast('已上传 ' + ok + ' 个文件，待手动索引', 'success')
    showUpload.value = false
    upForm.category = ''
    upNewCat.value = ''
    if (fileInput.value) fileInput.value.value = ''
    load()
  } catch (e) {
    console.error('[kb upload] failed', e)
    toast((e && e.msg) || '上传失败', 'error')
  } finally { uploading.value = false }
}
</script>

<style scoped>
.head-row { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; }
.filters { display: flex; gap: 8px; }
.batch-bar { display: flex; align-items: center; gap: 8px; padding: 8px 12px; background: var(--c-bg); border: 1px solid var(--c-border); border-radius: var(--radius); margin-bottom: 12px; flex-wrap: wrap; }
.form { display: flex; flex-direction: column; gap: 14px; }
.field { display: flex; flex-direction: column; gap: 6px; }
.f-label { font-size: 12px; color: var(--c-text-2); }
.up-tabs { display: flex; gap: 8px; }
.err-msg { font-size: 11px; color: #e5484d; margin-top: 2px; max-width: 320px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
