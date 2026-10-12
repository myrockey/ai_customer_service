<template>
  <div class="tickets">
    <div class="tk-layout">
      <!-- 工单列表 -->
      <div class="card tk-list">
        <div class="tk-toolbar">
          <span class="text-3" style="font-size:12px;">状态</span>
          <select v-model="status" class="select" style="width:100px;" @change="load">
            <option value="-1">全部</option>
            <option value="0">待处理</option>
            <option value="1">接管中</option>
            <option value="2">已取消</option>
            <option value="3">已关闭</option>
          </select>
          <span class="text-3" style="font-size:12px;margin-left:10px;">优先级</span>
          <select v-model="priority" class="select" style="width:96px;" @change="load">
            <option value="-1">全部</option>
            <option value="0">低</option>
            <option value="1">中</option>
            <option value="2">高</option>
            <option value="3">紧急</option>
          </select>
          <span class="text-3" style="font-size:12px;margin-left:10px;">范围</span>
          <select v-model="scope" class="select" style="width:100px;" @change="load">
            <option value="all">全部</option>
            <option value="mine">我的工单</option>
          </select>
          <input v-model="keyword" class="input" style="width:150px;height:26px;margin-left:10px;"
                 placeholder="工单号/用户ID/接管人" @keyup.enter="load" @input="onKeywordInput" />
          <button class="btn btn-sm" @click="load">查询</button>
          <button class="btn btn-sm" @click="resetFilters">重置</button>
          <span class="text-3" style="font-size:12px;">共 {{ total }} 条</span>
          <span style="flex:1;"></span>
          <!-- 客服在线状态条 -->
          <span class="text-3" style="font-size:12px;">在线客服:</span>
          <span class="badge badge-green" style="margin-right:8px;">{{ onlineCount }}</span>
          <template v-if="myAgent">
            <span class="text-3" style="font-size:12px;">我的状态:</span>
            <select v-model="myStatus" class="select" style="width:82px;" @change="setMyStatus">
              <option :value="1">在线</option>
              <option :value="2">忙碌</option>
              <option :value="0">离线</option>
            </select>
            <span class="text-3" style="font-size:12px;margin-left:6px;">处理 {{ myAgent.handling_count }}/{{ myAgent.max_concurrent }}</span>
          </template>
          <span v-else-if="session.user_id" class="text-3" style="font-size:12px;color:#999;">（未接入在线状态）</span>
        </div>
        <div class="tk-items">
          <div v-if="!list.length" class="empty">暂无工单</div>
          <div
            v-for="t in list" :key="t.id" class="tk-item"
            :class="{ active: current && current.id === t.id }"
            @click="open(t)"
          >
            <div class="tk-item-row">
              <b class="mono">{{ t.ticket_no }}</b>
              <span v-if="t.unread_count > 0" class="badge badge-red" style="border-radius:10px;padding:0 6px;">{{ t.unread_count }}</span>
              <span class="badge" :class="statusClass(t.status)">{{ statusText(t.status) }}</span>
            </div>
            <div class="tk-item-row text-2 mono" style="font-size:12px;">
              <span class="badge" :style="priorityStyle(t.priority)">{{ priorityText(t.priority) }}</span>
              用户: {{ t.user_id }}
              <span v-if="t.handler_name" class="text-3" style="margin-left:6px;">接管: {{ t.handler_name }}</span>
            </div>
            <div class="tk-item-row text-2" style="font-size:12px;">{{ t.reason || '-' }}</div>
            <div class="tk-item-row text-3" style="font-size:11px;">{{ fmtTime(t.created_at) }}
              <span v-if="t.handler_uid === session.user_id" class="badge badge-green" style="margin-left:6px;">我的</span>
            </div>
          </div>
        </div>
      </div>

      <!-- 会话详情 -->
      <div class="card tk-detail">
        <template v-if="current">
          <div class="tk-detail-head">
            <div>
              <b class="mono">{{ current.ticket_no }}</b>
              <span class="badge" :class="statusClass(current.status)" style="margin-left:8px;">{{ statusText(current.status) }}</span>
            </div>
            <div class="text-3" style="font-size:12px;">用户: <span class="mono">{{ current.user_id }}</span>
              <span v-if="current.handler_name" style="margin-left:10px;">接管客服: <span class="mono">{{ current.handler_name }}</span></span>
            </div>
          </div>

          <div class="msg-list" ref="msgBox">
            <div v-for="(m, i) in messages" :key="i" class="msg" :class="'msg-' + m.role">
              <span class="msg-role" :class="roleClass(m.role)">{{ roleText(m.role) }}</span>
              <div class="msg-content">{{ m.content }}</div>
              <div class="msg-time text-3">{{ fmtTime(m.created_at) }}</div>
            </div>
            <div v-if="!messages.length" class="empty">暂无消息</div>
          </div>

          <div class="tk-actions">
            <template v-if="current.status === 0">
              <template v-if="isPlat || isAdmin">
                <span class="text-2" style="font-size:12px;margin-right:4px;">指派给:</span>
                <select v-model="transferTo" class="select" style="width:auto;">
                  <option :value="0">选择客服...</option>
                  <option v-for="m in members" :key="m.id" :value="m.id">{{ m.nickname || m.username }}</option>
                </select>
                <button class="btn btn-sm btn-primary" :disabled="!transferTo" @click="transferTicket">指派</button>
              </template>
              <button class="btn btn-primary btn-sm" @click="takeover">接管对话</button>
            </template>
            <template v-if="current.status === 1">
              <div v-if="canHandle()">
                <textarea v-model="replyText" class="textarea" style="min-height:60px;" placeholder="回复用户消息..."></textarea>
                <div style="display:flex;gap:8px;margin-top:8px;flex-wrap:wrap;align-items:center;">
                  <button class="btn btn-primary btn-sm" :disabled="!replyText.trim()" @click="reply">发送回复</button>
                  <button class="btn btn-sm" @click="toggleQuickReplies">📋 话术</button>
                  <button class="btn btn-sm" @click="closeTicket">关闭工单</button>
                  <select v-model="transferTo" class="select" style="width:auto;">
                    <option :value="0">转交给客服...</option>
                    <option v-for="m in members" :key="m.id" :value="m.id">{{ m.nickname || m.username }}</option>
                  </select>
                  <button class="btn btn-sm" :disabled="!transferTo" @click="transferTicket">转交</button>
                  <select v-model="newPriority" class="select" style="width:auto;margin-left:auto;" @change="setPriority">
                    <option :value="0">低优先级</option>
                    <option :value="1">中优先级</option>
                    <option :value="2">高优先级</option>
                    <option :value="3">紧急</option>
                  </select>
                </div>
                <!-- 快捷话术面板 -->
                <div v-if="showQuickReplies" class="card" style="margin-top:8px;padding:10px;max-height:260px;overflow-y:auto;">
                  <div v-if="!quickReplies.length" class="text-2" style="font-size:12px;padding:6px 0;">暂无话术，可在「快捷回复」菜单中维护</div>
                  <div v-for="qr in quickReplies" :key="qr.id" class="qr-item" style="padding:6px 8px;border-bottom:1px solid rgba(0,0,0,0.06);cursor:pointer;" @click="insertQuickReply(qr)">
                    <span class="badge badge-blue" style="margin-right:6px;">{{ qr.category }}</span>
                    <b style="font-size:12px;">{{ qr.title }}</b>
                    <div class="text-2" style="font-size:11px;margin-top:2px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:520px;">{{ qr.content }}</div>
                  </div>
                </div>
              </div>
              <div v-else class="text-2" style="font-size:13px;padding:8px 0;">
                该工单由 <b>{{ current.handler_name || '-' }}</b> 接管中，仅接管人可回复/关闭。
              </div>
            </template>
            <template v-if="current.status === 3">
              <button class="btn btn-sm" @click="showLogs = true; loadLogs()" style="margin-left:auto;">查看操作日志</button>
            </template>
            <button class="btn btn-sm" @click="toggleLogs" style="margin-left:auto;">{{ showLogs ? '隐藏日志' : '操作日志' }}</button>
          </div>

          <div v-if="showLogs" class="logs">
            <div v-for="l in logs" :key="l.id" class="log-item text-2" style="font-size:12px;">
              [{{ fmtTime(l.created_at) }}] {{ l.action }} · {{ l.operator || '-' }} · {{ l.remark || '-' }}
            </div>
            <div v-if="!logs.length" class="empty">暂无日志</div>
          </div>
        </template>
        <div v-else class="empty" style="margin-top:40vh;">选择左侧工单查看会话</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { api, toast, session } from '../api'

