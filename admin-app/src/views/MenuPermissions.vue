<template>
  <div>
    <div class="head-row">
      <div class="filters">
        <input v-model="search" class="input search-input" placeholder="搜索名称 / 路径 / 权限码" @input="checked = []" />
        <select v-model="typeFilter" class="input sel" @change="checked = []">
          <option value="">全部类型</option>
          <option :value="1">目录</option>
          <option :value="2">页面</option>
          <option :value="3">按钮</option>
        </select>
        <select v-model="groupFilter" class="input sel" @change="checked = []">
          <option value="">全部分组</option>
          <option>业务中心</option>
          <option>系统管理</option>
        </select>
        <button class="btn btn-sm btn-primary" @click="load">刷新</button>
        <button class="btn btn-sm" @click="openCreate(null)">+ 新增菜单</button>
        <button class="btn btn-sm" @click="resetBuiltin">重置内置菜单</button>
      </div>
    </div>

    <!-- 批量操作栏：勾选任意行后出现 -->
    <div v-if="checked.length" class="batch-bar">
      <span class="batch-info">已选 {{ checked.length }} 项</span>
      <button class="btn btn-sm btn-primary" @click="batchUpdate('status', 1)">批量启用</button>
      <button class="btn btn-sm" @click="batchUpdate('status', 0)">批量停用</button>
      <button class="btn btn-sm btn-primary" @click="batchUpdate('visible', 1)">批量显示</button>
      <button class="btn btn-sm" @click="batchUpdate('visible', 0)">批量隐藏</button>
      <button class="btn btn-sm" @click="checked = []">清空选择</button>
    </div>

    <div class="card" style="margin-top:16px;">
      <div v-for="g in groups" :key="g.group" class="menu-group">
        <div class="mg-title">{{ g.group }}</div>
        <div class="mrow mrow-head">
          <div class="mrow-main">
            <input type="checkbox" class="row-check" :checked="allChecked" :indeterminate="someChecked" @change="toggleAll" title="全选当前列表" />
            <span class="hh h-type">类型</span>
            <span class="hh h-name">名称</span>
            <span class="hh h-perm">权限码</span>
            <span class="hh h-path">路径</span>
            <span class="hh h-sort">排序</span>
            <span class="hh h-stat">状态</span>
            <span class="hh h-vis">可见</span>
            <span class="hh h-src">来源</span>
            <span class="hh h-acts">操作</span>
          </div>
        </div>
        <div
          v-for="node in g.nodes" :key="node.id"
          class="mrow" :class="{ 'drag-over': dragOverId === node.id, 'row-checked': checked.includes(node.id) }"
          draggable="true"
          @dragstart="dragStart(node, $event)"
          @dragover.prevent
          @dragenter="dragOverId = node.id"
          @dragleave="dragOverId = null"
          @drop="onDrop(node)"
          @dragend="dragOverId = null"
        >
          <div class="mrow-main">
            <input type="checkbox" class="row-check" :checked="checked.includes(node.id)" @change="toggleCheck(node.id)" @click.stop />
            <span class="mtype" :class="'t' + node.type">{{ typeName(node.type) }}</span>
            <span class="mname" :style="{ paddingLeft: node.depth * 22 + 'px' }" :title="node.name">{{ node.name }}</span>
            <code class="mperm">{{ node.perm || '' }}</code>
            <span class="mpath" v-if="node.path">{{ node.path }}</span>
            <label class="msort" title="直接修改排序，回车或失焦保存">
              <input type="number" class="sort-input" :value="node.sort" @change="onSortChange(node, $event)" @keydown.enter="onSortChange(node, $event)" />
            </label>
            <span class="mstat" :class="node.status === 1 ? 'ok' : 'off'">{{ node.status === 1 ? '启用' : '停用' }}</span>
            <span class="mstat" :class="node.visible === 1 ? 'ok' : 'off'">{{ node.visible === 1 ? '显示' : '隐藏' }}</span>
            <span class="mbadge" v-if="node.is_builtin === 1" :title="'内置菜单'">内置</span>
            <span class="mbadge mbadge-custom" v-else :title="'自定义菜单'">自定义</span>
            <div class="macts">
              <button class="btn btn-sm" @click="openCreate(node)">新增子项</button>
              <button class="btn btn-sm" @click="openEdit(node)">编辑</button>
              <button class="btn btn-sm btn-danger" @click="del(node)">删除</button>
            </div>
          </div>
        </div>
      </div>
      <div v-if="!groups.length" class="empty-tip">没有匹配的菜单，换个筛选条件试试</div>
    </div>

    <!-- 新增 / 编辑菜单 -->
    <Modal :visible="editVisible" :title="editing ? '编辑菜单 · ' + editing.name : '新增菜单'" width="620px" @close="editVisible = false">
      <div class="form">
        <!-- 基础信息 -->
        <div class="form-row2">
          <label class="field">
            <span class="f-label">菜单名</span>
            <input v-model="form.name" class="input" placeholder="例如 数据报表" />
          </label>
          <label class="field">
            <span class="f-label">类型</span>
            <select v-model="form.type" class="input">
              <option :value="1">目录</option>
              <option :value="2">页面</option>
              <option :value="3">按钮</option>
            </select>
          </label>
        </div>
        <div class="type-hint">
          <b v-if="form.type === 1">目录</b><b v-else-if="form.type === 2">页面</b><b v-else>按钮</b>
          <span v-if="form.type === 1">：用于分组导航，不产生可访问页面</span>
          <span v-else-if="form.type === 2">：可跳转的路由页面，需配置路由路径与组件</span>
          <span v-else>：仅作为权限码挂载点，不生成菜单项</span>
        </div>

        <!-- 归属 -->
        <div class="form-row2">
          <label class="field">
            <span class="f-label">分组</span>
            <select v-model="form.group_name" class="input">
              <option>业务中心</option>
              <option>系统管理</option>
            </select>
          </label>
          <label class="field">
            <span class="f-label">父节点</span>
            <select v-model="form.parent_id" class="input">
              <option :value="0">顶层</option>
              <option v-for="n in parentOptions" :key="n.id" :value="n.id">{{ n.group_name }} / {{ n.name }}</option>
            </select>
          </label>
        </div>

        <!-- 路由信息：仅页面类型 -->
        <div v-if="form.type === 2" class="form-row2">
          <label class="field">
            <span class="f-label">路由路径</span>
            <input v-model="form.path" class="input" placeholder="/report" />
          </label>
          <label class="field">
            <span class="f-label">组件</span>
            <select v-model="form.component" class="input">
              <option v-if="form.component && !viewOptions.includes(form.component)" :value="form.component">{{ form.component }}（已失效）</option>
              <option v-for="v in viewOptions" :key="v" :value="v">{{ v }}</option>
            </select>
          </label>
        </div>

        <!-- 权限码：页面 / 按钮 -->
        <div v-if="form.type !== 1" class="form-row2">
          <label class="field">
            <span class="f-label">权限码</span>
            <input v-model="form.perm" class="input" placeholder="report:view（全局唯一）" />
          </label>
          <label class="field">
            <span class="f-label">排序</span>
            <input v-model.number="form.sort" type="number" class="input" />
          </label>
        </div>
        <div v-else class="form-row2">
          <label class="field">
            <span class="f-label">排序</span>
            <input v-model.number="form.sort" type="number" class="input" />
          </label>
          <div class="field" aria-hidden="true"></div>
        </div>

        <!-- 状态 -->
        <div class="form-row2">
          <label class="field">
            <span class="f-label">状态</span>
            <select v-model.number="form.status" class="input">
              <option :value="1">启用</option>
              <option :value="0">停用 · 隐藏并拒绝接口</option>
            </select>
          </label>
          <label class="field">
            <span class="f-label">可见性</span>
            <select v-model.number="form.visible" class="input">
              <option :value="1">显示</option>
              <option :value="0">隐藏 · 保留路由</option>
            </select>
          </label>
        </div>

        <div class="form-tip">
          <span class="tip-dot">i</span>
          <span>权限码全局唯一；「系统管理」组仅平台管理员（app_key 登录）可见。删除内置菜单前请确认相关接口已停用，重置内置菜单可恢复初始状态。</span>
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
import { ref, computed, onMounted } from 'vue'
import { api, toast, loadMenus } from '../api'
import { registerDynamicRoutes } from '../router'
import Modal from '../components/Modal.vue'
import { viewOptions } from '../viewRegistry' // 组件下拉候选：与 router.js 动态路由同一套枚举

