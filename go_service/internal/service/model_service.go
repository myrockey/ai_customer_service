package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"customer_service/internal/pkg/auth"
	"customer_service/internal/config"
	"customer_service/internal/pkg/crypto"
	"customer_service/internal/model"
)

// AgentModelConfigResult Agent 获取租户模型配置结果（含明文API Key）
type AgentModelConfigResult struct {
	TenantID      string `json:"tenant_id"`
	UseCustom     bool   `json:"use_custom"`
	ModelProvider string `json:"model_provider"`
	ModelName     string `json:"model_name"`
	APIKey        string `json:"api_key"` // 明文，仅内网 Agent 调用
	APIBase       string `json:"api_base"`
	// 模型单价（元/百万 tokens）：chat 输入分「命中缓存/未命中」两档 + 输出；embedding（0=未定价，费用不参与统计）
	ChatInputCachePrice float64 `json:"chat_input_cache_price"`
	ChatInputPrice      float64 `json:"chat_input_price"`
	ChatOutputPrice     float64 `json:"chat_output_price"`
	EmbeddingPrice      float64 `json:"embedding_price"`
	// ConfigVersion 配置签名（chat+embedding 相关字段的 hash）。
	// Agent 侧懒检查：广播丢失时对比签名发现配置变化并重建，兜底最终一致。
	ConfigVersion string `json:"config_version"`
	Embedding     struct {
		UseCustom bool   `json:"use_custom"`
		Provider  string `json:"provider"`
		ModelName string `json:"model_name"`
		APIKey    string `json:"api_key"` // 明文，仅内网 Agent 调用
		APIBase   string `json:"api_base"`
	} `json:"embedding"`
}

// configVersionOf 计算配置签名：chat + embedding 的全部相关字段（含明文 key，改 key 也触发重建）
func configVersionOf(r *AgentModelConfigResult) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s", r.ModelProvider, r.ModelName, r.APIKey, r.APIBase)
	fmt.Fprintf(h, "|%s|%s|%s|%s",
		r.Embedding.Provider, r.Embedding.ModelName, r.Embedding.APIKey, r.Embedding.APIBase)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// loadProviderByID 加载提供商（启用状态）
func loadProviderByID(id uint64) (*model.ModelProvider, error) {
	var p model.ModelProvider
	if id == 0 {
		return nil, fmt.Errorf("provider id 为空")
	}
	if err := config.DB.First(&p, id).Error; err != nil {
		return nil, err
	}
	if p.Status != 1 {
		return nil, fmt.Errorf("提供商已停用")
	}
	return &p, nil
}