const status = ref('-1')
const priority = ref('-1')
const keyword = ref('')
const list = ref([])
const total = ref(0)
const current = ref(null)
const messages = ref([])
const logs = ref([])
const replyText = ref('')
const newPriority = ref(1)
const showLogs = ref(false)
const msgBox = ref(null)

const STATUS = { 0: ['待处理', 'badge-orange'], 1: ['接管中', 'badge-blue'], 2: ['已取消', 'badge-gray'], 3: ['已关闭', 'badge-green'] }
const PRIORITY = { 0: ['低', '#8c8c8c'], 1: ['中', '#2563eb'], 2: ['高', '#d97706'], 3: ['紧急', '#e5484d'] }
const statusText = s => (STATUS[s] || [s])[0]
const statusClass = s => (STATUS[s] || [s, 'badge-gray'])[1]
const priorityText = p => (PRIORITY[p] || ['中', '#2563eb'])[0]
const priorityStyle = p => ({ background: (PRIORITY[p] || [null, '#2563eb'])[1], color: '#fff', borderRadius: 4, padding: '1px 5px', fontSize: 11 })
const roleText = r => ({ user: '用户', ai: 'AI', human: '人工', system: '系统' }[r] || r)
const roleClass = r => ({ user: 'role-user', ai: 'role-ai', human: 'role-human', system: 'role-sys' }[r] || '')

