package main

import (
	"os"
	"time"

	"customer_service/internal/config"
	"customer_service/internal/pkg/agent"
	"customer_service/internal/pkg/auth"
	"customer_service/internal/pkg/cache"
	"customer_service/internal/pkg/crypto"
	"customer_service/internal/pkg/logger"
	"customer_service/internal/pkg/telemetry"
	"customer_service/internal/repository"
	"customer_service/internal/router"

	"github.com/gin-gonic/gin"
)

func main() {
	// 初始化结构化日志（JSON格式，便于Loki收集）
	logger.Init("json", "info")

	// 初始化 OpenTelemetry 链路追踪
	shutdownTracer := telemetry.InitTracer()
	defer shutdownTracer()

	if err := config.LoadConfig("./config.yaml"); err != nil {
		logger.Fatal("配置加载失败", logger.Fields{"error": err.Error()})
	}
	// 配置验证，启动时检查关键配置
	if err := config.ValidateConfig(); err != nil {
		logger.Fatal("配置验证失败", logger.Fields{"error": err.Error()})
	}
	if err := config.InitDB(); err != nil {
		logger.Fatal("数据库初始化失败", logger.Fields{"error": err.Error()})
	}
	auth.InitAuth(config.GlobalConfig.Auth.Enabled, config.GlobalConfig.Auth.TokenTTL, config.GlobalConfig.Auth.Tenants)

	// 初始化 RSA 密钥对（登录 app_secret 传输加密；首次启动自动生成并持久化）
	rsaPath := os.Getenv("RSA_PRIVATE_KEY_PATH")
	if rsaPath == "" {
		rsaPath = "data/rsa_private.pem"
	}
	crypto.InitRSA(rsaPath)
	logger.Info("RSA 密钥就绪", logger.Fields{"path": rsaPath})

	// 初始化 Redis（登录限流/Token黑名单/缓存失效广播/分布式锁）——强制依赖，启动失败即退出
	if err := cache.Init(); err != nil {
		logger.Fatal("Redis 初始化失败（强制依赖）", logger.Fields{"addr": cache.RedisAddr(), "error": err.Error()})
	}
	logger.Info("Redis 连接成功", logger.Fields{"addr": cache.RedisAddr()})

	logger.Info("服务启动中", logger.Fields{"port": config.GlobalConfig.Server.Port})

	// 启动后异步预热所有启用租户
	go func() {
		time.Sleep(3 * time.Second)
		for _, t := range auth.AllTenants() {
			if t.Status == 1 {
				if err := agent.PrewarmTenant(t.TenantID); err != nil {
					logger.Warn("租户预热失败", logger.MergeFields(logger.WithTenant(t.TenantID), logger.Fields{"error": err.Error()}))
				} else {
					logger.Info("租户预热成功", logger.WithTenant(t.TenantID))
				}
			}
		}
	}()

	// 定期清理过期的 Token 黑名单条目
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			auth.CleanupBlacklist()
			logger.Debug("Token黑名单清理完成")
		}
	}()

	// P1-4：每日消息归档（超过保留期 90 天的消息迁移到 messages_archive）
	go func() {
		time.Sleep(10 * time.Second) // 等 DB 就绪
		for {
			cnt := repository.ArchiveOldMessages(config.GlobalConfig.MsgRetentionDays, "")
			if cnt > 0 {
				logger.Info("消息归档完成", logger.Fields{"archived": cnt})
			}
			time.Sleep(24 * time.Hour)
		}
	}()

	r := gin.Default()

	// 注册所有路由（路由和中间件统一在 router 包中管理）
	router.SetupRoutes(r)

	logger.Info("服务启动完成", logger.Fields{"port": config.GlobalConfig.Server.Port})
	_ = r.Run(config.GlobalConfig.Server.Port)
}