const allMenus = ref([])
const editVisible = ref(false)
const saving = ref(false)
const editing = ref(null)
const form = ref({ name: '', type: 2, group_name: '业务中心', parent_id: 0, path: '', component: '', perm: '', sort: 0, visible: 1, status: 1 })

// 搜索 / 筛选
const search = ref('')
const typeFilter = ref('')
const groupFilter = ref('')

// 批量选择（数组 + includes，保持响应式简单可靠）
const checked = ref([])
function toggleCheck(id) {
  const i = checked.value.indexOf(id)
  if (i >= 0) checked.value = checked.value.filter(x => x !== id)
  else checked.value = [...checked.value, id]
}

// 表头全选：作用于当前可见（搜索/筛选后）的所有节点；半选态 indeterminate
const visibleIds = computed(() => groups.value.flatMap(g => g.nodes.map(n => n.id)))
const allChecked = computed(() => visibleIds.value.length > 0 && visibleIds.value.every(id => checked.value.includes(id)))
const someChecked = computed(() => visibleIds.value.some(id => checked.value.includes(id)) && !allChecked.value)
function toggleAll(e) {
  if (e.target.checked) checked.value = [...new Set([...checked.value, ...visibleIds.value])]
  else checked.value = checked.value.filter(id => !visibleIds.value.includes(id))
}

