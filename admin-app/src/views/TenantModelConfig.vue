<template>
  <div>
    <div class="notice-bar">
      💡 {{ platform ? '平台默认模型配置：即平台管理员租户（t_admin）的模型配置，各租户选择「跟随平台」时使用。Key 存于 t_admin 租户配置，AES 加密入库。' : '租户模型配置（BYOK）：可跟随平台默认，也可配置租户自有的模型 API Key（Key 存于本租户配置，AES 加密入库）。修改保存后 Agent 会自动重建，新会话立即生效。' }}
    </div>

    <div class="card" style="margin-top:16px;">
      <div class="head-row">
        <div>
          <b>对话模型</b>
          <div class="text-3" style="font-size:12px;margin-top:2px;">{{ platform ? '平台默认（直接编辑）' : (chat.custom ? '租户自定义' : '跟随平台默认') }}</div>
        </div>
        <div style="display:flex;align-items:center;gap:8px;">
          <template v-if="!platform">
            <span class="text-3" style="font-size:12px;">跟随平台</span>
            <label class="switch">
              <input v-model="chat.custom" type="checkbox" :disabled="platform" />
              <span class="slider"></span>
            </label>
            <span class="text-3" style="font-size:12px;">自定义</span>
          </template>
        </div>
      </div>
      <div v-if="chat.custom" class="grid2">
        <label class="field">
          <span class="f-label">模型提供商</span>
          <select v-model="chat.provider" class="select" @change="onChatProvider">
            <option v-for="p in chatProviders" :key="p.id" :value="p.id">{{ p.provider_name }}</option>
          </select>
          <span class="key-status warn">选择后自动带出该提供商的请求地址与默认模型名</span>
        </label>
        <label class="field">
          <span class="f-label">模型名称（可覆盖提供商默认）</span>
          <input v-model="chat.model_name" class="input mono" placeholder="留空使用提供商默认模型" />
        </label>
        <label class="field">
          <span class="f-label">API Key（租户密钥，必填）</span>
          <input v-model="chat.api_key" type="password" class="input mono" :placeholder="chat.api_key_configured ? '已配置，留空保持' : 'sk-...'" />
          <div v-if="chat.api_key_configured" class="key-row">
            <span class="key-tag">✓ 已配置 {{ chat.api_key_masked || '' }}</span>
            <button class="btn btn-sm" type="button" @click="clearChatKey">清除</button>
          </div>
          <span v-else class="key-status warn">未配置（租户自定义必须填写 Key）</span>
        </label>
        <label class="field">
          <span class="f-label">API Base URL（可覆盖提供商默认）</span>
          <input v-model="chat.api_base" class="input mono" placeholder="留空使用提供商默认 Base" />
        </label>
        <label class="field">
          <span class="f-label">输入单价·命中缓存（元/百万tokens）</span>
          <input v-model="chat.chat_in_cache_price" type="number" min="0" step="0.01" class="input mono" placeholder="0=未定价，如 DeepSeek 缓存命中 ¥0.1-0.5" />
        </label>
        <label class="field">
          <span class="f-label">输入单价·未命中缓存（元/百万tokens）</span>
          <input v-model="chat.chat_in_price" type="number" min="0" step="0.01" class="input mono" placeholder="0=未定价，用量不计算费用" />
        </label>
        <label class="field">
          <span class="f-label">输出单价（元/百万tokens）</span>
          <input v-model="chat.chat_out_price" type="number" min="0" step="0.01" class="input mono" placeholder="0=未定价，用量不计算费用" />
        </label>
        <span class="key-status warn" style="font-size:11px;">💡 未返回缓存拆分的模型，全部输入按「未命中缓存」单价计费</span>
      </div>
      <div v-else class="text-3" style="font-size:12px;padding:10px 0 4px;">使用平台默认对话模型</div>
      <div class="grid4" style="padding:10px;">
        <button v-if="chat.custom" class="btn btn-sm" type="button" :disabled="testing.chat" @click="testModel('chat')">{{ testing.chat ? '测试中...' : '测试模型可用性' }}</button>
        <span v-if="testResult.chat" class="test-result" :class="testResult.chat.ok ? 'ok' : 'fail'"><span :title="testResult.chat.text">{{ testResult.chat.text }}</span></span>
      </div>
    </div>

    <div class="card" style="margin-top:16px;">
      <div class="head-row">
        <div>
          <b>Embedding 模型</b>
          <div class="text-3" style="font-size:12px;margin-top:2px;">{{ platform ? '平台默认（直接编辑）' : (emb.custom ? '租户自定义' : '跟随平台默认') }}</div>
        </div>
        <div style="display:flex;align-items:center;gap:8px;">
          <template v-if="!platform">
            <span class="text-3" style="font-size:12px;">跟随平台</span>
            <label class="switch">
              <input v-model="emb.custom" type="checkbox" :disabled="platform" />
              <span class="slider"></span>
            </label>
            <span class="text-3" style="font-size:12px;">自定义</span>
          </template>
        </div>
      </div>
      <div v-if="emb.custom" class="grid2">
        <label class="field">
          <span class="f-label">模型提供商</span>
          <select v-model="emb.provider" class="select" @change="onEmbProvider">
            <option v-for="p in embProviders" :key="p.id" :value="p.id">{{ p.provider_name }}</option>
          </select>
          <span class="key-status warn">选择后自动带出该提供商的请求地址与默认模型名</span>
        </label>
        <label class="field">
          <span class="f-label">模型名称（可覆盖提供商默认）</span>
          <input v-model="emb.model_name" class="input mono" placeholder="留空使用提供商默认模型" />
        </label>
        <label class="field">
          <span class="f-label">API Key（租户密钥，必填）</span>
          <input v-model="emb.api_key" type="password" class="input mono" :placeholder="emb.api_key_configured ? '已配置，留空保持' : 'sk-...'" />
          <div v-if="emb.api_key_configured" class="key-row">
            <span class="key-tag">✓ 已配置 {{ emb.api_key_masked || '' }}</span>
            <button class="btn btn-sm" type="button" @click="clearEmbKey">清除</button>
          </div>
          <span v-else class="key-status warn">未配置（租户自定义必须填写 Key）</span>
        </label>
        <label class="field">
          <span class="f-label">API Base URL（可覆盖提供商默认）</span>
          <input v-model="emb.api_base" class="input mono" placeholder="留空使用提供商默认 Base" />
        </label>
        <label class="field">
          <span class="f-label">单价（元/百万tokens）</span>
          <input v-model="emb.emb_price" type="number" min="0" step="0.01" class="input mono" placeholder="0=未定价，用量不计算费用" />
        </label>
      </div>
      <div v-else class="text-3" style="font-size:12px;padding:10px 0 4px;">使用平台默认 Embedding 模型</div>
      <div class="warn-box">
        ⚠️ 修改 Embedding（向量）模型后，知识库已有文档的向量与新的模型不匹配，请到「知识库」页对文档进行<strong>重索引</strong>（支持批量勾选操作）。
      </div>
      <div class="grid4" style="padding:10px;">
        <button v-if="emb.custom" class="btn btn-sm" type="button" :disabled="testing.emb" @click="testModel('embedding')">{{ testing.emb ? '测试中...' : '测试模型可用性' }}</button>
        <span v-if="testResult.emb" class="test-result" :class="testResult.emb.ok ? 'ok' : 'fail'"><span :title="testResult.emb.text">{{ testResult.emb.text }}</span></span>
      </div>
    </div>

    <div style="margin-top:18px;">
      <button class="btn btn-primary" :disabled="saving" @click="save">{{ saving ? '保存中...' : '保存配置' }}</button>
      <span class="text-3" style="font-size:12px;margin-left:12px;">{{ platform ? '平台默认租户: t_admin' : '当前租户: ' + session.tenant_id }}</span>
    </div>
  </div>
