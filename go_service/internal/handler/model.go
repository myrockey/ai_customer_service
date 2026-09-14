package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"customer_service/internal/middleware"
	"customer_service/internal/pkg/cache"
	"customer_service/internal/service"

	"github.com/gin-gonic/gin"
)

// ============ 模型配置接口 ============
// P4-2：引入 service 层，handler 只负责参数解析和响应返回

// agentModelConfig Agent 获取租户模型配置（内部接口，返回解密后的明文）
// GET /api/agent/model-config?tenant_id=xxx
// 供 Agent 服务调用，决定用平台默认模型还是租户自定义模型
func AgentModelConfig(c *gin.Context) {
	tid := c.Query("tenant_id")
	if tid == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "tenant_id 不能为空"})
		return
	}
	result, err := service.GetAgentModelConfig(tid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "租户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// ============ 租户自配模型（业务接口，普通租户自己配置） ============

// businessModelConfig 获取当前租户模型配置：GET /api/business/model-config
func BusinessModelConfig(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusOK, gin.H{"code": 401, "msg": "未登录或 token 无效"})
		return
	}
	result, err := service.GetBusinessModelConfig(tid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "租户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// businessModelConfigUpdate 更新当前租户模型配置：PUT /api/business/model-config
func BusinessModelConfigUpdate(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusOK, gin.H{"code": 401, "msg": "未登录或 token 无效"})
		return
	}
	var input service.UpdateModelConfigInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	updates, err := service.UpdateModelConfig(tid, &input)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	if updates == nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "没有需要更新的字段"})
		return
	}
	// 通知 Agent 清除该租户模型配置缓存（按变更范围精确清除：chat/embedding/all），使新配置立即生效（失败不影响主流程）
	// 强制 Redis 广播：单实例自订阅自消费，多实例全量消费；发布失败仅记日志
	go func() {
		scope := service.ModelUpdateScope(updates)
		if err := cache.Publish("cs:model:cache:invalidate", fmt.Sprintf(`{"tenant_id":%q,"scope":%q}`, tid, scope)); err != nil {
			log.Printf("⚠️ Redis 发布缓存失效广播失败: %v", err)
		}
	}()
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "模型配置已保存", "data": gin.H{
		"tenant_id": tid, "updated": updates,
	}})
}

// adminModelTest 平台管理员测试模型可用性（仅测试，不落库）：POST /api/admin/model-config/test
// 用表单提交的模型名/Base/Key 做一次真实调用，返回 ok/latency/dim 或具体失败原因；
// 表单留空的字段（如已配置的掩码 Key）自动回退读取已存租户配置（默认 t_admin，可用 ?tenant_id= 指定）
func AdminModelTest(c *gin.Context) {
	var req struct {
		Type        string `json:"type" binding:"required"` // chat | embedding
		ProviderKey string `json:"provider_key"`
		ModelName   string `json:"model_name"`
		APIBase     string `json:"api_base"`
		APIKey      string `json:"api_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	// 表单留空时回退读取已存租户配置（明文），便于"已配置 Key 掩码回显"场景直接测试
	if req.ModelName == "" || req.APIBase == "" || req.APIKey == "" {
		tid := c.Query("tenant_id")
		if tid == "" {
			tid = "t_admin"
		}
		if cfg, err := service.GetAgentModelConfig(tid); err == nil {
			// 最小改动：仅租户启用自有自定义配置，才回填
			if cfg.UseCustom {
				if req.Type == "chat" {
					if req.ModelName == "" {
						req.ModelName = cfg.ModelName
					}
					if req.APIBase == "" {
						req.APIBase = cfg.APIBase
					}
					if req.APIKey == "" {
						req.APIKey = cfg.APIKey
					}
				} else {
					if req.ModelName == "" {
						req.ModelName = cfg.Embedding.ModelName
					}
					if req.APIBase == "" {
						req.APIBase = cfg.Embedding.APIBase
					}
					if req.APIKey == "" {
						req.APIKey = cfg.Embedding.APIKey
					}
				}
			}
		}
	}
	if req.APIKey == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "请填写 API Key 后再测试（或该租户尚未配置 API Key）"})
		return
	}
	if req.ModelName == "" || req.APIBase == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "模型名称与 API Base 不能为空（表单留空且未读到已存配置）"})
		return
	}
	start := time.Now()
	var url, payload string
	switch req.Type {
	case "chat":
		url = strings.TrimRight(req.APIBase, "/") + "/chat/completions"
		payload = fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"ping"}],"max_tokens":5}`, req.ModelName)
	case "embedding":
		url = strings.TrimRight(req.APIBase, "/") + "/embeddings"
		payload = fmt.Sprintf(`{"model":%q,"input":"test"}`, req.ModelName)
	default:
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "type 必须是 chat 或 embedding"})
		return
	}
	httpReq, err := http.NewRequest(http.MethodPost, url, strings.NewReader(payload))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "构造请求失败: " + err.Error()})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(httpReq)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"ok": false, "reason": "连接失败: " + err.Error()}})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	latency := time.Since(start).Milliseconds()
	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"ok": false, "reason": fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))}})
		return
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"ok": false, "reason": "响应解析失败: " + truncateStr(string(body), 200)}})
		return
	}
	if req.Type == "chat" {
		choices, _ := parsed["choices"].([]interface{})
		if len(choices) == 0 {
			c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"ok": false, "reason": "响应无 choices 字段，模型可能不支持该接口"}})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"ok": true, "latency_ms": latency}})
		return
	}
	// embedding：校验 data[0].embedding 并返回维度
	dataArr, _ := parsed["data"].([]interface{})
	if len(dataArr) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"ok": false, "reason": "响应无 data 字段，模型可能不支持该接口"}})
		return
	}
	first, _ := dataArr[0].(map[string]interface{})
	emb, _ := first["embedding"].([]interface{})
	if len(emb) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"ok": false, "reason": "embedding 结果为空"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"ok": true, "latency_ms": latency, "dim": len(emb)}})
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
