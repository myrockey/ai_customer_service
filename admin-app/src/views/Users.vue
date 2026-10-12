<template>
  <div>
    <div class="head-row">
      <div class="filters">
        <input v-model="filter.q" class="input" style="width:160px;" placeholder="用户名" @keyup.enter="search" />
        <select v-model="filter.role_id" class="select" style="width:130px;" @change="search">
          <option value="">全部角色</option>
          <option v-for="r in roles" :key="r.id" :value="r.id">{{ r.name }}</option>
        </select>
        <select v-model="filter.status" class="select" style="width:110px;" @change="search">
          <option value="">全部状态</option>
          <option value="1">正常</option>
          <option value="0">已禁用</option>
        </select>
        <button class="btn btn-sm btn-primary" @click="search">查询</button>
        <button class="btn btn-sm" @click="resetFilter">重置</button>
      </div>
      <button class="btn btn-primary btn-sm" v-perm="'member:add'" @click="openAdd">+ 添加成员</button>
    </div>

    <div class="card" style="margin-top:16px;">
      <DataTable :columns="columns" :rows="list">
        <template #cell-username="{ row }">
          <span class="mono">{{ row.username }}</span>
        </template>
        <template #cell-nickname="{ row }">
          <span>{{ row.nickname || '-' }}</span>
        </template>
        <template #cell-phone="{ row }">
          <span class="text-2" style="font-size:12px;">{{ row.phone || '-' }}</span>
        </template>
        <template #cell-email="{ row }">
          <span class="text-2" style="font-size:12px;">{{ row.email || '-' }}</span>
        </template>
        <template #cell-roles="{ row }">
          <template v-if="row.roles && row.roles.length">
            <span v-for="r in row.roles" :key="r.id" class="badge" :class="r.is_admin ? 'badge-blue' : 'badge-green'">{{ r.name }}</span>
          </template>
          <span v-else class="text-3">-</span>
        </template>
        <template #cell-status="{ row }">
          <span class="badge" :class="row.status === 1 ? 'badge-green' : 'badge-red'">{{ row.status === 1 ? '正常' : '已禁用' }}</span>
        </template>
        <template #cell-last_login_at="{ row }">
          <span class="text-2" style="font-size:12px;">{{ fmtTime(row.last_login_at) }}</span>
        </template>
        <template #cell-created_at="{ row }">
          <span class="text-2" style="font-size:12px;">{{ fmtTime(row.created_at) }}</span>
        </template>
        <template #cell-actions="{ row }">
          <button class="btn btn-sm" v-perm="'member:edit'" @click="openEdit(row)">编辑</button>
          <button class="btn btn-sm" v-perm="'member:edit'" @click="resetPwd(row)">重置密码</button>
          <button class="btn btn-sm" :class="row.status === 1 ? '' : 'btn-primary'" v-perm="'member:edit'" @click="toggleStatus(row)">
            {{ row.status === 1 ? '禁用' : '启用' }}
          </button>
          <button class="btn btn-sm btn-danger" v-perm="'member:remove'" @click="del(row)">删除</button>
        </template>
      </DataTable>
    </div>

    <!-- 添加成员 -->
    <Modal :visible="addVisible" title="添加成员" width="520px" @close="addVisible = false">
      <div class="form">
        <label class="field">
          <span class="f-label">用户名 <span class="tip-inline">3-32 字符，租户内唯一</span></span>
          <input v-model="addForm.username" class="input" placeholder="例如 kefu01" />
        </label>
        <label class="field">
          <span class="f-label">密码 <span class="tip-inline">至少 8 位</span></span>
          <input v-model="addForm.password" type="password" class="input" placeholder="初始登录密码" />
        </label>
        <div class="field">
          <span class="f-label">昵称 / 手机 / 邮箱 <span class="tip-inline">选填</span></span>
          <div style="display:flex;gap:8px;flex-wrap:wrap;">
            <input v-model="addForm.nickname" class="input" style="flex:1 1 120px;" placeholder="昵称/显示名" />
            <input v-model="addForm.phone" class="input" style="flex:1 1 140px;" placeholder="手机号" />
            <input v-model="addForm.email" class="input" style="flex:1 1 160px;" placeholder="邮箱" />
          </div>
        </div>
        <label class="field">
          <span class="f-label">备注 <span class="tip-inline">选填</span></span>
          <input v-model="addForm.remark" class="input" placeholder="例如：外包客服 / 晚班" />
        </label>
        <div class="field">
          <span class="f-label">分配角色 <span class="tip-inline">可多选，权限取并集</span></span>
          <div class="role-chips">
            <label v-for="r in roles" :key="r.id" class="chip" :class="{ 'chip-on': addForm.role_ids.includes(r.id) }">
              <input type="checkbox" :value="r.id" v-model="addForm.role_ids" class="chip-input" />
              <span>{{ r.name }}</span>
              <code class="text-3">{{ r.code }}</code>
            </label>
          </div>
        </div>
      </div>
      <template #footer>
        <button class="btn" @click="addVisible = false">取消</button>
        <button class="btn btn-primary" :disabled="saving" @click="submitAdd">{{ saving ? '提交中...' : '确认添加' }}</button>
      </template>
    </Modal>

    <!-- 编辑成员（用户名 + 角色） -->
    <Modal :visible="editVisible" :title="'编辑成员 · ' + (editUser && editUser.username || '')" width="520px" @close="editVisible = false">
      <div class="form">
        <label class="field">
          <span class="f-label">用户名 <span class="tip-inline">3-32 字符，租户内唯一</span></span>
          <input v-model="editForm.username" class="input" placeholder="成员登录用户名" />
        </label>
        <div class="field">
          <span class="f-label">昵称 / 手机 / 邮箱</span>
          <div style="display:flex;gap:8px;flex-wrap:wrap;">
            <input v-model="editForm.nickname" class="input" style="flex:1 1 120px;" placeholder="昵称/显示名" />
            <input v-model="editForm.phone" class="input" style="flex:1 1 140px;" placeholder="手机号" />
            <input v-model="editForm.email" class="input" style="flex:1 1 160px;" placeholder="邮箱" />
          </div>
        </div>
        <label class="field">
          <span class="f-label">备注</span>
          <input v-model="editForm.remark" class="input" placeholder="例如：外包客服 / 晚班" />
        </label>
        <div class="field">
          <span class="f-label">分配角色 <span class="tip-inline">可多选，权限取并集；保存后立即生效</span></span>
          <div class="role-chips">
            <label v-for="r in roles" :key="r.id" class="chip" :class="{ 'chip-on': editForm.role_ids.includes(r.id) }">
              <input type="checkbox" :value="r.id" v-model="editForm.role_ids" class="chip-input" />
              <span>{{ r.name }}</span>
              <code class="text-3">{{ r.code }}</code>
            </label>
          </div>
        </div>
      </div>
      <template #footer>
        <button class="btn" @click="editVisible = false">取消</button>
        <button class="btn btn-primary" :disabled="saving" @click="submitEdit">{{ saving ? '提交中...' : '保存' }}</button>
      </template>
    </Modal>

    <!-- 重置密码 -->
    <Modal :visible="pwdVisible" :title="'重置密码 · ' + (pwdUser && pwdUser.username || '')" width="420px" @close="pwdVisible = false">
      <div class="form">
        <label class="field">
          <span class="f-label">新密码 <span class="tip-inline">至少 8 位</span></span>
          <input v-model="pwdForm.password" type="password" class="input" placeholder="新的登录密码" />
        </label>
      </div>
      <template #footer>
        <button class="btn" @click="pwdVisible = false">取消</button>
        <button class="btn btn-primary" :disabled="saving" @click="submitPwd">{{ saving ? '提交中...' : '确认重置' }}</button>
      </template>
    </Modal>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { api, toast } from '../api'
