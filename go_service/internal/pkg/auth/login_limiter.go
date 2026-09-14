package auth

import (
	"context"
	"errors"
	"time"

	"customer_service/internal/pkg/cache"

	"github.com/redis/go-redis/v9"
)

// ============ 登录防爆破（强制 Redis，fail-closed） ============
// Redis key: cs:login:fail:{key}（值=失败次数，TTL=窗口10分钟）
// 语义：Redis 查询失败 → 拒绝本次登录尝试（防爆破失效比登录失败更严重），不计数。
// key 形如 "ip:app_key"

const (
	loginFailKeyPrefix = "cs:login:fail:"
	loginMaxFails      = 5
	loginWindow        = 10 * time.Minute
)

func redisLoginKey(key string) string { return loginFailKeyPrefix + key }

// CheckLoginAllowed 登录前置检查；Redis 不可用 → 拒绝（fail-closed）
func CheckLoginAllowed(key string) (bool, time.Duration) {
	rdb := cache.Client()
	if rdb == nil {
		return false, time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	k := redisLoginKey(key)
	n, err := rdb.Get(ctx, k).Int()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return true, 0 // key 不存在：首次尝试，放行
		}
		// Redis 故障：fail-closed 拒绝（防爆破失效比登录失败更严重），不计失败
		return false, time.Minute
	}
	if n >= loginMaxFails {
		ttl, err := rdb.TTL(ctx, k).Result()
		if err != nil || ttl < 0 {
			ttl = loginWindow
		}
		return false, ttl
	}
	return true, 0
}

// RecordLoginFail 记录一次失败（INCR + 首次 EXPIRE）
func RecordLoginFail(key string) {
	rdb := cache.Client()
	if rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	k := redisLoginKey(key)
	n, err := rdb.Incr(ctx, k).Result()
	if err == nil && n == 1 {
		rdb.Expire(ctx, k, loginWindow)
	}
}

// ClearLoginFails 清除失败记录
func ClearLoginFails(key string) {
	rdb := cache.Client()
	if rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rdb.Del(ctx, redisLoginKey(key))
}