// 拖拽状态
const dragNodeId = ref(null)
const dragOverId = ref(null)

const typeName = t => ({ 1: '目录', 2: '页面', 3: '按钮' }[t] || '页面')

// 父节点候选：按钮（type=3）不可作为父级，编辑时排除自身（v-for 与 v-if 不能用于同一元素，过滤放 computed）
const parentOptions = computed(() => {
  const editingId = editing.value && editing.value.id
  return allMenus.value.filter(m => m.type !== 3 && m.id !== editingId)
})

// 关键字匹配：名称 / 路径 / 权限码
function hitKeyword(m) {
  const kw = search.value.trim().toLowerCase()
  if (!kw) return true
  return [m.name, m.path || '', m.perm || ''].some(t => t.toLowerCase().includes(kw))
}

// 扁平列表组树：按 group_name 分组，递归收集任意深度节点（目录→页面→按钮）。
// 支持搜索/筛选：命中节点保留其祖先链（祖先不命中也会显示，保证树形完整），类型筛选作用于叶子命中。
const groups = computed(() => {
  const byGroup = {}
  for (const m of allMenus.value) {
    if (!byGroup[m.group_name]) byGroup[m.group_name] = []
    byGroup[m.group_name].push(m)
  }
  const res = []
  for (const g of Object.keys(byGroup)) {
    if (groupFilter.value && g !== groupFilter.value) continue
    const items = byGroup[g]
    // 收集命中节点集合（含祖先链）
    const hitIds = new Set()
    const kwActive = search.value.trim() !== ''
    if (kwActive || typeFilter.value !== '') {
      // 收集命中节点（含父链上每一级节点，保证树形完整）：命中节点自身已加入，这里逐级加入其父链
      const collectAncestors = (id, out) => {
        if (id === 0) return
        const p = items.find(x => x.id === id)
        if (p) {
          out.add(id)
          collectAncestors(p.parent_id, out)
        }
      }
      for (const m of items) {
        const kwHit = hitKeyword(m)
        const typeHit = typeFilter.value === '' || m.type === Number(typeFilter.value)
        if (kwHit && typeHit) {
          hitIds.add(m.id)
          collectAncestors(m.parent_id, hitIds)
        }
      }
      if (!hitIds.size) continue
    }
    const rows = []
    const visit = (m, depth) => {
      if (hitIds.size && !hitIds.has(m.id)) return
      rows.push({ ...m, depth })
      const kids = items.filter(x => x.parent_id === m.id).sort((a, b) => a.sort - b.sort)
      for (const k of kids) visit(k, depth + 1)
    }
    const tops = items.filter(m => m.parent_id === 0).sort((a, b) => a.sort - b.sort)
    for (const t of tops) visit(t, 0)
    if (rows.length) res.push({ group: g, nodes: rows })
  }
  return res
})

