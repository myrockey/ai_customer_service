<template>
  <div>
    <div class="head-row">
      <span style="font-weight:600;">模型提供商</span>
      <button class="btn btn-primary btn-sm" @click="openCreate">+ 新增提供商</button>
    </div>

    <div class="card" style="margin-top:16px;">
      <DataTable :columns="columns" :rows="list">
        <template #cell-provider_name="{ row }">
          <b>{{ row.provider_name }}</b>
          <span class="badge" :class="typeClass(row.provider_type)" style="margin-left:8px;">{{ typeText(row.provider_type) }}</span>
        </template>
        <template #cell-provider_key="{ row }">
          <span class="mono">{{ row.provider_key }}</span>
        </template>
        <template #cell-default_model="{ row }">
          <span class="mono text-2" style="font-size:12px;">{{ row.default_model || '-' }}</span>
        </template>
        <template #cell-status="{ row }">
          <span class="badge" :class="row.status === 1 ? 'badge-green' : 'badge-gray'">{{ row.status === 1 ? '启用' : '停用' }}</span>
        </template>
        <template #cell-actions="{ row }">
          <button class="btn btn-sm" @click="openEdit(row)">编辑</button>
          <button class="btn btn-sm" @click="toggle(row)">{{ row.status === 1 ? '停用' : '启用' }}</button>
          <button class="btn btn-sm btn-danger" @click="remove(row)">删除</button>
        </template>
      </DataTable>
    </div>

    <Modal :visible="showModal" :title="editing ? '编辑提供商' : '新增提供商'" width="640px" @close="showModal = false">
      <div class="form">
        <label class="field">
          <span class="f-label">提供商名称</span>
          <input v-model="form.provider_name" class="input" placeholder="如: 阿里云百炼" />
        </label>
        <label class="field">
          <span class="f-label">提供商 Key（唯一标识，如 dashscope / openai）</span>
          <input v-model="form.provider_key" class="input mono" :disabled="!!editing" placeholder="如: dashscope" />
        </label>
        <label class="field">
          <span class="f-label">类型</span>
          <select v-model="form.provider_type" class="select">
            <option value="chat">对话模型</option>
            <option value="embedding">Embedding 模型</option>
            <option value="both">两者</option>
          </select>
        </label>
        <label class="field">
          <span class="f-label">默认 API Base</span>
          <input v-model="form.default_api_base" class="input mono" placeholder="https://dashscope.aliyuncs.com/compatible-mode/v1" />
        </label>
        <label class="field">
          <span class="f-label">默认模型</span>
          <input v-model="form.default_model" class="input mono" placeholder="如: qwen-max" />
        </label>
        <label class="field">
          <span class="f-label">排序权重</span>
          <input v-model.number="form.sort_order" type="number" class="input" style="width:120px;" />
        </label>
      </div>
      <template #footer>
        <button class="btn" @click="showModal = false">取消</button>
        <button class="btn btn-primary" @click="save">{{ editing ? '保存' : '创建' }}</button>
      </template>
    </Modal>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { api, toast } from '../api'
import DataTable from '../components/DataTable.vue'
import Modal from '../components/Modal.vue'

const list = ref([])
const showModal = ref(false)
const editing = ref(null)
const form = reactive({ provider_name: '', provider_key: '', provider_type: 'both', default_api_base: '', default_model: '', sort_order: 10 })

const TYPE = { chat: ['对话', 'badge-blue'], embedding: ['向量', 'badge-green'], both: ['两者', 'badge-purple'] }
const typeText = t => (TYPE[t] || [t])[0]
const typeClass = t => (TYPE[t] || [null, 'badge-gray'])[1]

const columns = [
  { key: 'provider_name', title: '提供商' },
  { key: 'provider_key', title: 'Key', width: '130px' },
  { key: 'default_api_base', title: '默认 API Base' },
  { key: 'default_model', title: '默认模型', width: '160px' },
  { key: 'sort_order', title: '排序', width: '60px' },
  { key: 'status', title: '状态', width: '80px' },
  { key: 'actions', title: '操作', width: '200px' }
]

async function load() {
  try {
    const r = await api('/api/admin/model-providers')
    if (r.code === 0) list.value = r.data || []
  } catch (e) {}
}

function openCreate() {
  editing.value = null
  Object.assign(form, { provider_name: '', provider_key: '', provider_type: 'both', default_api_base: '', default_model: '', sort_order: 10 })
  showModal.value = true
}
function openEdit(p) {
  editing.value = p
  Object.assign(form, {
    provider_name: p.provider_name, provider_key: p.provider_key, provider_type: p.provider_type || 'both',
    default_api_base: p.default_api_base || '', default_model: p.default_model || '', sort_order: p.sort_order || 10
  })
  showModal.value = true
}

async function save() {
  if (!form.provider_name || !form.provider_key) return toast('请填写名称和 Key', 'warning')
  try {
    const body = {
      provider_name: form.provider_name, default_model: form.default_model,
      default_api_base: form.default_api_base, provider_type: form.provider_type, sort_order: form.sort_order
    }
    const r = editing.value
      ? await api('/api/admin/model-providers/' + editing.value.id, { method: 'PUT', body })
      : await api('/api/admin/model-providers', { method: 'POST', body: { provider_key: form.provider_key, ...body } })
    if (r.code !== 0) return toast(r.msg || '保存失败', 'error')
    toast(r.msg || '保存成功', 'success')
    showModal.value = false
    load()
  } catch (e) {}
}

async function toggle(p) {
  try {
    const r = await api('/api/admin/model-providers/' + p.id, { method: 'PUT', body: { status: p.status === 1 ? 0 : 1 } })
    toast(r.msg || '操作成功', r.code === 0 ? 'success' : 'error')
    load()
  } catch (e) {}
}

async function remove(p) {
  if (!confirm('确认删除提供商「' + p.provider_name + '」？')) return
  try {
    const r = await api('/api/admin/model-providers/' + p.id, { method: 'DELETE' })
    toast(r.msg || '删除成功', r.code === 0 ? 'success' : 'error')
    load()
  } catch (e) {}
}

onMounted(load)
</script>

<style scoped>
.head-row { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; }
.form { display: flex; flex-direction: column; gap: 14px; }
.field { display: flex; flex-direction: column; gap: 6px; }
.f-label { font-size: 12px; color: var(--c-text-2); }
</style>
