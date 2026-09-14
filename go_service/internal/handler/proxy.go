package handler

import (
	"customer_service/internal/pkg/agent"
	"customer_service/internal/pkg/auth"
	"customer_service/internal/config"
	"customer_service/internal/middleware"
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"github.com/gin-gonic/gin"
)

// proxyToAgent 通用透传：将管理后台请求原样转发给 Python Agent
func ProxyToAgent(c *gin.Context, method, urlPath string) {
	var body io.Reader
	var contentType string
	if c.Request.Body != nil {
		b, err := io.ReadAll(c.Request.Body)
		if err == nil && len(b) > 0 {
			body = bytes.NewReader(b)
			contentType = c.GetHeader("Content-Type")
		}
	}
	data, err := agent.AgentRequest(method, urlPath, c.Request.URL.RawQuery,
		contentType, body, c.GetString(config.TraceIDKey), middleware.TenantOf(c))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "调用agent失败:" + err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

// ProxyPrompt Prompt 管理代理：?scope=platform 时仅平台管理员可操作平台级（全局 tenant_id=''）Prompt，
// 透传特殊租户标识 __platform__ 给 Agent 侧映射为全局作用域；普通租户走原租户隔离逻辑
func ProxyPrompt(c *gin.Context, method, urlPath string) {
	if c.Query("scope") == "platform" {
		info, ok := auth.TenantInfo(middleware.TenantOf(c))
		if !ok || !info.IsPlatformAdmin {
			c.JSON(http.StatusOK, gin.H{"code": 403, "msg": "仅平台管理员可管理平台级（全局）Prompt"})
			return
		}
		var body io.Reader
		var contentType string
		if c.Request.Body != nil {
			b, err := io.ReadAll(c.Request.Body)
			if err == nil && len(b) > 0 {
				body = bytes.NewReader(b)
				contentType = c.GetHeader("Content-Type")
			}
		}
		data, err := agent.AgentRequest(method, urlPath, c.Request.URL.RawQuery,
			contentType, body, c.GetString(config.TraceIDKey), "__platform__")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "调用agent失败:" + err.Error()})
			return
		}
		c.Data(http.StatusOK, "application/json", data)
		return
	}
	ProxyToAgent(c, method, urlPath)
}

// TokenUsageReport P1-1：Token 用量报表
// 平台管理员：可透传 tenant_id/days/group_by 全览所有租户
// 租户管理端：强制绑定自身 tenant_id，防止越权查看其他租户用量
func TokenUsageReport(c *gin.Context) {
	q := c.Request.URL.Query()
	if !auth.AuthEnabled() {
		q.Set("tenant_id", "")
	} else if info, ok := auth.TenantInfo(middleware.TenantOf(c)); !ok || !info.IsPlatformAdmin {
		q.Set("tenant_id", middleware.TenantOf(c))
	}
	data, err := agent.AgentRequest(http.MethodGet, "/agent/token-usage", q.Encode(),
		"", nil, c.GetString(config.TraceIDKey), middleware.TenantOf(c))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "调用agent失败:" + err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

// proxyKbUpload 知识库文件上传：支持 multipart(file+title) 与 JSON(title+content)
func ProxyKbUpload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err == nil && file != nil {
		title := c.PostForm("title")
		category := c.PostForm("category")
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, err := mw.CreateFormFile("file", file.Filename)
		if err == nil {
			src, err := file.Open()
			if err == nil {
				_, _ = io.Copy(fw, src)
				_ = src.Close()
			}
		}
		_ = mw.WriteField("title", title)
		_ = mw.WriteField("category", category)
		_ = mw.Close()

		data, err := agent.AgentRequest(http.MethodPost, "/agent/kb/upload", "",
			mw.FormDataContentType(), &buf, c.GetString(config.TraceIDKey), middleware.TenantOf(c))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "调用agent失败:" + err.Error()})
			return
		}
		c.Data(http.StatusOK, "application/json", data)
		return
	}
	// 非文件上传：按 JSON 透传
	ProxyToAgent(c, http.MethodPost, "/agent/kb/upload")
}
