package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Prometheus metrics 指标定义
var (
	// HTTP 请求总数（按 method/path/status 分类）
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cs_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	// HTTP 请求延迟（直方图）
	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cs_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	// 当前活跃 WebSocket 连接数
	wsConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "cs_websocket_connections",
			Help: "Current active WebSocket connections",
		},
	)

	// 当前活跃用户会话数
	activeSessions = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "cs_active_sessions",
			Help: "Current active user sessions",
		},
	)

	// Agent 调用次数
	agentCallsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cs_agent_calls_total",
			Help: "Total number of Agent calls",
		},
		[]string{"endpoint", "status"},
	)

	// Agent 调用延迟
	agentCallDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cs_agent_call_duration_seconds",
			Help:    "Agent call duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"endpoint"},
	)
)

// PrometheusMetrics Prometheus 指标收集中间件
func PrometheusMetrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())

		// 记录请求数和延迟
		httpRequestsTotal.WithLabelValues(c.Request.Method, path, status).Inc()
		httpRequestDuration.WithLabelValues(c.Request.Method, path).Observe(duration)
	}
}

// IncWSConnections 增加 WebSocket 连接数
func IncWSConnections() {
	wsConnections.Inc()
}

// DecWSConnections 减少 WebSocket 连接数
func DecWSConnections() {
	wsConnections.Dec()
}

// SetActiveSessions 设置活跃会话数
func SetActiveSessions(n float64) {
	activeSessions.Set(n)
}

// IncAgentCalls 增加 Agent 调用次数
func IncAgentCalls(endpoint string, success bool) {
	status := "success"
	if !success {
		status = "error"
	}
	agentCallsTotal.WithLabelValues(endpoint, status).Inc()
}

// ObserveAgentCallDuration 记录 Agent 调用延迟
func ObserveAgentCallDuration(endpoint string, duration float64) {
	agentCallDuration.WithLabelValues(endpoint).Observe(duration)
}
