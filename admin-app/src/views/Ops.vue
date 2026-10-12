<template>
  <div>
    <div class="head-row">
      <div class="filters">
        <b>运营看板</b>
        <span class="text-3" style="font-size:12px;margin-left:12px;">最后更新 {{ updated }}</span>
      </div>
      <button class="btn btn-sm" @click="load">刷新</button>
    </div>

    <!-- 总览卡片 -->
    <div style="display:flex;gap:16px;flex-wrap:wrap;margin:16px 0;">
      <div class="card" style="flex:1 1 150px;min-width:140px;padding:16px;">
        <div class="text-2" style="font-size:12px;">总会话数</div>
        <div class="stat-num" style="font-size:24px;font-weight:600;margin-top:6px;">{{ ov.total_sessions || 0 }}</div>
      </div>
      <div class="card" style="flex:1 1 150px;min-width:140px;padding:16px;">
        <div class="text-2" style="font-size:12px;">总消息数</div>
        <div class="stat-num" style="font-size:24px;font-weight:600;margin-top:6px;">{{ ov.total_messages || 0 }}</div>
        <div class="text-3" style="font-size:11px;margin-top:2px;">客户消息 {{ ov.user_messages || 0 }}</div>
      </div>
      <div class="card" style="flex:1 1 150px;min-width:140px;padding:16px;">
        <div class="text-2" style="font-size:12px;">开放工单</div>
        <div class="stat-num" style="font-size:24px;font-weight:600;margin-top:6px;color:#d97706;">{{ ov.open_tickets || 0 }}</div>
      </div>
      <div class="card" style="flex:1 1 150px;min-width:140px;padding:16px;">
        <div class="text-2" style="font-size:12px;">已转人工</div>
        <div class="stat-num" style="font-size:24px;font-weight:600;margin-top:6px;color:#2563eb;">{{ ov.handoff_sessions || 0 }}</div>
      </div>
      <div class="card" style="flex:1 1 150px;min-width:140px;padding:16px;">
        <div class="text-2" style="font-size:12px;">平均评分</div>
        <div class="stat-num" style="font-size:24px;font-weight:600;margin-top:6px;color:#f5a623;">{{ ov.avg_rating ? Number(ov.avg_rating).toFixed(2) : '-' }}<span style="font-size:13px;color:#999;"> / 5</span></div>
      </div>
      <div class="card" style="flex:1 1 150px;min-width:140px;padding:16px;">
        <div class="text-2" style="font-size:12px;">满意率（4-5星）</div>
        <div class="stat-num" style="font-size:24px;font-weight:600;margin-top:6px;color:#52c41a;">{{ ov.satisfaction_rate ? Number(ov.satisfaction_rate).toFixed(1) : 0 }}%</div>
        <div class="text-3" style="font-size:11px;margin-top:2px;">评价 {{ ov.rating_count || 0 }} 次</div>
      </div>
    </div>

    <div style="display:flex;gap:16px;flex-wrap:wrap;">
      <!-- 近7天趋势 -->
      <div class="card" style="flex:1 1 380px;min-width:340px;padding:16px;">
        <div style="font-size:13px;font-weight:600;margin-bottom:12px;">近 7 天会话 / 消息趋势</div>
        <div style="display:flex;align-items:flex-end;gap:8px;height:140px;padding-top:8px;">
          <div v-for="d in trend" :key="d.date" style="flex:1;display:flex;flex-direction:column;align-items:center;gap:4px;">
            <span style="font-size:10px;color:#999;">{{ d.messages || 0 }}</span>
            <div style="display:flex;align-items:flex-end;gap:3px;height:90px;">
              <div :style="{ width: '10px', height: Math.max(3, (d.sessions / trendMax) * 90) + 'px', background: '#94D4D0', borderRadius: '3px 3px 0 0' }" :title="'会话 ' + (d.sessions||0)"></div>
              <div :style="{ width: '10px', height: Math.max(3, (d.messages / trendMax) * 90) + 'px', background: '#8BC8EA', borderRadius: '3px 3px 0 0' }" :title="'消息 ' + (d.messages||0)"></div>
            </div>
            <span style="font-size:10px;color:#999;">{{ d.date.slice(5) }}</span>
          </div>
        </div>
        <div style="display:flex;gap:14px;margin-top:8px;font-size:11px;color:#666;">
          <span><i style="display:inline-block;width:10px;height:10px;background:#94D4D0;border-radius:2px;margin-right:4px;"></i>会话数</span>
          <span><i style="display:inline-block;width:10px;height:10px;background:#8BC8EA;border-radius:2px;margin-right:4px;"></i>消息数</span>
        </div>
      </div>

      <!-- 工单状态分布 + 客服排行 -->
      <div style="flex:1 1 320px;min-width:300px;display:flex;flex-direction:column;gap:16px;">
        <div class="card" style="padding:16px;">
          <div style="font-size:13px;font-weight:600;margin-bottom:12px;">工单状态分布（共 {{ ticketTotal }}）</div>
          <div v-for="t in tickets" :key="t.status" style="display:flex;align-items:center;gap:8px;margin-bottom:8px;">
            <span style="width:52px;font-size:12px;color:#666;">{{ t.name }}</span>
            <div style="flex:1;height:10px;background:#f0f0f0;border-radius:5px;overflow:hidden;">
              <div :style="{ width: ticketPct(t.count) + '%', background: ticketColor(t.status), height: '100%', borderRadius: '5px' }"></div>
            </div>
            <span style="width:30px;font-size:12px;color:#999;text-align:right;">{{ t.count }}</span>
          </div>
        </div>
        <div class="card" style="padding:16px;">
          <div style="font-size:13px;font-weight:600;margin-bottom:12px;">客服处理量排行</div>
          <div v-if="!agents.length" class="text-2" style="font-size:12px;padding:6px 0;">暂无数据</div>
          <table v-else style="width:100%;border-collapse:collapse;font-size:12px;">
            <thead>
              <tr style="color:#999;text-align:left;">
                <th style="padding:4px 6px;">客服</th>
                <th style="padding:4px 6px;text-align:right;">处理总数</th>
                <th style="padding:4px 6px;text-align:right;">开放中</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(a, i) in agents" :key="a.handler_name" style="border-top:1px solid #f0f0f0;">
                <td style="padding:6px;"><span class="badge" :class="'badge-' + ['blue','green','orange','red','gray'][i % 5]">{{ i + 1 }}</span> {{ a.handler_name }}</td>
                <td style="padding:6px;text-align:right;font-weight:600;">{{ a.total }}</td>
                <td style="padding:6px;text-align:right;color:#d97706;">{{ a.open }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { api } from '../api'

const data = ref({ overview: {}, trend: [], tickets: [], agents: [] })
const updated = ref('-')

const ov = computed(() => data.value.overview || {})
const trend = computed(() => data.value.trend || [])
const tickets = computed(() => data.value.tickets || [])
const agents = computed(() => data.value.agents || [])
const ticketTotal = computed(() => tickets.value.reduce((s, t) => s + (t.count || 0), 0))
const trendMax = computed(() => Math.max(1, ...trend.value.flatMap(d => [d.sessions || 0, d.messages || 0])))

const TICKET_COLORS = { 0: '#faad14', 1: '#2563eb', 2: '#bfbfbf', 3: '#52c41a' }
const ticketColor = s => TICKET_COLORS[s] || '#bfbfbf'
function ticketPct(c) {
  if (!ticketTotal.value) return 0
  return Math.round((c / ticketTotal.value) * 100)
}

async function load() {
  try {
    const r = await api('/api/admin/ops/dashboard')
    if (r.code === 0 && r.data) {
      data.value = r.data
      updated.value = new Date().toLocaleString('zh-CN', { hour12: false })
    }
  } catch (e) {}
}

onMounted(load)
</script>
