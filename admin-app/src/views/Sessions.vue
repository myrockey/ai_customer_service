<template>
  <div>
    <div class="sv-layout">
      <!-- 会话列表 -->
      <div class="card sv-list">
        <div class="sv-toolbar">
          <b>会话列表</b>
          <span class="text-3" style="font-size:12px;">关键词</span>
          <input v-model="keyword" class="input" style="width:150px;height:26px;"
                 placeholder="会话ID/姓名/电话/邮箱/地址" @keyup.enter="load" @input="onKeywordInput" />
          <span class="text-3" style="font-size:12px;">转人工</span>
          <select v-model="handoff" class="select" style="width:86px;" @change="load">
            <option value="0">全部</option>
            <option value="1">已转人工</option>
          </select>
          <span class="text-3" style="font-size:12px;">日期</span>
          <input v-model="start" type="date" class="input" style="width:130px;height:26px;" @change="load" />
          <span class="text-3" style="font-size:12px;">至</span>
          <input v-model="end" type="date" class="input" style="width:130px;height:26px;" @change="load" />
          <button class="btn btn-sm" @click="load">查询</button>
          <button class="btn btn-sm" @click="resetFilters">重置</button>
          <button class="btn btn-sm" @click="load">刷新</button>
          <button v-if="canCleanup" class="btn btn-sm" v-perm="'session:cleanup'" @click="showCleanup = true">🗑 清理归档</button>
          <span class="text-3" style="font-size:12px;margin-left:10px;">标签</span>
          <input v-model="tagFilter" class="input" style="width:110px;height:26px;"
                 placeholder="按标签筛选" @keyup.enter="load" @input="onTagFilterInput" />
          <span class="text-3" style="font-size:12px;">共 {{ total }} 个会话</span>
          <div class="pager">
            <button class="btn btn-sm" :disabled="page <= 1" @click="turn(-1)">上一页</button>
            <span class="text-3" style="font-size:12px;">{{ page }}/{{ pages }}</span>
            <button class="btn btn-sm" :disabled="page >= pages" @click="turn(1)">下一页</button>
          </div>
        </div>
        <div class="sv-items">
          <div v-if="!list.length" class="empty">暂无会话（客户尚未聊天）</div>
          <div v-for="s in list" :key="s.thread_id" class="sv-item"
               :class="{ active: current && current.thread_id === s.thread_id }"
               @click="open(s)">
            <div class="sv-item-row">
              <span class="mono" style="font-size:12px;max-width:150px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">{{ s.thread_id }}</span>
              <span v-if="s.has_open_ticket" class="badge badge-orange">人工</span>
              <span v-if="s.unread_count > 0" class="badge badge-red" style="border-radius:10px;padding:0 6px;">{{ s.unread_count }}</span>
              <span v-if="s.tags" class="text-3" style="font-size:11px;margin-left:auto;">
                <span v-for="t in (s.tags || '').split(',').filter(Boolean)" :key="t" class="badge badge-blue" style="margin-right:4px;">{{ t }}</span>
              </span>
              <span v-else class="text-3" style="font-size:11px;margin-left:auto;">{{ fmtTime(s.last_active_at) }}</span>
            </div>
            <div class="sv-item-row text-2" style="font-size:12px;">
              <span>👤 {{ s.name || '-' }}</span>
              <span>📞 {{ s.phone || '-' }}</span>
              <span class="text-3">({{ s.user_count }} 条客户消息)</span>
            </div>
            <div class="sv-item-row text-2" style="font-size:12px;">
              <span>📍 {{ s.address || '-' }}</span>
              <span>✉️ {{ s.email || '-' }}</span>
            </div>
            <div v-if="s.summary" class="sv-preview sv-summary" :title="s.summary">{{ s.summary }}</div>
            <div class="sv-preview text-3">{{ s.last_content || '' }}</div>
          </div>
        </div>
      </div>

      <!-- 会话详情 -->
      <div class="card sv-detail">
        <template v-if="current">
          <div class="sv-detail-head">
            <div>
              <b class="mono">{{ current.thread_id }}</b>
              <span v-if="current.has_open_ticket" class="badge badge-orange" style="margin-left:8px;">已转人工</span>
            </div>
            <div class="text-3" style="font-size:12px;">最后活跃 {{ fmtTime(current.last_active_at) }}</div>
          </div>

          <!-- 会话摘要（AI 自动生成） -->
          <div v-if="current.summary" class="summary-box">
            <span class="profile-title">会话摘要</span>
            <p class="summary-text">{{ current.summary }}</p>
          </div>

          <!-- 会话标签（运营打标，逗号分隔多标签） -->
          <div class="tag-box">
            <div class="tag-head">
              <span class="profile-title">会话标签</span>
              <span class="text-3" style="font-size:11px;">{{ tagSummary }}</span>
            </div>
            <div class="tag-body">
              <input v-model="tagsText" class="input" style="flex:1;"
                     placeholder="如：意向客户,需要回访（逗号分隔）" @keyup.enter="saveTags" />
              <button class="btn btn-sm btn-primary" :disabled="savingTags" v-perm="'session:profile:edit'" @click="saveTags">
                {{ savingTags ? '保存中...' : '保存' }}
              </button>
            </div>
            <div v-if="tagsList.length" class="tag-list">
              <span v-for="t in tagsList" :key="t" class="badge badge-blue" style="margin-right:6px;">{{ t }}</span>
            </div>
          </div>

          <!-- 客户信息（默认折叠，点开编辑） -->
          <div class="profile-box">
            <div class="profile-head" :class="{ open: profileOpen }" @click="profileOpen = !profileOpen">
              <span class="profile-title">客户信息</span>
              <span class="text-3" style="font-size:11px;">{{ profileSummary }}</span>
              <svg class="arrow" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 6l6 6-6 6"/></svg>
            </div>
            <div v-show="profileOpen" class="profile-body">
              <div class="profile-grid">
                <div class="p-field"><label>姓名</label><input v-model="profile.name" class="input" /></div>
                <div class="p-field"><label>电话</label><input v-model="profile.phone" class="input" /></div>
                <div class="p-field"><label>邮箱</label><input v-model="profile.email" class="input" /></div>
                <div class="p-field"><label>地址</label><input v-model="profile.address" class="input" /></div>
                <div class="p-field p-field-wide"><label>备注</label><input v-model="profile.remark" class="input" /></div>
              </div>
              <div class="profile-actions">
                <button class="btn btn-sm btn-primary" :disabled="savingProfile" v-perm="'session:profile:edit'" @click="saveProfile">
                  {{ savingProfile ? '保存中...' : '保存' }}
                </button>
                <button class="btn btn-sm" :disabled="reextracting" v-perm="'session:profile:edit'" @click="reextract">
                  {{ reextracting ? '提取中...' : '重新提取电话/邮箱' }}
                </button>
                <span class="text-3" style="font-size:11px;">电话/邮箱自动提取</span>
              </div>
            </div>
          </div>

          <!-- 聊天记录 -->
          <div class="msg-list" ref="msgBox">
            <div v-for="(m, i) in messages" :key="i" class="msg" :class="'msg-' + m.role">
              <span class="msg-role" :class="roleClass(m.role)">{{ roleText(m.role) }}</span>
              <div class="msg-content">
                <img v-if="m.msg_type === 'image'" :src="m.content" class="msg-img" @click="previewImg(m.content)" />
                <video v-else-if="m.msg_type === 'video'" :src="m.content" class="msg-video" controls preload="metadata"></video>
                <template v-else>{{ m.content }}</template>
              </div>
              <div class="msg-time text-3">{{ fmtTime(m.created_at) }}<span v-if="m.archived" class="archived-tag">已归档</span></div>
            </div>
            <div v-if="!messages.length" class="empty">暂无消息</div>
          </div>
        </template>
        <div v-else class="empty" style="margin-top:40vh;">选择左侧会话查看聊天记录</div>
      </div>
    </div>

    <!-- 清理归档弹窗（会话数据维护） -->
    <CleanupDialog v-if="showCleanup" @close="showCleanup = false" @done="load" />

    <!-- 图片预览 -->
    <div v-if="previewImgUrl" class="img-preview-mask" @click="previewImgUrl = ''">
      <img :src="previewImgUrl" class="img-preview" @click.stop />
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import { api, toast, session } from '../api'
import CleanupDialog from './CleanupDialog.vue'