async function load() {
  try {
    const r = await api('/api/admin/menus')
    if (r.code === 0) allMenus.value = r.data || []
  } catch (e) { }
}

// 菜单变更后刷新当前登录用户的菜单缓存 + 动态路由（无需退出重新登录，侧边栏/路由立即生效）
async function refreshMenus() {
  try {
    const menus = await loadMenus()
    if (menus && menus.length) {
      registerDynamicRoutes()
      toast('菜单缓存已刷新', 'success')
    }
  } catch (e) { /* loadMenus 静默失败保留旧缓存 */ }
}

// 刷新按钮：重拉菜单列表 + 刷新当前用户菜单缓存
async function refreshAll() {
  await load()
  await refreshMenus()
}

// 行内排序：直接输入数字，回车 / 失焦保存
async function onSortChange(node, e) {
  const val = Number(e.target.value)
  if (!isFinite(val) || val === node.sort) return
  try {
    const r = await api('/api/admin/menus/' + node.id, { method: 'PUT', body: { sort: val } })
    if (r.code !== 0) return
    toast('排序已更新', 'success')
    load()
    refreshMenus()
  } catch (err) { }
}

// 批量更新：status / visible
async function batchUpdate(field, value) {
  if (!checked.value.length) return
  try {
    const r = await api('/api/admin/menus/batch-update', { method: 'POST', body: { ids: checked.value, [field]: value } })
    if (r.code !== 0) return
    toast(r.msg || '批量更新成功', 'success')
    checked.value = []
    load()
    refreshMenus()
  } catch (e) { }
}

// ============ 拖拽移动归属 ============
function dragStart(node, e) {
  dragNodeId.value = node.id
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', String(node.id))
  }
}

// target 是否在 ancestorId 的子孙链中（防循环挂载）
function isDescendant(targetId, ancestorId) {
  const stack = [ancestorId]
  while (stack.length) {
    const cur = stack.pop()
    for (const m of allMenus.value) {
      if (m.parent_id === cur) {
        if (m.id === targetId) return true
        stack.push(m.id)
      }
    }
  }
  return false
}

// 取某父节点下当前最大 sort + 1（拖入默认追加到末尾）
function nextSort(parentId) {
  const sibs = allMenus.value.filter(m => m.parent_id === parentId)
  if (!sibs.length) return 1
  return Math.max(...sibs.map(m => m.sort)) + 1
}

async function onDrop(target) {
  const dragId = dragNodeId.value
  dragOverId.value = null
  dragNodeId.value = null
  if (!dragId || dragId === target.id) return
  // 不能拖到自己的子孙下
  if (isDescendant(target.id, dragId)) return toast('不能拖到自己的子菜单下', 'warning')
  // 目标为目录 → 挂入目录；目标为页面/按钮 → 挂到其父级下（追加末尾）
  const parentId = target.type === 1 ? target.id : target.parent_id
  if (parentId === dragId) return
  const old = allMenus.value.find(m => m.id === dragId)
  if (!old) return
  if (old.parent_id === parentId && old.type === target.type && old.id !== target.id) {
    // 同目录内拖动：仅调整排序（排到目标之前，目标及之后顺延）
    const sibs = allMenus.value.filter(m => m.parent_id === parentId && m.id !== dragId).sort((a, b) => a.sort - b.sort)
    const tIdx = sibs.findIndex(m => m.id === target.id)
    const newSort = tIdx < 0 ? nextSort(parentId) : (tIdx === 0 ? 1 : sibs[tIdx - 1].sort + 1)
    try {
      const r = await api('/api/admin/menus/' + dragId, { method: 'PUT', body: { parent_id: parentId, sort: newSort } })
      if (r.code === 0) { toast('排序已调整', 'success'); load() }
    } catch (e) { }
    return
  }
  const sort = nextSort(parentId)
  try {
    const r = await api('/api/admin/menus/' + dragId, { method: 'PUT', body: { parent_id: parentId, sort } })
    if (r.code !== 0) return
    toast('菜单已移动至「' + (parentId === 0 ? '顶层' : (allMenus.value.find(m => m.id === parentId) || {}).name || '') + '」', 'success')
    load()
  } catch (e) { }
}

