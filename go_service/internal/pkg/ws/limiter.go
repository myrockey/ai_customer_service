package ws

import (
	"sync"
	"time"
)

// WSLimiter WebSocket 连接和消息频率限制器
// 防止恶意刷连接和刷消息耗尽服务器资源
type WSLimiter struct {
	mu sync.Mutex

	// 单 IP 连接数限制
	maxConnsPerIP int
	ipConns       map[string]int // IP -> 连接数

	// 单 user_id 连接数限制
	maxConnsPerUser int
	userConns       map[string]int // user_id -> 连接数

	// 消息发送频率限制（滑动窗口）
	maxMsgsPerSecond int            // 每秒最多消息数
	userMsgTimes     map[string][]time.Time // user_id -> 最近消息时间戳
}

// 全局限流器实例
var GlobalWSLimiter = NewWSLimiter()

// NewWSLimiter 创建限流器
func NewWSLimiter() *WSLimiter {
	l := &WSLimiter{
		maxConnsPerIP:    10,  // 单 IP 最多 10 个连接
		maxConnsPerUser:  3,   // 单 user_id 最多 3 个连接
		maxMsgsPerSecond: 5,   // 每秒最多 5 条消息
		ipConns:          make(map[string]int),
		userConns:        make(map[string]int),
		userMsgTimes:     make(map[string][]time.Time),
	}
	// 启动后台清理协程，定期清理过期的消息时间戳
	go l.cleanupLoop()
	return l
}

// cleanupLoop 定期清理过期的消息时间戳
func (l *WSLimiter) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		now := time.Now()
		for userID, times := range l.userMsgTimes {
			// 保留最近 2 秒内的时间戳
			valid := make([]time.Time, 0, len(times))
			for _, t := range times {
				if now.Sub(t) < 2*time.Second {
					valid = append(valid, t)
				}
			}
			if len(valid) == 0 {
				delete(l.userMsgTimes, userID)
			} else {
				l.userMsgTimes[userID] = valid
			}
		}
		l.mu.Unlock()
	}
}

// TryAcquireConn 尝试获取连接配额
// 返回 (allowed bool, reason string)
func (l *WSLimiter) TryAcquireConn(ip, userID string) (bool, string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 检查单 IP 连接数
	if l.ipConns[ip] >= l.maxConnsPerIP {
		return false, "单 IP 连接数已达上限（最多 " + itoa(l.maxConnsPerIP) + " 个）"
	}

	// 检查单 user_id 连接数
	if l.userConns[userID] >= l.maxConnsPerUser {
		return false, "单用户连接数已达上限（最多 " + itoa(l.maxConnsPerUser) + " 个）"
	}

	// 增加连接计数
	l.ipConns[ip]++
	l.userConns[userID]++
	return true, ""
}

// ReleaseConn 释放连接配额
func (l *WSLimiter) ReleaseConn(ip, userID string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.ipConns[ip] > 0 {
		l.ipConns[ip]--
		if l.ipConns[ip] == 0 {
			delete(l.ipConns, ip)
		}
	}
	if l.userConns[userID] > 0 {
		l.userConns[userID]--
		if l.userConns[userID] == 0 {
			delete(l.userConns, userID)
		}
	}
}

// TryAcquireMsg 尝试获取消息发送配额（滑动窗口）
// 返回 (allowed bool, reason string)
func (l *WSLimiter) TryAcquireMsg(userID string) (bool, string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	times := l.userMsgTimes[userID]

	// 清理 1 秒前的时间戳
	valid := make([]time.Time, 0, len(times))
	for _, t := range times {
		if now.Sub(t) < time.Second {
			valid = append(valid, t)
		}
	}

	// 检查是否超过频率限制
	if len(valid) >= l.maxMsgsPerSecond {
		l.userMsgTimes[userID] = valid
		return false, "消息发送过于频繁（每秒最多 " + itoa(l.maxMsgsPerSecond) + " 条）"
	}

	// 记录当前消息时间
	valid = append(valid, now)
	l.userMsgTimes[userID] = valid
	return true, ""
}

// GetStats 获取限流器统计信息
func (l *WSLimiter) GetStats() map[string]interface{} {
	l.mu.Lock()
	defer l.mu.Unlock()

	totalIPConns := 0
	for _, c := range l.ipConns {
		totalIPConns += c
	}
	totalUserConns := 0
	for _, c := range l.userConns {
		totalUserConns += c
	}

	return map[string]interface{}{
		"active_ips":         len(l.ipConns),
		"active_users":       len(l.userConns),
		"total_ip_conns":     totalIPConns,
		"total_user_conns":   totalUserConns,
		"max_conns_per_ip":   l.maxConnsPerIP,
		"max_conns_per_user": l.maxConnsPerUser,
		"max_msgs_per_sec":   l.maxMsgsPerSecond,
	}
}

// itoa 简单的整数转字符串（避免引入 strconv）
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