</template>

<script setup>
import { reactive, ref, onMounted, watch, computed } from 'vue'
import { api, toast, session } from '../api'

// platform=true 时作为「平台模型配置」页：直接维护平台管理员租户 t_admin 的配置（单数据源）
const props = defineProps({ platform: { type: Boolean, default: false } })

// 平台模式固定操作 t_admin，普通租户模式操作当前登录租户
const cfgURL = computed(() => props.platform ? '/api/admin/tenants/t_admin/model-config' : '/api/admin/model-config')
const pageTenant = computed(() => props.platform ? 't_admin（平台管理员）' : session.tenant_id)

const chat = reactive({ custom: false, provider: '', model_name: '', api_key: '', api_base: '', api_key_configured: false, api_key_masked: '', clearKey: false, chat_in_cache_price: 0, chat_in_price: 0, chat_out_price: 0 })
const emb = reactive({ custom: false, provider: '', model_name: '', api_key: '', api_base: '', api_key_configured: false, api_key_masked: '', clearKey: false, emb_price: 0 })
const chatProviders = ref([])
const embProviders = ref([])
const saving = ref(false)
const testing = reactive({ chat: false, emb: false })
const testResult = reactive({ chat: null, emb: null })

// 用户重新输入 key 时，撤销"清除"标记（避免 clear 与新 key 同时提交）
watch(() => chat.api_key, v => { if (v) chat.clearKey = false })
watch(() => emb.api_key, v => { if (v) emb.clearKey = false })