// firstEnabledProvider 兜底：取第一个启用的指定类型提供商（仅元信息，无 Key 概念）
func firstEnabledProvider(typ string) (*model.ModelProvider, error) {
	var p model.ModelProvider
	if err := config.DB.Where("status = 1 AND (provider_type = ? OR provider_type = 'both')", typ).
		Order("sort_order ASC, id ASC").First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// decryptKey 解密 AES 密文 Key（空返回空串）
func decryptKey(enc string) string {
	k, _ := crypto.DecryptAPIKey(enc)
	return k
}

// resolveChatConfig 解析租户最终生效的对话模型配置（递归处理"跟随平台"）：
//  Key 唯一来源 = tenants.model_api_key（AES 密文，租户自身密钥）；model_providers 仅提供名称/Base/默认模型名
//  1. llm_provider_id > 0 → 使用 provider 行 default_model/default_api_base，租户字段非空则覆盖；Key 取本行 model_api_key
//     （本行 Key 为空 = 配置不完整 → 视为未配置，继续跟随平台）
//  2. 无 provider_id 但 model_provider='custom' → 兼容旧版手填字段（Key 与模型名/Base 齐全才有效）
//  3. 跟随平台（platform）→ 递归解析平台管理员租户 t_admin
//  4. t_admin 也未配置 → 取第一个启用的对话提供商兜底（Key 从 t_admin 行取，空则报错提示后台配置）
func resolveChatConfig(tid string, depth int) (providerKey, modelName, apiBase, apiKey string, err error) {
	if depth > 3 {
		return "", "", "", "", fmt.Errorf("模型配置解析递归过深（跟随平台循环）")
	}
	var row model.TenantRow
	if err := config.DB.Where("tenant_id = ?", tid).First(&row).Error; err != nil {
		return "", "", "", "", err
	}
	// 1. provider_id 优先（Key 必须来自租户本行，为空视为未配置）
	if row.LlmProviderID > 0 {
		p, e := loadProviderByID(row.LlmProviderID)
		if e == nil {
			m, b := p.DefaultModel, p.DefaultAPIBase
			if row.ModelName != "" {
				m = row.ModelName
			}
			if row.ModelAPIBase != "" {
				b = row.ModelAPIBase
			}
			k := decryptKey(row.ModelAPIKey)
			if k != "" {
				return p.ProviderKey, m, b, k, nil
			}
			// 本行无 Key → 视为未配置，继续后续分支
		}
	}
	// 2. 兼容旧数据：手填 custom（必须 Key 与模型名/Base 齐全才视为有效配置，否则视为未配置→跟随平台）
	if row.ModelProvider == "custom" {
		k := decryptKey(row.ModelAPIKey)
		if k != "" && (row.ModelName != "" || row.ModelAPIBase != "") {
			return row.ModelProviderKey, row.ModelName, row.ModelAPIBase, k, nil
		}
	}
	// 3. 跟随平台 → 平台管理员租户
	if row.TenantID != "t_admin" {
		return resolveChatConfig("t_admin", depth+1)
	}
	// 4. t_admin 兜底：取第一个启用的提供商（Key 从 t_admin 行取）
	p, e := firstEnabledProvider("chat")
	if e != nil {
		return "", "", "", "", fmt.Errorf("未配置默认对话模型提供商，请在「模型提供商」中启用至少一个")
	}
	k := decryptKey(row.ModelAPIKey)
	if k == "" {
		return "", "", "", "", fmt.Errorf("平台默认对话模型未配置 API Key，请以平台管理员登录后在「平台模型配置」中配置（Key 存于 t_admin 租户）")
	}
	return p.ProviderKey, p.DefaultModel, p.DefaultAPIBase, k, nil
}

// resolveEmbConfig 解析租户最终生效的 Embedding 模型配置（逻辑同 resolveChatConfig）
func resolveEmbConfig(tid string, depth int) (providerKey, modelName, apiBase, apiKey string, err error) {
	if depth > 3 {
		return "", "", "", "", fmt.Errorf("模型配置解析递归过深（跟随平台循环）")
	}
	var row model.TenantRow
	if err := config.DB.Where("tenant_id = ?", tid).First(&row).Error; err != nil {
		return "", "", "", "", err
	}
	if row.EmbeddingProviderID > 0 {
		p, e := loadProviderByID(row.EmbeddingProviderID)
		if e == nil {
			m, b := p.DefaultModel, p.DefaultAPIBase
			if row.EmbeddingModelName != "" {
				m = row.EmbeddingModelName
			}
			if row.EmbeddingAPIBase != "" {
				b = row.EmbeddingAPIBase
			}
			k := decryptKey(row.EmbeddingAPIKey)
			if k != "" {
				return p.ProviderKey, m, b, k, nil
			}
			// 本行无 Key → 视为未配置，继续后续分支
		}
	}
	if row.EmbeddingProvider == "custom" {
		k := decryptKey(row.EmbeddingAPIKey)
		if k != "" && (row.EmbeddingModelName != "" || row.EmbeddingAPIBase != "") {
			return row.EmbeddingProviderKey, row.EmbeddingModelName, row.EmbeddingAPIBase, k, nil
		}
	}
	if row.TenantID != "t_admin" {
		return resolveEmbConfig("t_admin", depth+1)
	}
	// 4. t_admin 兜底：取第一个启用的提供商（Key 从 t_admin 行取）
	p, e := firstEnabledProvider("embedding")
	if e != nil {
		return "", "", "", "", fmt.Errorf("未配置默认 Embedding 模型提供商，请在「模型提供商」中启用至少一个")
	}
	k := decryptKey(row.EmbeddingAPIKey)
	if k == "" {
		return "", "", "", "", fmt.Errorf("平台默认 Embedding 模型未配置 API Key，请以平台管理员登录后在「平台模型配置」中配置（Key 存于 t_admin 租户）")
	}
	return p.ProviderKey, p.DefaultModel, p.DefaultAPIBase, k, nil
}

// GetAgentModelConfig Agent 获取租户模型配置（内部接口，返回解密后的明文）
// 数据源唯一：model_providers（提供商默认配置）+ tenants（provider_id 关联与租户覆盖），
// 平台默认 = t_admin 租户的配置；不再回退 .env（AGNES_* 键已废弃）
func GetAgentModelConfig(tid string) (*AgentModelConfigResult, error) {
	var row model.TenantRow
	if err := config.DB.Where("tenant_id = ?", tid).First(&row).Error; err != nil {
		return nil, err
	}
	chatKey, chatModel, chatBase, chatAPIKey, err := resolveChatConfig(tid, 0)
	if err != nil {
		return nil, err
	}
	embKey, embModel, embBase, embAPIKey, err := resolveEmbConfig(tid, 0)
	if err != nil {
		return nil, err
	}

	// useCustom 语义：该租户是否实际使用了自身配置（provider 关联 或 手填 key 齐全，Key 均取本行 model_api_key）；
	// 配置不完整（历史脏数据）视为未配置 → 跟随平台默认
	chatOwn := decryptKey(row.ModelAPIKey) != "" &&
		(row.LlmProviderID > 0 || (row.ModelProvider == "custom" && (row.ModelName != "" || row.ModelAPIBase != "")))
	embOwn := decryptKey(row.EmbeddingAPIKey) != "" &&
		(row.EmbeddingProviderID > 0 || (row.EmbeddingProvider == "custom" && (row.EmbeddingModelName != "" || row.EmbeddingAPIBase != "")))

	result := &AgentModelConfigResult{
		TenantID:            tid,
		UseCustom:           chatOwn,
		ModelProvider:       chatKey,
		ModelName:           chatModel,
		APIKey:              chatAPIKey,
		APIBase:             chatBase,
		ChatInputCachePrice: row.ChatInputCachePrice,
		ChatInputPrice:      row.ChatInputPrice,
		ChatOutputPrice:     row.ChatOutputPrice,
		EmbeddingPrice:      row.EmbeddingPrice,
	}
	// 跟随平台（未自定义）时单价取平台默认：平台默认模型单价维护在 t_admin 行
	if !chatOwn && tid != "t_admin" {
		var plat model.TenantRow
		if err := config.DB.Where("tenant_id = ?", "t_admin").First(&plat).Error; err == nil {
			result.ChatInputCachePrice = plat.ChatInputCachePrice
			result.ChatInputPrice = plat.ChatInputPrice
			result.ChatOutputPrice = plat.ChatOutputPrice
		}
	}
	if !embOwn && tid != "t_admin" {
		var plat model.TenantRow
		if err := config.DB.Where("tenant_id = ?", "t_admin").First(&plat).Error; err == nil {
			result.EmbeddingPrice = plat.EmbeddingPrice
		}
	}
	result.Embedding.UseCustom = embOwn
	result.Embedding.Provider = embKey
	result.Embedding.ModelName = embModel
	result.Embedding.APIKey = embAPIKey
	result.Embedding.APIBase = embBase
	result.ConfigVersion = configVersionOf(result)
	return result, nil
}

// BusinessModelConfigResult 租户自配模型查询结果（脱敏，不返回明文API Key）
type BusinessModelConfigResult struct {
	TenantID string `json:"tenant_id"`
	// 模型单价（元/百万tokens，0=未定价）：回显给配置表单维护
	ChatInputCachePrice float64 `json:"chat_input_cache_price"`
	ChatInputPrice      float64 `json:"chat_input_price"`
	ChatOutputPrice     float64 `json:"chat_output_price"`
	EmbeddingPrice      float64 `json:"embedding_price"`
	ModelConfig  struct {
		ModelProvider    string `json:"model_provider"`
		ProviderID       uint64 `json:"provider_id"` // 关联 model_providers.id（0=未选）
		ProviderKey      string `json:"provider_key"` // 模型提供商标识（custom 时按模型名/BaseURL 推断）
		ModelName        string `json:"model_name"`
		ModelAPIBase     string `json:"model_api_base"`
		APIKeyConfigured bool   `json:"api_key_configured"`
		APIKeyMasked     string `json:"api_key_masked"`
	} `json:"model_config"`
	EmbeddingConfig struct {
		EmbeddingProvider   string `json:"embedding_provider"`
		ProviderID          uint64 `json:"provider_id"` // 关联 model_providers.id（0=未选）
		ProviderKey         string `json:"provider_key"` // 模型提供商标识（custom 时按模型名/BaseURL 推断）
		EmbeddingModelName  string `json:"embedding_model_name"`
		EmbeddingAPIBase    string `json:"embedding_api_base"`
		APIKeyConfigured    bool   `json:"api_key_configured"`
		APIKeyMasked        string `json:"api_key_masked"`
	} `json:"embedding_config"`
}

// inferProviderKey 根据模型名/API Base 推断提供商标识（优先存值 > 默认模型精确匹配 > BaseURL 匹配 > custom）
func inferProviderKey(storedKey, modelName, apiBase string) string {
	if storedKey != "" {
		return storedKey
	}
	if modelName == "" && apiBase == "" {
		return "custom"
	}
	var p model.ModelProvider
	// 优先默认模型精确匹配
	if err := config.DB.Where("status = 1 AND provider_key != 'custom' AND default_model = ?", modelName).
		Order("sort_order ASC, id ASC").First(&p).Error; err == nil && p.ProviderKey != "" {
		return p.ProviderKey
	}
	// 其次 API Base 匹配
	if apiBase != "" {
		if err := config.DB.Where("status = 1 AND provider_key != 'custom' AND default_api_base = ?", apiBase).
			Order("sort_order ASC, id ASC").First(&p).Error; err == nil && p.ProviderKey != "" {
			return p.ProviderKey
		}
	}
	return "custom"
}

// GetBusinessModelConfig 获取当前租户模型配置（脱敏返回）
func GetBusinessModelConfig(tid string) (*BusinessModelConfigResult, error) {
	var row model.TenantRow
	if err := config.DB.Where("tenant_id = ?", tid).First(&row).Error; err != nil {
		return nil, err
	}
	apiKeyPlain, _ := crypto.DecryptAPIKey(row.ModelAPIKey)
	embKeyPlain, _ := crypto.DecryptAPIKey(row.EmbeddingAPIKey)

	result := &BusinessModelConfigResult{TenantID: tid}
	result.ChatInputCachePrice = row.ChatInputCachePrice
	result.ChatInputPrice = row.ChatInputPrice
	result.ChatOutputPrice = row.ChatOutputPrice
	result.EmbeddingPrice = row.EmbeddingPrice
	result.ModelConfig.ModelProvider = row.ModelProvider
	result.ModelConfig.ProviderID = row.LlmProviderID
	result.ModelConfig.ProviderKey = inferProviderKey(row.ModelProviderKey, row.ModelName, row.ModelAPIBase)
	result.ModelConfig.ModelName = row.ModelName
	result.ModelConfig.ModelAPIBase = row.ModelAPIBase
	result.ModelConfig.APIKeyConfigured = row.ModelAPIKey != ""
	result.ModelConfig.APIKeyMasked = crypto.MaskAPIKey(apiKeyPlain)

	result.EmbeddingConfig.EmbeddingProvider = row.EmbeddingProvider
	result.EmbeddingConfig.ProviderID = row.EmbeddingProviderID
	result.EmbeddingConfig.ProviderKey = inferProviderKey(row.EmbeddingProviderKey, row.EmbeddingModelName, row.EmbeddingAPIBase)
	result.EmbeddingConfig.EmbeddingModelName = row.EmbeddingModelName
	result.EmbeddingConfig.EmbeddingAPIBase = row.EmbeddingAPIBase
	result.EmbeddingConfig.APIKeyConfigured = row.EmbeddingAPIKey != ""
	result.EmbeddingConfig.APIKeyMasked = crypto.MaskAPIKey(embKeyPlain)
	return result, nil
}

// UpdateModelConfigInput 更新模型配置输入
type UpdateModelConfigInput struct {
	ModelConfig *struct {
		ModelProvider   string  `json:"model_provider"`
		ProviderID      uint64  `json:"provider_id"` // 关联 model_providers.id（0=未选）
		ProviderKey     string  `json:"provider_key"`
		ModelName       string  `json:"model_name"`
		ModelAPIKey     string  `json:"model_api_key"`
		ModelAPIBase    string  `json:"model_api_base"`
		ClearAPIKey     bool    `json:"clear_api_key"`
		ChatInputCachePrice float64 `json:"chat_input_cache_price"` // 元/百万 tokens，0=未定价
		ChatInputPrice  float64 `json:"chat_input_price"`  // 元/百万 tokens（未命中缓存），0=未定价
		ChatOutputPrice float64 `json:"chat_output_price"`
	} `json:"model_config"`
	EmbeddingConfig *struct {
		EmbeddingProvider  string  `json:"embedding_provider"`
		ProviderID         uint64  `json:"provider_id"` // 关联 model_providers.id（0=未选）
		ProviderKey        string  `json:"provider_key"`
		EmbeddingModelName string  `json:"embedding_model_name"`
		EmbeddingAPIKey    string  `json:"embedding_api_key"`
		EmbeddingAPIBase   string  `json:"embedding_api_base"`
		ClearAPIKey        bool    `json:"clear_api_key"`
		EmbeddingPrice     float64 `json:"embedding_price"` // 元/百万 tokens（0=未定价）
	} `json:"embedding_config"`
}

// UpdateModelConfig 更新当前租户模型配置（加密存储+刷新租户缓存）
// 关键：逐字段与数据库现值比较，只有【真正变化】的字段才计入 updates，
// 保证 ModelUpdateScope 能精确判断 chat/embedding 变更范围（避免前端全量提交导致 scope=all）
func UpdateModelConfig(tid string, input *UpdateModelConfigInput) (map[string]interface{}, error) {
	var row model.TenantRow
	if err := config.DB.Where("tenant_id = ?", tid).First(&row).Error; err != nil {
		return nil, err
	}
	updates := map[string]interface{}{}

	// 对话模型配置
	if input.ModelConfig != nil {
		mc := input.ModelConfig
		if mc.ModelProvider != "" && mc.ModelProvider != row.ModelProvider {
			updates["model_provider"] = mc.ModelProvider
		}
		// provider_id：custom 必须携带所选 provider_id；跟随平台（platform）不传则清 0
		// （0 表示未关联提供商：custom 时走手填字段，platform 时跟随平台默认）
		if mc.ProviderID != row.LlmProviderID {
			updates["llm_provider_id"] = mc.ProviderID
		}
		if mc.ProviderKey != "" && mc.ProviderKey != row.ModelProviderKey {
			updates["model_provider_key"] = mc.ProviderKey
		}
		// custom（含 provider 关联）必须提供 API Key（未提供且未清除时拒绝，避免静默回退平台默认）
		if mc.ModelProvider == "custom" && mc.ModelAPIKey == "" && !mc.ClearAPIKey {
			existingKey, _ := crypto.DecryptAPIKey(row.ModelAPIKey)
			if existingKey == "" {
				return nil, fmt.Errorf("自定义对话模型必须填写 API Key")
			}
		}
		if mc.ModelName != "" && mc.ModelName != row.ModelName {
			updates["model_name"] = mc.ModelName
		}
		// 单价：元/百万 tokens（0=未定价；前端提交当前值，逐字段比较避免误更新）
		if mc.ChatInputCachePrice != row.ChatInputCachePrice {
			updates["chat_input_cache_price"] = mc.ChatInputCachePrice
		}
		if mc.ChatInputPrice != row.ChatInputPrice {
			updates["chat_input_price"] = mc.ChatInputPrice
		}
		if mc.ChatOutputPrice != row.ChatOutputPrice {
			updates["chat_output_price"] = mc.ChatOutputPrice
		}
		if mc.ModelAPIBase != "" && mc.ModelAPIBase != row.ModelAPIBase {
			updates["model_api_base"] = mc.ModelAPIBase
		}
		// API Key：显式清除 → 清空；提交新值 → 与解密现值比较（掩码视为未修改）
		if mc.ClearAPIKey {
			if row.ModelAPIKey != "" {
				updates["model_api_key"] = ""
			}
		} else if mc.ModelAPIKey != "" && !isMaskedKey(mc.ModelAPIKey) {
			existingKey, _ := crypto.DecryptAPIKey(row.ModelAPIKey)
			if mc.ModelAPIKey != existingKey {
				encrypted, err := crypto.EncryptAPIKey(mc.ModelAPIKey)
				if err != nil {
					return nil, err
				}
				updates["model_api_key"] = encrypted
			}
		}
	}

	// Embedding 模型配置
	if input.EmbeddingConfig != nil {
		ec := input.EmbeddingConfig
		if ec.EmbeddingProvider != "" && ec.EmbeddingProvider != row.EmbeddingProvider {
			updates["embedding_provider"] = ec.EmbeddingProvider
		}
		if ec.ProviderID != row.EmbeddingProviderID {
			updates["embedding_provider_id"] = ec.ProviderID
		}
		if ec.ProviderKey != "" && ec.ProviderKey != row.EmbeddingProviderKey {
			updates["embedding_provider_key"] = ec.ProviderKey
		}
		// custom（含 provider 关联）必须提供 API Key
		if ec.EmbeddingProvider == "custom" && ec.EmbeddingAPIKey == "" && !ec.ClearAPIKey {
			existingKey, _ := crypto.DecryptAPIKey(row.EmbeddingAPIKey)
			if existingKey == "" {
				return nil, fmt.Errorf("自定义 Embedding 模型必须填写 API Key")
			}
		}
		if ec.EmbeddingModelName != "" && ec.EmbeddingModelName != row.EmbeddingModelName {
			updates["embedding_model_name"] = ec.EmbeddingModelName
		}
		// embedding 单价（元/百万 tokens）
		if ec.EmbeddingPrice != row.EmbeddingPrice {
			updates["embedding_price"] = ec.EmbeddingPrice
		}
		if ec.EmbeddingAPIBase != "" && ec.EmbeddingAPIBase != row.EmbeddingAPIBase {
			updates["embedding_api_base"] = ec.EmbeddingAPIBase
		}
		// API Key：显式清除 → 清空；提交新值 → 与解密现值比较（掩码视为未修改）
		if ec.ClearAPIKey {
			if row.EmbeddingAPIKey != "" {
				updates["embedding_api_key"] = ""
			}
		} else if ec.EmbeddingAPIKey != "" && !isMaskedKey(ec.EmbeddingAPIKey) {
			existingKey, _ := crypto.DecryptAPIKey(row.EmbeddingAPIKey)
			if ec.EmbeddingAPIKey != existingKey {
				encrypted, err := crypto.EncryptAPIKey(ec.EmbeddingAPIKey)
				if err != nil {
					return nil, err
				}
				updates["embedding_api_key"] = encrypted
			}
		}
	}

	if len(updates) == 0 {
		return nil, nil // 没有需要更新的字段
	}

	if err := config.DB.Model(&row).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := auth.RefreshTenants(); err != nil {
		return nil, err
	}
	return updates, nil
}

// isMaskedKey 判断前端提交的 API Key 是否为掩码（如 "****" / "sk-***"），掩码视为未修改
func isMaskedKey(k string) bool {
	return strings.Contains(k, "*")
}

// ModelUpdateScope 根据实际更新的字段判断模型变更范围，用于精确清 Agent 缓存：
//   - 只改对话模型 → "chat"（向量缓存不受影响）
//   - 只改向量模型 → "embedding"（对话缓存不受影响）
//   - 两者都改/无法判断 → "all"
func ModelUpdateScope(updates map[string]interface{}) string {
	hasChat, hasEmb := false, false
	for k := range updates {
		if strings.HasPrefix(k, "model_") || k == "llm_provider_id" || strings.HasPrefix(k, "chat_") {
			hasChat = true
		}
		if strings.HasPrefix(k, "embedding_") || k == "embedding_provider_id" {
			hasEmb = true
		}
	}
	if hasChat && !hasEmb {
		return "chat"
	}
	if !hasChat && hasEmb {
		return "embedding"
	}
	return "all"
}
