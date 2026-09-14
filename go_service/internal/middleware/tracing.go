package middleware

import (
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/gin-gonic/gin"
)

const tracerName = "customer-service-gin"

// OpenTelemetry Gin 中间件，自动追踪 HTTP 请求
func Tracing() gin.HandlerFunc {
	tracer := otel.Tracer(tracerName)
	propagator := otel.GetTextMapPropagator()

	return func(c *gin.Context) {
		// 从请求头中提取 trace context（W3C TraceContext）
		ctx := propagator.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))

		// 开始 span
		spanName := c.Request.Method + " " + c.FullPath()
		if spanName == "" {
			spanName = c.Request.Method + " " + c.Request.URL.Path
		}

		ctx, span := tracer.Start(
			ctx,
			spanName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPMethod(c.Request.Method),
				semconv.HTTPURL(c.Request.URL.String()),
				semconv.HTTPScheme(c.Request.URL.Scheme),
				semconv.NetHostName(c.Request.Host),
				attribute.String("http.client_ip", c.ClientIP()),
			),
		)
		defer span.End()

		// 将 trace context 注入到请求上下文
		c.Request = c.Request.WithContext(ctx)

		// 记录请求开始时间
		startTime := time.Now()

		// 将 trace_id 写入 gin context，方便日志使用
		spanCtx := trace.SpanContextFromContext(ctx)
		if spanCtx.IsValid() {
			c.Set("trace_id", spanCtx.TraceID().String())
			c.Set("span_id", spanCtx.SpanID().String())
			// 同时设置到 X-Trace-Id 响应头
			c.Header("X-Trace-Id", spanCtx.TraceID().String())
		}

		// 处理请求
		c.Next()

		// 设置响应状态码和延迟
		status := c.Writer.Status()
		duration := time.Since(startTime)

		span.SetAttributes(
			semconv.HTTPStatusCode(status),
			attribute.Float64("http.duration_ms", float64(duration.Milliseconds())),
		)

		// 根据状态码设置 span 状态
		if status >= 500 {
			span.SetStatus(codes.Error, "HTTP 5xx error")
		} else if status >= 400 {
			span.SetStatus(codes.Error, "HTTP 4xx error")
		} else {
			span.SetStatus(codes.Ok, "")
		}

		// 如果有错误，记录到 span
		if len(c.Errors) > 0 {
			span.SetAttributes(
				attribute.String("http.errors", c.Errors.String()),
			)
			span.SetStatus(codes.Error, c.Errors.String())
		}
	}
}
