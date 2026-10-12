<template>
  <div>
    <div class="head-row">
      <div class="filters">
        <input v-model="filter.name" class="input" style="width:160px;" placeholder="角色名" @keyup.enter="search" />
        <button class="btn btn-sm btn-primary" @click="search">查询</button>
        <button class="btn btn-sm" @click="resetFilter">重置</button>
      </div>
      <button class="btn btn-primary btn-sm" v-perm="'role:add'" @click="openCreate">+ 新建角色</button>
    </div>

    <div class="card" style="margin-top:16px;">
      <DataTable :columns="columns" :rows="filtered">
        <template #cell-name="{ row }">
          <b>{{ row.name }}</b>
          <div class="mono text-3" style="font-size:11px;">{{ row.code }}</div>
        </template>
        <template #cell-permissions="{ row }">
          <span class="badge badge-gray">{{ row.menu_ids.length }} 项</span>
          <span v-if="row.is_admin" class="badge badge-blue">管理员</span>
        </template>
        <template #cell-description="{ row }">
          <span class="text-2">{{ row.description || '-' }}</span>
        </template>
        <template #cell-actions="{ row }">
          <button class="btn btn-sm" v-perm="'role:edit'" @click="openEdit(row)">编辑权限</button>
          <button class="btn btn-sm btn-danger" v-perm="'role:remove'" @click="del(row)">删除</button>
        </template>
      </DataTable>
    </div>

    <!-- 新建 / 编辑角色 -->
    <Modal :visible="editVisible" :title="editing ? '编辑角色 · ' + editing.name : '新建角色'" @close="editVisible = false">
      <div class="form">
        <label class="field">
          <span class="f-label">角色名</span>
          <input v-model="form.name" class="input" placeholder="例如 质检员" />
        </label>
        <label class="field">
          <span class="f-label">描述</span>
          <input v-model="form.description" class="input" placeholder="角色说明（可选）" />
        </label>
        <div class="field">
          <div class="f-head">
            <span class="f-label">菜单权限（勾选授权，按钮级权限码自动纳入）</span>
            <span class="batch-tools">
              <a class="link" @click="selectAll">全选</a>
              <span class="sep">|</span>
              <a class="link" @click="selectNone">清空</a>
              <span class="sep">|</span>
              <a class="link" @click="invertSelect">反选</a>
            </span>
          </div>
          <div class="menu-tree">
            <template v-for="root in tree" :key="root.id || root.group_name || 'r'">
              <div v-for="node in root.children" :key="node.id" class="mnode">
                <div class="mrow">
                  <label class="mcheck">
                    <input type="checkbox" :value="node.id" v-model="form.menu_ids" @change="syncParent(node)" />
                    <span>{{ node.name }}</span>
                  </label>
                  <code class="mperm">{{ node.perm || '' }}</code>
                </div>
                <div v-if="node.children && node.children.length" class="mchildren">
                  <div v-for="ch in node.children" :key="ch.id" class="mrow mchild">
                    <label class="mcheck">
                      <input type="checkbox" :value="ch.id" v-model="form.menu_ids" @change="syncParent(ch)" />
                      <span>{{ ch.name }}</span>
                    </label>
                    <code class="mperm">{{ ch.perm || '' }}</code>
                    <div v-if="ch.children && ch.children.length" class="mchildren">
                      <div v-for="gc in ch.children" :key="gc.id" class="mrow mchild">
                        <label class="mcheck">
                          <input type="checkbox" :value="gc.id" v-model="form.menu_ids" @change="syncParent(gc)" />
                          <span>{{ gc.name }}</span>
                        </label>
                        <code class="mperm">{{ gc.perm || '' }}</code>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </template>
          </div>
        </div>
      </div>
      <template #footer>
        <button class="btn" @click="editVisible = false">取消</button>
        <button class="btn btn-primary" :disabled="saving" @click="submit">{{ saving ? '提交中...' : '保存' }}</button>
      </template>
    </Modal>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { api, toast } from '../api'
import DataTable from '../components/DataTable.vue'
import Modal from '../components/Modal.vue'

const columns = [
  { key: 'name', title: '角色', width: '160px' },
  { key: 'permissions', title: '权限数', width: '140px' },
  { key: 'description', title: '描述' },
  { key: 'actions', title: '操作', width: '180px' }
]

const list = ref([])
const tree = ref([])
const editVisible = ref(false)
const saving = ref(false)
const editing = ref(null)
const form = ref({ name: '', description: '', menu_ids: [] })
const filter = reactive({ name: '' })

const filtered = computed(() => {
  return list.value.filter(r => {
    if (filter.name && !r.name.includes(filter.name)) return false
    return true
  })
})