// 接管权限：app_key 管理员 / 租户管理员（后端登录时判定 is_admin，前端不维护权限清单）/ 接管人本人 可回复、关闭、转交
const isPlat = session.is_platform_admin
const isAdmin = session.is_admin
const members = ref([])
const transferTo = ref(0)
const quickReplies = ref([])
const showQuickReplies = ref(false)

// 在线状态：scope 分组 / 我的状态 / 在线客服数
const scope = ref('all')
const agentStatusList = ref([])
const myStatus = ref(1)
const myAgent = computed(() => agentStatusList.value.find(a => a.user_id === session.user_id) || null)
const onlineCount = computed(() => agentStatusList.value.filter(a => a.status === 1).length)

async function loadAgentStatus() {
  try {
    const r = await api('/api/admin/agent-status')
    if (r.code === 0) {
      agentStatusList.value = r.data || []
      const me = agentStatusList.value.find(a => a.user_id === session.user_id)
      if (me) myStatus.value = me.status
    }
  } catch (e) {}
}

async function setMyStatus() {
  try {
    const r = await api('/api/admin/agent-status', { method: 'PUT', body: { status: myStatus.value } })
    if (r.code === 0) toast(myStatus.value === 1 ? '已切换在线' : myStatus.value === 2 ? '已切换忙碌' : '已切换离线', 'success')
    await loadAgentStatus()
  } catch (e) {}
}

// 话术面板：加载启用中的快捷回复
async function toggleQuickReplies() {
  showQuickReplies.value = !showQuickReplies.value
  if (showQuickReplies.value && !quickReplies.value.length) {
    try {
      const r = await api('/api/admin/quick-replies', { params: { active: 1 } })
      if (r.code === 0) quickReplies.value = r.data || []
    } catch (e) {}
  }
}