const route = useRoute()

const list = ref([])
const total = ref(0)
const page = ref(1)
const showCleanup = ref(false)
// 清理归档为运维操作：仅管理员或拥有 session:cleanup 权限的角色可见/可操作
const canCleanup = computed(() => !session.user_id || (session.permissions || []).includes('session:cleanup'))
const size = 20
const keyword = ref('')
const handoff = ref('0')
const start = ref('')
const end = ref('')
const tagFilter = ref('')
const tagsText = ref('')
const savingTags = ref(false)
const current = ref(null)
const messages = ref([])
const profile = ref({ name: '', phone: '', address: '', email: '', remark: '' })
const profileOpen = ref(false)
const savingProfile = ref(false)
const reextracting = ref(false)
const msgBox = ref(null)
const previewImgUrl = ref('')
function previewImg(url) { previewImgUrl.value = url }

const pages = computed(() => Math.max(1, Math.ceil(total.value / size)))

// 客户信息摘要：有值字段拼接，无值显示"暂无"
const profileSummary = computed(() => {
  const p = profile.value
  const parts = [p.name, p.phone, p.email, p.address].filter(Boolean)
  return parts.length ? parts.join(' · ') : '暂无（点开填写/查看）'
})

function fmtTime(t) { return t ? String(t).replace('T', ' ').slice(0, 19) : '-' }
const roleText = r => ({ user: '客户', ai: 'AI', human: '人工', system: '系统' }[r] || r)
const roleClass = r => ({ user: 'role-user', ai: 'role-ai', human: 'role-human', system: 'role-sys' }[r] || '')