import DataTable from '../components/DataTable.vue'
import Modal from '../components/Modal.vue'

const columns = [
  { key: 'username', title: '用户名', width: '150px' },
  { key: 'nickname', title: '昵称', width: '120px' },
  { key: 'phone', title: '手机号', width: '130px' },
  { key: 'email', title: '邮箱', width: '170px' },
  { key: 'roles', title: '角色', width: '200px' },
  { key: 'status', title: '状态', width: '90px' },
  { key: 'last_login_at', title: '最近登录' },
  { key: 'created_at', title: '创建时间' },
  { key: 'actions', title: '操作', width: '260px' }
]

const list = ref([])
const roles = ref([])
const addVisible = ref(false)
const editVisible = ref(false)
const pwdVisible = ref(false)
const saving = ref(false)
const addForm = ref({ username: '', password: '', role_ids: [], nickname: '', phone: '', email: '', remark: '' })
const editForm = ref({ username: '', role_ids: [], nickname: '', phone: '', email: '', remark: '' })
const editUser = ref(null)
const pwdForm = ref({ password: '' })
const pwdUser = ref(null)
const filter = reactive({ q: '', role_id: '', status: '' })

function fmtTime(t) { return t ? String(t).replace('T', ' ').slice(0, 19) : '-' }

async function load() {
  try {
    const params = new URLSearchParams()
    if (filter.q) params.set('q', filter.q)
    if (filter.role_id) params.set('role_id', filter.role_id)
    if (filter.status !== '') params.set('status', filter.status)
    const qs = params.toString()
    const r = await api('/api/admin/members' + (qs ? '?' + qs : ''))
    if (r.code !== 0) return
    list.value = r.data || []
  } catch (e) { }
}