// 点击话术 → 插入输入框（可再编辑，不直接发送）
function insertQuickReply(qr) {
  replyText.value = (replyText.value ? replyText.value + '\n' : '') + qr.content
  showQuickReplies.value = false
}
function canHandle() {
  return !!(current.value && (isPlat || isAdmin || current.value.handler_uid === session.user_id))
}
async function loadMembers() {
  try {
    const r = await api('/api/admin/tickets/transfer-targets')
    if (r.code === 0) members.value = r.data || []
  } catch (e) {}
}
async function transferTicket() {
  if (!transferTo.value) return toast('请选择目标客服', 'error')
  const r = await api('/api/admin/tickets/' + current.value.id + '/transfer', {
    method: 'POST', body: { to_user_id: transferTo.value }
  })
  if (r.code !== 0) return
  toast('已转交')
  transferTo.value = 0
  await load()
  await loadAgentStatus()
}

function fmtTime(t) {
  if (!t) return '-'
  return String(t).replace('T', ' ').slice(0, 19)
}

// 关键词清空时自动刷新列表
function onKeywordInput() {
  if (!keyword.value) load()
}

async function load() {
  try {
    const params = { status: status.value, page: 1, size: 50 }
    if (scope.value === 'mine') params.scope = 'mine'
    if (priority.value !== '-1') params.priority = priority.value
    if (keyword.value.trim()) params.q = keyword.value.trim()
    const r = await api('/api/admin/tickets', { params })
    if (r.code !== 0) return
    list.value = r.data.list || []
    total.value = r.data.total || 0
  } catch (e) {}
}

function resetFilters() {
  status.value = '-1'
  priority.value = '-1'
  keyword.value = ''
  scope.value = 'all'
  load()
}

async function fetchMessages(t) {
  try {
    const r = await api('/api/admin/tickets/' + t.id + '/messages')
    if (r.code === 0) {
      messages.value = r.data || []
      await nextTick()
      if (msgBox.value) msgBox.value.scrollTop = msgBox.value.scrollHeight
    }
  } catch (e) {}
}

// 标记当前工单会话已读（未读数归零）
async function markRead(t) {
  try {
    await api('/api/admin/tickets/' + t.id + '/read', { method: 'POST', body: {}, silent: true })
  } catch (e) {}
}

async function open(t) {
  current.value = t
  showLogs.value = false
  logs.value = [] // 切换工单必须清空日志，否则残留上一工单的操作日志
  await fetchMessages(t)
  markRead(t)
  load() // 刷新列表未读数
}

// 操作日志开关：打开时重新加载当前工单日志（避免显示上一工单的旧数据）
async function toggleLogs() {
  showLogs.value = !showLogs.value
  if (showLogs.value) await loadLogs()
}

// 接管/关闭/改优先级后，用最新列表数据刷新当前工单状态（否则详情区仍显示旧状态）
function refreshCurrent() {
  const fresh = list.value.find(x => x.id === current.value.id)
  if (fresh) current.value = fresh
}

async function takeover() {
  try {
    const r = await api('/api/admin/tickets/' + current.value.id + '/takeover', { method: 'POST', body: { wait_time: 3 } })
    if (r.code !== 0) return
    toast('已接管对话', 'success')
    await load()
    refreshCurrent()
    await open(current.value)
  } catch (e) {}
}

async function reply() {
  const content = replyText.value.trim()
  if (!content) return
  try {
    const r = await api('/api/admin/tickets/' + current.value.id + '/reply', { method: 'POST', body: { content } })
    if (r.code !== 0) return
    replyText.value = ''
    await open(current.value)
  } catch (e) {}
}

async function closeTicket() {
  try {
    const r = await api('/api/admin/tickets/' + current.value.id + '/close', { method: 'POST', body: {} })
    if (r.code !== 0) return
    toast('工单已关闭', 'success')
    await load()
    refreshCurrent()
    await open(current.value)
  } catch (e) {}
}

async function setPriority() {
  try {
    const r = await api('/api/admin/tickets/' + current.value.id + '/priority', {
      method: 'POST', body: { priority: newPriority.value, remark: '前端修改优先级' }
    })
    if (r.code !== 0) return
    toast('优先级已更新', 'success')
    await load()
    refreshCurrent()
  } catch (e) {}
}