async function load() {
  let mc = null, ec = null
  try {
    const r = await api(cfgURL.value)
    if (r.code === 0 && r.data) {
      mc = r.data.model_config
      ec = r.data.embedding_config
      chat.custom = props.platform ? true : !!(mc && (mc.model_provider === 'custom' || mc.provider_id > 0))
      chat.provider = mc?.provider_id || 0
      chat.model_name = mc?.model_name || ''
      chat.api_base = mc?.model_api_base || ''
      chat.api_key = ''
      chat.clearKey = false
      chat.api_key_configured = !!mc?.api_key_configured
      chat.api_key_masked = mc?.api_key_masked || ''
      chat.chat_in_cache_price = r.data?.chat_input_cache_price || 0
      chat.chat_in_price = r.data?.chat_input_price || 0
      chat.chat_out_price = r.data?.chat_output_price || 0
      emb.custom = props.platform ? true : !!(ec && (ec.embedding_provider === 'custom' || ec.provider_id > 0))
      emb.provider = ec?.provider_id || 0
      emb.model_name = ec?.embedding_model_name || ''
      emb.api_base = ec?.embedding_api_base || ''
      emb.api_key = ''
      emb.clearKey = false
      emb.api_key_configured = !!ec?.api_key_configured
      emb.api_key_masked = ec?.api_key_masked || ''
      emb.emb_price = r.data?.embedding_price || 0
    }
    const [rc, re] = await Promise.all([
      api('/api/admin/available-model-providers', { params: { type: 'chat' } }),
      api('/api/admin/available-model-providers', { params: { type: 'embedding' } })
    ])
    if (rc.code === 0) chatProviders.value = rc.data || []
    if (re.code === 0) embProviders.value = re.data || []
    // 自动选中第一个提供商
    if (chatProviders.value.length > 0 && !chat.provider) {
      chat.provider = chatProviders.value[0].id
      onChatProvider()
    }
    if (embProviders.value.length > 0 && !emb.provider) {
      emb.provider = embProviders.value[0].id
      onEmbProvider()
    }
  } catch (e) {}
}

function onChatProvider() {
  const p = chatProviders.value.find(x => x.id === chat.provider)
  if (p) {
    chat.model_name = p.default_model || ''
    chat.api_base = p.default_api_base || ''
  }
}
function onEmbProvider() {
  const p = embProviders.value.find(x => x.id === emb.provider)
  if (p) {
    emb.model_name = p.default_model || ''
    emb.api_base = p.default_api_base || ''
  }
}

function clearChatKey() { chat.clearKey = true; chat.api_key = ''; chat.api_key_configured = false; chat.api_key_masked = '' }
function clearEmbKey() { emb.clearKey = true; emb.api_key = ''; emb.api_key_configured = false; emb.api_key_masked = '' }

// 测试模型可用性：用表单当前值调一次真实接口（key 留空时后端回退读已存配置）
async function testModel(type) {
  const f = type === 'chat' ? chat : emb
  if (type === 'chat' && !chat.custom) return
  if (type === 'embedding' && !emb.custom) return
  if (!f.model_name) return toast('请先填写模型名称（或选择提供商自动带出）', 'warning')
  if (!f.api_base) return toast('请先填写 API Base URL（或选择提供商自动带出）', 'warning')
  testing[type === 'chat' ? 'chat' : 'emb'] = true
  testResult[type === 'chat' ? 'chat' : 'emb'] = null
  try {
    const body = { type, model_name: f.model_name, api_base: f.api_base }
    if (f.api_key) body.api_key = f.api_key
    const tid = props.platform ? 't_admin' : session.tenant_id
    const r = await api('/api/admin/model-config/test', { method: 'POST', body, params: { tenant_id: tid } })
    if (r.code !== 0) {
      testResult[type === 'chat' ? 'chat' : 'emb'] = { ok: false, text: r.msg || '请求失败' }
      return
    }
    const d = r.data || {}
    testResult[type === 'chat' ? 'chat' : 'emb'] = d.ok
      ? { ok: true, text: `✓ 可用（${d.latency_ms}ms${d.dim ? '，维度 ' + d.dim : ''}）` }
      : { ok: false, text: `✗ ${d.reason || '不可用'}` }
  } catch (e) {
    testResult[type === 'chat' ? 'chat' : 'emb'] = { ok: false, text: '✗ 请求异常，请稍后重试' }
  } finally {
    testing[type === 'chat' ? 'chat' : 'emb'] = false
  }
}