async function loadRoles() {
  try {
    const r = await api('/api/admin/roles')
    if (r.code === 0) roles.value = r.data || []
  } catch (e) { /* 角色加载失败不阻塞 */ }
}

function search() { load() }
function resetFilter() {
  filter.q = ''
  filter.role_id = ''
  filter.status = ''
  load()
}

function openAdd() {
  addForm.value = { username: '', password: '', role_ids: [], nickname: '', phone: '', email: '', remark: '' }
  addVisible.value = true
}

async function submitAdd() {
  const f = addForm.value
  if (!f.username || !f.password) return toast('请填写用户名和密码', 'warning')
  if (f.password.length < 8) return toast('密码长度至少 8 位', 'warning')
  if (!f.role_ids.length) return toast('请至少选择一个角色', 'warning')
  if (f.phone && !/^1[3-9]\d{9}$/.test(f.phone)) return toast('手机号格式不正确', 'warning')
  if (f.email && !f.email.includes('@')) return toast('邮箱格式不正确', 'warning')
  saving.value = true
  try {
    const r = await api('/api/admin/members', { method: 'POST', body: f })
    if (r.code !== 0) return
    toast('成员已创建', 'success')
    addVisible.value = false
    load()
  } catch (e) { }
  finally { saving.value = false }
}

function openEdit(u) {
  editUser.value = u
  editForm.value = {
    username: u.username,
    role_ids: (u.roles || []).map(r => r.id),
    nickname: u.nickname || '',
    phone: u.phone || '',
    email: u.email || '',
    remark: u.remark || ''
  }
  editVisible.value = true
}

async function submitEdit() {
  const f = editForm.value
  if (!f.username) return toast('用户名不能为空', 'warning')
  if (!f.role_ids.length) return toast('请至少选择一个角色', 'warning')
  if (f.phone && !/^1[3-9]\d{9}$/.test(f.phone)) return toast('手机号格式不正确', 'warning')
  if (f.email && !f.email.includes('@')) return toast('邮箱格式不正确', 'warning')
  saving.value = true
  try {
    const r = await api('/api/admin/members/' + editUser.value.id, { method: 'PUT', body: {
      username: f.username, role_ids: f.role_ids,
      nickname: f.nickname, phone: f.phone, email: f.email, remark: f.remark
    } })
    if (r.code !== 0) return
    toast('成员已更新', 'success')
    editVisible.value = false
    load()
  } catch (e) { }
  finally { saving.value = false }
}

function resetPwd(u) {
  pwdUser.value = u
  pwdForm.value = { password: '' }
  pwdVisible.value = true
}

async function submitPwd() {
  if (!pwdForm.value.password || pwdForm.value.password.length < 8) return toast('密码长度至少 8 位', 'warning')
  saving.value = true
  try {
    const r = await api('/api/admin/members/' + pwdUser.value.id + '/password', { method: 'PUT', body: pwdForm.value })
    if (r.code !== 0) return
    toast('密码已重置', 'success')
    pwdVisible.value = false
  } catch (e) { }
  finally { saving.value = false }
}

async function toggleStatus(u) {
  const next = u.status === 1 ? 0 : 1
  try {
    const r = await api('/api/admin/members/' + u.id + '/status', { method: 'PUT', body: { status: next } })
    if (r.code !== 0) return
    toast(next === 1 ? '已启用' : '已禁用', 'success')
    load()
  } catch (e) { }
}

async function del(u) {
  if (!confirm('确定删除成员「' + u.username + '」？删除后该账号将无法登录（数据保留，可追溯）。')) return
  try {
    const r = await api('/api/admin/members/' + u.id, { method: 'DELETE' })
    if (r.code !== 0) return
    toast('成员已删除', 'success')
    load()
  } catch (e) { }
}

onMounted(() => { load(); loadRoles() })
</script>

<style scoped>
.filters { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.tip-inline { font-size: 12px; color: var(--c-text-3); }
.role-chips { display: flex; flex-wrap: wrap; gap: 8px; }
.chip {
  display: flex; align-items: center; gap: 6px; padding: 7px 12px;
  border: 1px solid var(--c-border); border-radius: var(--radius); font-size: 13px; cursor: pointer;
  background: var(--c-bg); transition: all .15s;
}
.chip:hover { border-color: var(--c-primary, #3b82f6); }
.chip-on { border-color: var(--c-primary, #3b82f6); background: rgba(59,130,246,.08); }
.chip-input { accent-color: var(--c-primary, #3b82f6); }
.chip code { font-size: 10px; }
</style>