async function loadLogs() {
  try {
    const r = await api('/api/admin/tickets/' + current.value.id + '/logs')
    if (r.code === 0) logs.value = r.data || []
  } catch (e) {}
}

// WS 实时推送：命中当前工单线程则刷新详情，否则刷新列表未读数
function onWsMsg(e) {
  const msg = e.detail
  if (!msg) return
  const hit = current.value && (msg.thread_id === current.value.user_id || msg.thread_id === current.value.thread_id)
  if (msg.type === 'user_msg' || msg.type === 'human_reply' || msg.type === 'human_notice') {
    if (hit) {
      // 当前工单收到新消息：拉取最新消息并标记已读
      fetchMessages(current.value)
      markRead(current.value)
    } else {
      // 其他工单有新消息：刷新列表未读数
      load()
    }
  } else if (msg.type === 'ticket_update' || msg.type === 'new_ticket') {
    load()
    loadAgentStatus()
    if (hit) refreshCurrent()
  }
}

// ws 连接成功/重连后同步一次最新数据（替代旧的定时轮询）
function onWsConnected() {
  load()
  loadAgentStatus()
  if (current.value) {
    fetchMessages(current.value)
    markRead(current.value)
  }
}

onMounted(() => {
  loadMembers()
  load()
  loadAgentStatus()
  window.addEventListener('cs-ws-msg', onWsMsg)
  window.addEventListener('cs-ws-connected', onWsConnected)
})
onUnmounted(() => {
  window.removeEventListener('cs-ws-msg', onWsMsg)
  window.removeEventListener('cs-ws-connected', onWsConnected)
})
</script>

<style scoped>
.tk-layout { display: grid; grid-template-columns: 340px 1fr; gap: 16px; align-items: start; }
.tk-list { padding: 14px; }
.tk-toolbar { display: flex; align-items: center; gap: 10px; margin-bottom: 12px; flex-wrap: wrap; }
.tk-items { max-height: calc(100vh - 220px); overflow-y: auto; display: flex; flex-direction: column; gap: 8px; }
.tk-item { border: 1px solid var(--c-border); border-radius: var(--radius); padding: 10px 12px; cursor: pointer; transition: all .12s; }
.tk-item:hover { border-color: var(--c-primary); }
.tk-item.active { border-color: var(--c-primary); background: var(--c-primary-weak); }
.tk-item-row { display: flex; align-items: center; gap: 6px; margin-bottom: 4px; }
.tk-detail { padding: 16px; display: flex; flex-direction: column; min-height: calc(100vh - 160px); }
.tk-detail-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.msg-list { flex: 1; overflow-y: auto; max-height: calc(100vh - 380px); display: flex; flex-direction: column; gap: 10px; padding: 4px 0; }
.msg { display: flex; flex-direction: column; max-width: 78%; padding: 9px 12px; border-radius: 10px; background: var(--c-hover); }
.msg-user { align-self: flex-end; background: var(--c-primary); color: #fff; }
.msg-human { align-self: flex-end; background: #d97706; color: #fff; }
.msg-system { align-self: center; background: transparent; color: var(--c-text-3); font-size: 12px; }
.msg-role { font-size: 10px; opacity: .75; margin-bottom: 3px; }
.msg-time { font-size: 10px; margin-top: 4px; }
.msg-user .msg-time, .msg-human .msg-time { color: rgba(255,255,255,.7); }
.msg-content { font-size: 13px; line-height: 1.6; white-space: pre-wrap; word-break: break-word; }
.tk-actions { display: flex; align-items: flex-start; gap: 8px; margin-top: 12px; flex-wrap: wrap; }
.logs { margin-top: 12px; border-top: 1px solid var(--c-border); padding-top: 10px; max-height: 160px; overflow-y: auto; display: flex; flex-direction: column; gap: 4px; }
.empty { text-align: center; color: var(--c-text-3); padding: 24px 0; font-size: 13px; }
</style>
