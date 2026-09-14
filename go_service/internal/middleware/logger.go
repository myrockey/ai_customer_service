package middleware

import (
	"time"

	"customer_service/internal/config"
	"customer_service/internal/pkg/logger"

	"github.com/gin-gonic/gin"
)

// RequestLogger HTTP 请求日志中间件
// 记录每个请求的 method、path、status、duration、trace_id、tenant_id
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		// 处理请求
		c.Next()

		// 计算耗时
		duration := time.Since(start)
		status := c.Writer.Status()
		traceID := c.GetString(config.TraceIDKey)
		tenantID, _ := c.Get("tenant_id")

		// 构建日志字段
		fields := logger.Fields{
			"method":   c.Request.Method,
			"path":     path,
			"query":    query,
			"status":   status,
			"duration": duration.Milliseconds(),
			"client_ip": c.ClientIP(),
		}

		if traceID != "" {
			fields["trace_id"] = traceID
		}
		if tenantID != nil && tenantID != "" {
			fields["tenant_id"] = tenantID
		}

		// 根据状态码选择日志级别
		switch {
		case status >= 500:
			logger.Error("HTTP请求失败", fields)
		case status >= 400:
			logger.Warn("HTTP请求客户端错误", fields)
		default:
			logger.Info("HTTP请求完成", fields)
		}
	}
}
