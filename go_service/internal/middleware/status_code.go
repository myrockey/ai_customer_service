package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// maxBufferSize 缓冲上限：超过则放弃改写直接透传（避免大响应内存占用）
const maxBufferSize = 1 << 20 // 1MB

// statusWriter 包装 gin.ResponseWriter：缓冲响应体，依据 body.code 改写 HTTP 状态码
type statusWriter struct {
	gin.ResponseWriter
	buf        *bytes.Buffer
	written    bool   // 是否已直接写入底层（非 200 或超限）
	statusCode int    // handler 写入的状态码
	limit      int64
}

func (w *statusWriter) WriteHeader(code int) {
	w.statusCode = code
	if code != http.StatusOK {
		// handler 已用真实状态码，直接透传（后续 Write 也直写底层）
		w.written = true
		w.ResponseWriter.WriteHeader(code)
	}
	// code==200 时暂不写，等待 body 判断
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.written {
		return w.ResponseWriter.Write(b)
	}
	if int64(w.buf.Len()+len(b)) > w.limit {
		// 超限：放弃改写，按 200 透传
		w.flushAs(http.StatusOK)
		return w.ResponseWriter.Write(b)
	}
	return w.buf.Write(b)
}

// flushAs 按指定状态码把缓冲内容写入底层
func (w *statusWriter) flushAs(code int) {
	if w.written {
		return
	}
	w.written = true
	if w.statusCode != http.StatusOK && code == http.StatusOK {
		code = w.statusCode
	}
	w.ResponseWriter.WriteHeader(code)
	_, _ = w.ResponseWriter.Write(w.buf.Bytes())
}

// codeToStatus 业务 code → 真实 HTTP 状态码
func codeToStatus(code float64) int {
	switch {
	case code >= 500:
		return http.StatusInternalServerError
	case code >= 400:
		// 400/401/403/404/405/409/429 直接透传；其余 4xx 统一 400
		switch int(code) {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusConflict,
			http.StatusTooManyRequests:
			return int(code)
		default:
			return http.StatusBadRequest
		}
	default:
		return 0 // 不改写
	}
}

// StatusCode 响应状态码归一化中间件（P2-1）：
// 将历史上 handler 返回的 "200 + body.code=4xx/5xx" 规范为真实 HTTP 状态码，
// 业务方仍按 body.code 判断，HTTP 语义同时准确（前端 fetch 可感知 4xx/5xx）。
func StatusCode() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := c.Request.URL.Path
		// WebSocket 升级与 metrics 不适用 JSON 改写
		if strings.HasPrefix(p, "/ws/") || p == "/metrics" {
			c.Next()
			return
		}
		w := &statusWriter{ResponseWriter: c.Writer, buf: &bytes.Buffer{}, statusCode: http.StatusOK, limit: maxBufferSize}
		c.Writer = w
		c.Next()

		if w.written {
			return
		}
		// 解析 body.code 改写状态码；解析失败（非 JSON/无 code）按原状态透传
		var payload struct {
			Code float64 `json:"code"`
		}
		if err := json.Unmarshal(w.buf.Bytes(), &payload); err != nil {
			w.flushAs(http.StatusOK)
			return
		}
		if st := codeToStatus(payload.Code); st > 0 {
			w.flushAs(st)
			return
		}
		w.flushAs(http.StatusOK)
	}
}