async function load() {
  try {
    const r = await api('/api/admin/roles')
    if (r.code !== 0) return
    list.value = r.data || []
  } catch (e) { }
}

async function loadTree() {
  try {
    const r = await api('/api/admin/menus/tree')
    if (r.code === 0) tree.value = r.data || []
  } catch (e) { /* 忽略 */ }
}

function search() { /* 前端过滤，computed 自动生效 */ }
function resetFilter() {
  filter.name = ''
}

function openCreate() {
  editing.value = null
  form.value = { name: '', description: '', menu_ids: [] }
  editVisible.value = true
}

function openEdit(r) {
  editing.value = r
  form.value = { name: r.name, description: r.description, menu_ids: [...(r.menu_ids || [])] }
  editVisible.value = true
}

// 勾选子节点时确保父链被一并勾选（授权语义：拥有子权限即拥有进入父菜单的权限）
function syncParent(node) {
  const byId = {}
  for (const root of tree.value) {
    const walk = n => { byId[n.id] = n; (n.children || []).forEach(walk) }
    ;(root.children || []).forEach(walk)
  }
  let cur = node
  while (cur && cur.parent_id && byId[cur.parent_id]) {
    const p = byId[cur.parent_id]
    if (!form.value.menu_ids.includes(p.id)) form.value.menu_ids.push(p.id)
    cur = p
  }
}

// 全部菜单节点 ID（含按钮权限码），用于批量选择
const allMenuIds = computed(() => {
  const ids = []
  const walk = n => { ids.push(n.id); (n.children || []).forEach(walk) }
  for (const root of tree.value) (root.children || []).forEach(walk)
  return ids
})

// 批量操作：全选 / 清空 / 反选（父链补齐由下方 watch 兜底自动完成）
function selectAll() {
  const s = new Set(form.value.menu_ids)
  for (const id of allMenuIds.value) s.add(id)
  form.value.menu_ids = [...s]
}
function selectNone() {
  form.value.menu_ids = []
}
function invertSelect() {
  const cur = new Set(form.value.menu_ids)
  form.value.menu_ids = allMenuIds.value.filter(id => !cur.has(id))
}

// 勾选联动兜底：menu_ids 变化时自动补齐父链（任意深度；父已在则收敛，避免死循环）
watch(() => [...form.value.menu_ids], () => {
  const byId = {}
  for (const root of tree.value) {
    const walk = n => { byId[n.id] = n; (n.children || []).forEach(walk) }
    ;(root.children || []).forEach(walk)
  }
  for (const id of [...form.value.menu_ids]) {
    let cur = byId[id]
    while (cur && cur.parent_id && byId[cur.parent_id]) {
      const p = byId[cur.parent_id]
      if (!form.value.menu_ids.includes(p.id)) form.value.menu_ids.push(p.id)
      cur = p
    }
  }
})

async function submit() {
  if (!form.value.name) return toast('请填写角色名', 'warning')
  saving.value = true
  try {
    const body = { name: form.value.name, description: form.value.description, menu_ids: form.value.menu_ids }
    const path = editing.value ? '/api/admin/roles/' + editing.value.id : '/api/admin/roles'
    const method = editing.value ? 'PUT' : 'POST'
    const r = await api(path, { method, body })
    if (r.code !== 0) return
    toast('角色已保存', 'success')
    editVisible.value = false
    load()
  } catch (e) { }
  finally { saving.value = false }
}

async function del(r) {
  if (!confirm('确定删除角色「' + r.name + '」？引用该角色的成员将被解绑（失去全部权限），app_key 管理员不受影响。')) return
  try {
    const resp = await api('/api/admin/roles/' + r.id, { method: 'DELETE' })
    if (resp.code !== 0) return
    toast('角色已删除', 'success')
    load()
  } catch (e) { }
}

onMounted(() => { load(); loadTree() })
</script>

<style scoped>
.filters { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.f-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.batch-tools { display: flex; align-items: center; gap: 8px; font-size: 12px; white-space: nowrap; }
.batch-tools .sep { color: var(--c-border-strong); }
.menu-tree { max-height: 380px; overflow: auto; border: 1px solid var(--c-border); border-radius: var(--radius); padding: 8px; background: var(--c-bg); }
.mnode { margin-bottom: 6px; }
.mrow { display: flex; align-items: center; gap: 8px; padding: 4px 6px; border-radius: 6px; font-size: 12px; }
.mrow:hover { background: var(--c-hover, rgba(0,0,0,0.04)); }
.mcheck { display: flex; align-items: center; gap: 6px; cursor: pointer; }
.mperm { font-size: 10px; color: var(--c-text-3); }
.mchildren { margin-left: 24px; border-left: 1px dashed var(--c-border); padding-left: 8px; }
.mchild { font-size: 12px; }
</style>