function openCreate(parent) {
  editing.value = null
  form.value = {
    name: '', type: parent ? 2 : 2, group_name: parent ? parent.group_name : '业务中心',
    parent_id: parent ? parent.id : 0, path: '', component: '', perm: '', sort: 0, visible: 1, status: 1
  }
  editVisible.value = true
}

function openEdit(m) {
  editing.value = m
  form.value = {
    name: m.name, type: m.type, group_name: m.group_name, parent_id: m.parent_id,
    path: m.path || '', component: m.component || '', perm: m.perm || '', sort: m.sort,
    visible: m.visible, status: m.status
  }
  editVisible.value = true
}

async function submit() {
  if (!form.value.name) return toast('请填写菜单名', 'warning')
  saving.value = true
  try {
    const body = { ...form.value }
    // 排序输入被清空时 v-model.number 会得到空串，转数字兜底，避免后端 JSON 绑定 int 失败
    body.sort = Number(body.sort) || 0
    body.parent_id = Number(body.parent_id) || 0
    // 按类型清洗字段：目录/按钮不携带路由信息，目录不携带权限码
    if (body.type === 1) { body.path = ''; body.component = ''; body.perm = '' }
    else if (body.type === 3) { body.path = ''; body.component = '' }
    const path = editing.value ? '/api/admin/menus/' + editing.value.id : '/api/admin/menus'
    const method = editing.value ? 'PUT' : 'POST'
    const r = await api(path, { method, body })
    if (r.code !== 0) return
    toast('菜单已保存', 'success')
    editVisible.value = false
    load()
    refreshMenus()
  } catch (e) { }
  finally { saving.value = false }
}

async function del(m) {
  let msg = '确定删除菜单「' + m.name + '」？其子菜单与角色授权关联将一并删除。'
  if (m.is_builtin === 1) msg += '\n该菜单为内置菜单，删除后相关页面将不可访问，可通过「重置内置菜单」恢复。'
  if (!confirm(msg)) return
  try {
    const r = await api('/api/admin/menus/' + m.id, { method: 'DELETE' })
    if (r.code !== 0) return
    const perms = (r.data && r.data.removed_perms) || []
    if (perms.length) toast('菜单已删除，涉及的权限码：' + perms.join(', '), 'warning')
    else toast('菜单已删除', 'success')
    load()
    refreshMenus()
  } catch (e) { }
}

async function resetBuiltin() {
  if (!confirm('重置内置菜单将恢复全部内置菜单节点（新增的自定义菜单保留），确定继续？')) return
  try {
    const r = await api('/api/admin/menus/reset-builtin', { method: 'POST' })
    if (r.code !== 0) return
    toast('内置菜单已重置为初始状态', 'success')
    load()
    refreshMenus()
  } catch (e) { }
}

onMounted(load)
</script>