// 关键词清空时自动刷新列表
function onKeywordInput() {
  if (!keyword.value) load()
}

const tagsList = computed(() => (tagsText.value || '').split(',').map(t => t.trim()).filter(Boolean))
const tagSummary = computed(() => tagsList.value.length ? tagsList.value.join(' · ') : '暂无标签')

async function load() {
  try {
    const params = { page: page.value, size }
    if (keyword.value.trim()) params.q = keyword.value.trim()
    if (handoff.value === '1') params.handoff = '1'
    if (start.value) params.start = start.value
    if (end.value) params.end = end.value
    if (tagFilter.value.trim()) params.tag = tagFilter.value.trim()
    const r = await api('/api/admin/sessions', { params })
    if (r.code !== 0) return
    list.value = r.data.list || []
    total.value = r.data.total || 0
    if (current.value) {
      const fresh = list.value.find(x => x.thread_id === current.value.thread_id)
      if (fresh) current.value = fresh
    }
  } catch (e) {}
}

function resetFilters() {
  keyword.value = ''
  handoff.value = '0'
  start.value = ''
  end.value = ''
  tagFilter.value = ''
  page.value = 1
  load()
}

// 标签筛选：清空时自动刷新
function onTagFilterInput() {
  if (!tagFilter.value) load()
}

function turn(d) {
  const np = page.value + d
  if (np < 1) return
  page.value = np
  load()
}

async function open(s) {
  current.value = s
  tagsText.value = s.tags || ''
  const tr = await api('/api/admin/sessions/' + s.thread_id + '/tags', { silent: true })
  if (tr.code === 0 && tr.data) tagsText.value = tr.data.tags || ''
  await fetchDetail(s.thread_id)
}

async function saveTags() {
  if (!current.value) return
  savingTags.value = true
  try {
    const r = await api('/api/admin/sessions/' + current.value.thread_id + '/tags', {
      method: 'PUT', body: { tags: tagsText.value }
    })
    if (r.code === 0) {
      toast('标签已保存', 'success')
      current.value.tags = tagsText.value
      load()
    }
  } finally {
    savingTags.value = false
  }
}

async function fetchDetail(threadId) {
  try {
    const r = await api('/api/admin/sessions/' + encodeURIComponent(threadId) + '/messages')
    if (r.code === 0) {
      messages.value = r.data.messages || []
      const p = r.data.profile || {}
      profile.value = { name: p.name || '', phone: p.phone || '', address: p.address || '', email: p.email || '', remark: p.remark || '' }
      await nextTick()
      if (msgBox.value) msgBox.value.scrollTop = msgBox.value.scrollHeight
    }
  } catch (e) {}
}

async function saveProfile() {
  savingProfile.value = true
  try {
    const r = await api('/api/admin/sessions/' + encodeURIComponent(current.value.thread_id) + '/profile', {
      method: 'PUT', body: profile.value
    })
    if (r.code === 0) {
      toast('客户信息已保存')
      load()
    } else {
      toast(r.msg || '保存失败', 'error')
    }
  } catch (e) {
  } finally {
    savingProfile.value = false
  }
}

async function reextract() {
  reextracting.value = true
  try {
    const r = await api('/api/admin/sessions/' + encodeURIComponent(current.value.thread_id) + '/reextract', { method: 'POST', body: {} })
    if (r.code === 0) {
      toast('重新提取完成')
      await fetchDetail(current.value.thread_id)
      load()
    } else {
      toast(r.msg || '提取失败', 'error')
    }
  } catch (e) {
  } finally {
    reextracting.value = false
  }
}

onMounted(() => {
  load()
  // 支持从「客户列表」跳转：?thread=xxx 直接打开该会话详情（即使已被归档也可看完整记录）
  const tid = route.query.thread
  if (tid) {
    current.value = { thread_id: String(tid) }
    fetchDetail(String(tid))
  }
})
</script>

