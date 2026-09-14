package cache

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// ============ Redis 客户端（全局单例，强制依赖，启动 fail-fast） ============
// 用途：登录限流计数、Token 黑名单、Agent 缓存失效广播、定时任务分布式锁。
// 设计：Redis 是硬基础设施（与 MySQL/Postgres/Qdrant 同级）。
//   - 启动时 Ping 失败 → Init 返回错误，服务直接启动失败（部署期暴露问题）
//   - 运行中 Redis 不可用 → 调用方 fail-closed（登录拒绝/黑名单按命中处理），不静默降级

var (
	rdb      *redis.Client
	initOnce sync.Once
)

// ErrRedisUnavailable Redis 不可用（运行中连接失败）
var ErrRedisUnavailable = errors.New("redis unavailable")

// RedisAddr 返回 Redis 地址（环境变量 REDIS_ADDR，默认 redis:6379）
func RedisAddr() string {
	if v := os.Getenv("REDIS_ADDR"); v != "" {
		return v
	}
	return "redis:6379"
}

// Init 初始化 Redis 客户端（幂等；Ping 失败返回错误，由调用方决定启动失败）
func Init() error {
	var initErr error
	initOnce.Do(func() {
		rdb = redis.NewClient(&redis.Options{
			Addr:     RedisAddr(),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       0,
			DialTimeout:  3 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
		})
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := rdb.Ping(ctx).Err(); err != nil {
			initErr = err
			rdb = nil
			return
		}
	})
	return initErr
}

// Client 返回 Redis 客户端（Init 成功后非 nil）
func Client() *redis.Client { return rdb }

// Publish 发布缓存失效广播（channel: cs:model:cache:invalidate）
func Publish(channel, msg string) error {
	if rdb == nil {
		return ErrRedisUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return rdb.Publish(ctx, channel, msg).Err()
}