<style scoped>
.filters { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.search-input { width: 220px; }
.sel { width: 110px; }
.batch-bar {
  display: flex; align-items: center; gap: 8px; margin-top: 12px; padding: 8px 12px;
  background: var(--c-primary-weak, rgba(59,130,246,.08)); border: 1px solid var(--c-primary, #3b82f6);
  border-radius: var(--radius); flex-wrap: wrap;
}
.batch-info { font-size: 13px; font-weight: 600; color: var(--c-primary, #3b82f6); margin-right: 4px; }
.menu-group { margin-bottom: 14px; }
.card { overflow-x: auto; }
.mg-title { font-size: 12px; font-weight: 600; color: var(--c-text-2); padding: 6px 0; border-bottom: 1px solid var(--c-border); }
.mrow-head { background: var(--c-bg-2, #f6f7f9); cursor: default; border-bottom: 1px solid var(--c-border); }
.mrow-head:hover { background: var(--c-bg-2, #f6f7f9); }
.mrow-head .mrow-main { font-size: 11px; font-weight: 600; color: var(--c-text-2); }
.hh { flex-shrink: 0; }
.h-type { width: 40px; text-align: center; }
.h-name { flex: 1; min-width: 80px; }
.h-perm { width: 128px; }
.h-path { width: 112px; }
.h-sort { width: 56px; text-align: center; }
.h-stat { width: 42px; }
.h-vis { width: 42px; }
.h-src { width: 52px; text-align: center; }
.h-acts { width: 186px; }
.mrow { border-bottom: 1px solid var(--c-border); border-radius: 6px; cursor: grab; transition: background .12s; }
.mrow:hover { background: var(--c-hover); }
.mrow.drag-over { background: var(--c-primary-weak, rgba(59,130,246,.15)); outline: 1px dashed var(--c-primary, #3b82f6); outline-offset: -1px; }
.mrow.row-checked { background: var(--c-primary-weak, rgba(59,130,246,.08)); }
.mrow-main { display: flex; align-items: center; gap: 10px; padding: 7px 4px; font-size: 12px; min-width: 900px; }
.row-check { flex-shrink: 0; width: 14px; height: 14px; cursor: pointer; accent-color: var(--c-primary, #3b82f6); }
.mtype { padding: 1px 0; border-radius: 10px; font-size: 10px; background: var(--c-bg-2, #f2f3f5); color: var(--c-text-2); line-height: 16px; flex-shrink: 0; width: 40px; text-align: center; box-sizing: border-box; }
.mtype.t1 { background: rgba(139,200,234,0.2); color: #3a7ca5; }
.mtype.t2 { background: rgba(158,172,234,0.2); color: #5a66b5; }
.mtype.t3 { background: rgba(226,232,240,0.6); color: #718096; }
.mname { flex: 1; min-width: 80px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 600; }
.mperm { flex-shrink: 0; width: 128px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 10px; color: var(--c-text-3); font-family: monospace; background: var(--c-bg-2, #f6f7f9); padding: 1px 5px; border-radius: 4px; box-sizing: border-box; }
.mpath { flex-shrink: 0; width: 112px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 10px; color: var(--c-text-3); font-family: monospace; }
.msort { flex-shrink: 0; width: 56px; display: inline-flex; align-items: center; justify-content: center; }
.sort-input {
  width: 52px; padding: 3px 6px; font-size: 12px; text-align: center;
  border: 1px solid var(--c-border); border-radius: 5px;
  background: var(--c-input-bg, var(--c-card)); color: var(--c-text);
  outline: none; transition: border-color .12s; box-sizing: border-box;
}
.sort-input:focus { border-color: var(--c-primary, #3b82f6); }
.sort-input:hover { border-color: var(--c-primary, #3b82f6); }
.mstat { flex-shrink: 0; width: 42px; text-align: center; font-size: 10px; }
.mstat.ok { color: #52C41A; }
.mstat.off { color: #EA6668; }
.mbadge { flex-shrink: 0; width: 52px; text-align: center; font-size: 10px; background: rgba(250,173,20,0.15); color: #b8860b; padding: 1px 0; border-radius: 4px; box-sizing: border-box; }
.mbadge-custom { background: rgba(59,130,246,.10); color: #3b82f6; }
.macts { flex-shrink: 0; width: 186px; margin-left: auto; display: flex; gap: 6px; justify-content: flex-end; }
.empty-tip { padding: 30px 0; text-align: center; color: var(--c-text-3); font-size: 13px; }

/* 编辑弹窗表单 */
.form { display: flex; flex-direction: column; gap: 14px; }
.form-row2 { display: flex; gap: 14px; }
.form-row2 .field { flex: 1; min-width: 0; }
.field { display: flex; flex-direction: column; gap: 6px; }
.f-label { font-size: 13px; color: var(--c-text-2); font-weight: 500; }
.type-hint {
  font-size: 12px; color: var(--c-text-3); background: var(--c-bg-2, #f6f7f9);
  border-radius: 8px; padding: 8px 12px; line-height: 1.6;
}
.type-hint b { color: var(--c-primary); font-weight: 600; }
.form-tip {
  display: flex; align-items: flex-start; gap: 8px;
  font-size: 12px; color: var(--c-text-3); line-height: 1.7;
  background: var(--c-primary-weak, rgba(59,130,246,.06)); border: 1px solid var(--c-border);
  border-radius: 8px; padding: 9px 12px;
}
.tip-dot {
  flex-shrink: 0; width: 16px; height: 16px; margin-top: 1px; border-radius: 50%;
  background: var(--c-primary); color: #fff; font-size: 11px; font-weight: 700;
  display: inline-flex; align-items: center; justify-content: center;
}
</style>
