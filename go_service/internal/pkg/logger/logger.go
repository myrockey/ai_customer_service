package logger

import (
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

var log *logrus.Logger

// Init 初始化结构化日志
// output: json 或 text
// level: debug/info/warn/error
func Init(output string, level string) {
	log = logrus.New()

	// 输出格式
	if output == "json" {
		log.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: time.RFC3339Nano,
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyTime:  "timestamp",
				logrus.FieldKeyLevel: "level",
				logrus.FieldKeyMsg:   "message",
			},
		})
	} else {
		log.SetFormatter(&logrus.TextFormatter{
			TimestampFormat: "2006-01-02 15:04:05.000",
			FullTimestamp:   true,
		})
	}

	// 日志级别
	switch level {
	case "debug":
		log.SetLevel(logrus.DebugLevel)
	case "warn":
		log.SetLevel(logrus.WarnLevel)
	case "error":
		log.SetLevel(logrus.ErrorLevel)
	default:
		log.SetLevel(logrus.InfoLevel)
	}

	log.SetOutput(os.Stdout)
}

// Fields 日志字段类型
type Fields map[string]interface{}

// withFields 创建带字段的日志条目
func withFields(fields Fields) *logrus.Entry {
	if log == nil {
		Init("text", "info")
	}
	return log.WithFields(logrus.Fields(fields))
}

// Info 信息日志
func Info(msg string, fields ...Fields) {
	if len(fields) > 0 {
		withFields(fields[0]).Info(msg)
	} else {
		log.Info(msg)
	}
}

// Warn 警告日志
func Warn(msg string, fields ...Fields) {
	if len(fields) > 0 {
		withFields(fields[0]).Warn(msg)
	} else {
		log.Warn(msg)
	}
}

// Error 错误日志
func Error(msg string, fields ...Fields) {
	if len(fields) > 0 {
		withFields(fields[0]).Error(msg)
	} else {
		log.Error(msg)
	}
}

// Debug 调试日志
func Debug(msg string, fields ...Fields) {
	if len(fields) > 0 {
		withFields(fields[0]).Debug(msg)
	} else {
		log.Debug(msg)
	}
}

// Fatal 致命错误日志
func Fatal(msg string, fields ...Fields) {
	if len(fields) > 0 {
		withFields(fields[0]).Fatal(msg)
	} else {
		log.Fatal(msg)
	}
}

// WithTrace 创建带 trace_id 的日志字段
func WithTrace(traceID string) Fields {
	return Fields{"trace_id": traceID}
}

// WithTenant 创建带 tenant_id 的日志字段
func WithTenant(tenantID string) Fields {
	return Fields{"tenant_id": tenantID}
}

// WithUser 创建带 user_id 的日志字段
func WithUser(userID string) Fields {
	return Fields{"user_id": userID}
}

// MergeFields 合并多个日志字段
func MergeFields(fields ...Fields) Fields {
	merged := Fields{}
	for _, f := range fields {
		for k, v := range f {
			merged[k] = v
		}
	}
	return merged
}