async function save() {
  saving.value = true
  try {
    const body = {}
    if (chat.custom) {
      if (!chat.provider) return toast('请选择对话模型提供商', 'warning')
      if (!chat.api_key && !chat.api_key_configured) return toast('请填写对话模型 API Key', 'warning')
      body.model_config = { model_provider: 'custom', provider_id: chat.provider, model_name: chat.model_name || '', model_api_base: chat.api_base || '' }
      if (chat.api_key) body.model_config.model_api_key = chat.api_key
      if (chat.clearKey) body.model_config.clear_api_key = true
    } else {
      body.model_config = { model_provider: 'platform' }
    }
    // 单价：跟随平台配置表单也提交（平台默认模型单价=t_admin 行维护）；0=未定价
    body.model_config.chat_input_cache_price = Number(chat.chat_in_cache_price) || 0
    body.model_config.chat_input_price = Number(chat.chat_in_price) || 0
    body.model_config.chat_output_price = Number(chat.chat_out_price) || 0
    if (emb.custom) {
      if (!emb.provider) return toast('请选择 Embedding 模型提供商', 'warning')
      if (!emb.api_key && !emb.api_key_configured) return toast('请填写 Embedding 模型 API Key', 'warning')
      body.embedding_config = { embedding_provider: 'custom', provider_id: emb.provider, embedding_model_name: emb.model_name || '', embedding_api_base: emb.api_base || '' }
      if (emb.api_key) body.embedding_config.embedding_api_key = emb.api_key
      if (emb.clearKey) body.embedding_config.clear_api_key = true
    } else {
      body.embedding_config = { embedding_provider: 'platform' }
    }
    body.embedding_config.embedding_price = Number(emb.emb_price) || 0
    const r = await api(cfgURL.value, { method: 'PUT', body })
    if (r.code !== 0) return toast(r.msg || '保存失败', 'error')
    toast(r.msg || '保存成功，Agent 已重建', 'success')
    load()
  } catch (e) {
  } finally { saving.value = false }
}

onMounted(load)
</script>

<style scoped>
.notice-bar {
  background: var(--c-primary-weak); color: var(--c-primary); border: 1px solid var(--c-border);
  border-radius: var(--radius); padding: 10px 14px; font-size: 13px;
}
.warn-box {
  background: rgba(250, 173, 20, 0.08); border: 1px solid rgba(250, 173, 20, 0.35);
  color: #8a5a00; border-radius: var(--radius); padding: 10px 14px; font-size: 12px;
  margin-top: 12px; line-height: 1.6;
}
.warn-box strong { color: #6b4600; }
.head-row { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; }
.grid2 { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 14px; margin-top: 14px; }
.field { display: flex; flex-direction: column; gap: 6px; }
.f-label { font-size: 12px; color: var(--c-text-2); }
.key-row { display: flex; align-items: center; gap: 8px; }
.key-tag { font-size: 11px; color: var(--c-success); }
.key-status { font-size: 11px; }
.key-status.warn { color: var(--c-warning); }
.test-result { font-size: 12px; font-weight: 500; }
.test-result.ok { color: var(--c-success); }
.test-result.fail { 
  color: var(--c-danger); 
  max-width: 260px;
  display: inline-block; /* 重要！span默认inline，max-width不生效 */
}
.test-result.fail > span {display: inline-block;max-width: 260px;white-space: nowrap;overflow: hidden;text-overflow: ellipsis;vertical-align: middle;}
.switch { position: relative; display: inline-block; width: 40px; height: 22px; }
.switch input { opacity: 0; width: 0; height: 0; }
.slider { position: absolute; cursor: pointer; inset: 0; background: var(--c-border-strong); border-radius: 22px; transition: .2s; }
.slider::before { content: ''; position: absolute; width: 16px; height: 16px; left: 3px; top: 3px; background: #fff; border-radius: 50%; transition: .2s; }
.switch input:checked + .slider { background: var(--c-primary); }
.switch input:checked + .slider::before { transform: translateX(18px); }
</style>