<style scoped>
.sv-layout { display: grid; grid-template-columns: 400px 1fr; gap: 16px; align-items: start; margin-top: 16px; }
.sv-list { padding: 14px; }
.sv-toolbar { display: flex; align-items: center; gap: 10px; margin-bottom: 12px; flex-wrap: wrap; }
.pager { display: flex; align-items: center; gap: 6px; margin-left: auto; }
.sv-items { max-height: calc(100vh - 300px); overflow-y: auto; display: flex; flex-direction: column; gap: 8px; }
.sv-item { border: 1px solid var(--c-border); border-radius: var(--radius); padding: 10px 12px; cursor: pointer; transition: all .12s; }
.sv-item:hover { border-color: var(--c-primary); }
.sv-item.active { border-color: var(--c-primary); background: var(--c-primary-weak); }
.sv-item-row { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; flex-wrap: wrap; }
.sv-preview { font-size: 11px; color: var(--c-text-3); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; margin-top: 2px; }
.sv-summary { color: #2563eb; background: rgba(37,99,235,.08); border-radius: 4px; padding: 2px 6px; margin-top: 6px;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; white-space: normal; }
.summary-box { margin-bottom: 10px; padding: 8px 10px; background: rgba(37,99,235,.06); border: 1px solid rgba(37,99,235,.15); border-radius: 6px; }
.summary-text { margin: 4px 0 0; font-size: 12px; color: var(--c-text); line-height: 1.6; }

.sv-detail { padding: 16px; display: flex; flex-direction: column; height: calc(100vh - 300px); min-height: 480px; }
.sv-detail-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; flex: none; }
.profile-box { border: 1px solid var(--c-border); border-radius: var(--radius); padding: 0; margin-bottom: 12px; flex: none; overflow: hidden; }
.tag-box { border: 1px solid var(--c-border); border-radius: var(--radius); padding: 10px 12px; margin-bottom: 12px; flex: none; }
.tag-head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.tag-head .profile-title { font-size: 13px; font-weight: 600; }
.tag-body { display: flex; align-items: center; gap: 8px; }
.tag-list { display: flex; align-items: center; gap: 4px; margin-top: 8px; flex-wrap: wrap; }
.profile-head { display: flex; align-items: center; gap: 8px; padding: 8px 12px; cursor: pointer; user-select: none; background: var(--c-hover); }
.profile-head .profile-title { font-size: 13px; font-weight: 600; }
.profile-head .arrow { width: 13px; height: 13px; margin-left: auto; opacity: .5; transition: transform .15s; }
.profile-head.open .arrow { transform: rotate(90deg); }
.profile-body { padding: 10px 12px; border-top: 1px solid var(--c-border); }
.profile-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 8px; }
.p-field { display: flex; flex-direction: column; gap: 3px; }
.p-field-wide { grid-column: 1 / -1; }
.p-field label { font-size: 11px; color: var(--c-text-2); }
.p-field .input { width: 100%; box-sizing: border-box; padding: 6px 8px; font-size: 13px; }
.profile-actions { display: flex; align-items: center; gap: 8px; margin-top: 8px; flex-wrap: wrap; }

.msg-list { flex: 1; min-height: 0; overflow-y: auto; display: flex; flex-direction: column; gap: 10px; padding: 4px 0; }
.msg { display: flex; flex-direction: column; max-width: 78%; padding: 9px 12px; border-radius: 10px; background: var(--c-hover); }
.msg-user { align-self: flex-end; background: var(--c-primary); color: #fff; }
.msg-human { align-self: flex-end; background: #d97706; color: #fff; }
.msg-system { align-self: center; background: transparent; color: var(--c-text-3); font-size: 12px; }
.msg-role { font-size: 10px; opacity: .75; margin-bottom: 3px; }
.msg-time { font-size: 10px; margin-top: 4px; }
.archived-tag { display: inline-block; margin-left: 6px; padding: 0 5px; border-radius: 4px; background: rgba(0,0,0,.06); font-size: 10px; color: var(--c-text-2); }
.msg-user .msg-time, .msg-human .msg-time { color: rgba(255,255,255,.7); }
.msg-content { font-size: 13px; line-height: 1.6; white-space: pre-wrap; word-break: break-word; }
.msg-img { max-width: 220px; max-height: 180px; border-radius: 6px; display: block; cursor: zoom-in; border: 1px solid rgba(0,0,0,.08); }
.msg-video { max-width: 280px; max-height: 220px; border-radius: 6px; display: block; border: 1px solid rgba(0,0,0,.08); background: #000; }
.img-preview-mask { position: fixed; inset: 0; background: rgba(0,0,0,.65); display: flex; align-items: center; justify-content: center; z-index: 999; cursor: zoom-out; }
.img-preview { max-width: 80vw; max-height: 80vh; border-radius: 6px; box-shadow: 0 8px 40px rgba(0,0,0,.4); }
.empty { text-align: center; color: var(--c-text-3); padding: 24px 0; font-size: 13px; }
</style>
