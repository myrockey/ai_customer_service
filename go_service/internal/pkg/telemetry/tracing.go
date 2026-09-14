package telemetry

import (
	"context"
	"log"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

var tracerProvider *sdktrace.TracerProvider

// InitTracer 初始化 OpenTelemetry 链路追踪
func InitTracer() func() {
	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "customer-service-go"
	}

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://jaeger:4318"
	}

	exporter, err := otlptracehttp.New(
		context.Background(),
		otlptracehttp.WithEndpointURL(endpoint+"/v1/traces"),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		log.Printf("创建 OTLP exporter 失败: %v，链路追踪将不可用", err)
		return func() {}
	}

	res, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		log.Printf("创建 resource 失败: %v", err)
	}

	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1))

	tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	log.Printf("OpenTelemetry 链路追踪已初始化，endpoint: %s, service: %s", endpoint, serviceName)

	return func() {
		if tracerProvider != nil {
			if err := tracerProvider.Shutdown(context.Background()); err != nil {
				log.Printf("关闭 TracerProvider 失败: %v", err)
			}
		}
	}
}
