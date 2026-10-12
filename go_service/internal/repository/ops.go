package repository

import (
	"time"

	"customer_service/internal/config"
	"customer_service/internal/model"
)

// DashboardOverview 运营看板总览
type DashboardOverview struct {
	TotalSessions      int64   `json:"total_sessions"`       // 总会话数（distinct thread_id）
	TotalMessages      int64   `json:"total_messages"`       // 总消息数
	UserMessages       int64   `json:"user_messages"`        // 客户消息数
	OpenTickets        int64   `json:"open_tickets"`         // 开放工单（待处理+接管中）
	HandoffSessions    int64   `json:"handoff_sessions"`     // 已转人工会话数
	AvgRating          float64 `json:"avg_rating"`           // 平均评分（0 表示暂无评价）
	RatingCount        int64   `json:"rating_count"`         // 评价数
	SatisfactionRate   float64 `json:"satisfaction_rate"`    // 满意率（4-5星占比，0-100）
}

// DashboardTrend 每日趋势点
type DashboardTrend struct {
	Date     string `json:"date"`      // YYYY-MM-DD
	Sessions int64  `json:"sessions"`  // 当日会话数
	Messages int64  `json:"messages"`  // 当日消息数
}

// TicketDist 工单状态分布
type TicketDist struct {
	Status int    `json:"status"`
	Name   string `json:"name"`
	Count  int64  `json:"count"`
}

// AgentTicketStat 客服处理量排行项
type AgentTicketStat struct {
	HandlerName string `json:"handler_name"`
	Total       int64  `json:"total"`  // 处理过（含已关闭）
	Open        int64  `json:"open"`   // 开放中
}

// DashboardData 运营看板聚合数据
type DashboardData struct {
	Overview DashboardOverview `json:"overview"`
	Trend    []DashboardTrend  `json:"trend"`
	Tickets  []TicketDist      `json:"tickets"`
	Agents   []AgentTicketStat `json:"agents"`
}

// toInt64 兼容驱动返回的 int64/int32/[]byte 等类型
func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case int:
		return int64(n)
	case uint64:
		return int64(n)
	case float64:
		return int64(n)
	case []byte:
		var i int64
		for _, b := range n {
			if b < '0' || b > '9' {
				return 0
			}
			i = i*10 + int64(b-'0')
		}
		return i
	case nil:
		return 0
	default:
		return 0
	}
}

// BuildDashboard 聚合运营看板数据（仅统计当前租户）
func BuildDashboard(tenantID string) (*DashboardData, error) {
	d := &DashboardData{}
	tid := tenantID

	// 1. 会话/消息/工单/满意度总览（单条聚合，避免多次 Scan 同一结构）
	ov := &d.Overview
	if err := config.DB.Raw(`
		SELECT
		  (SELECT COUNT(DISTINCT m.thread_id) FROM messages m WHERE m.tenant_id = ?) AS total_sessions,
		  (SELECT COUNT(*) FROM messages m WHERE m.tenant_id = ?) AS total_messages,
		  (SELECT COALESCE(SUM(CASE WHEN m.role = 'user' THEN 1 ELSE 0 END), 0) FROM messages m WHERE m.tenant_id = ?) AS user_messages,
		  (SELECT COUNT(*) FROM tickets t WHERE t.tenant_id = ? AND t.status IN (0,1)) AS open_tickets,
		  (SELECT COUNT(DISTINCT t.thread_id) FROM tickets t WHERE t.tenant_id = ? AND t.status IN (0,1)) AS handoff_sessions,
		  (SELECT COALESCE(AVG(r.score), 0) FROM satisfaction_ratings r WHERE r.tenant_id = ?) AS avg_rating,
		  (SELECT COUNT(*) FROM satisfaction_ratings r WHERE r.tenant_id = ?) AS rating_count,
		  (SELECT COALESCE(SUM(CASE WHEN r.score >= 4 THEN 1 ELSE 0 END), 0) * 100.0 / NULLIF((SELECT COUNT(*) FROM satisfaction_ratings r WHERE r.tenant_id = ?), 0)
		   FROM satisfaction_ratings r WHERE r.tenant_id = ?) AS satisfaction_rate`,
		tid, tid, tid, tid, tid, tid, tid, tid, tid).Scan(ov).Error; err != nil {
		return nil, err
	}

	// 2. 近 7 天趋势（按天分组；用 map 中间结果规避 GORM 列名映射问题）
	start := time.Now().AddDate(0, 0, -6).Format("2006-01-02") + " 00:00:00"
	var trendRows []map[string]interface{}
	if err := config.DB.Model(&model.Message{}).
		Select("DATE(created_at) AS day, COUNT(DISTINCT thread_id) AS sessions, COUNT(*) AS messages").
		Where("tenant_id = ? AND created_at >= ?", tid, start).
		Group("DATE(created_at)").
		Scan(&trendRows).Error; err != nil {
		return nil, err
	}
	byDate := map[string]*DashboardTrend{}
	for _, row := range trendRows {
		day, _ := row["day"].(time.Time)
		if day.IsZero() {
			if s, ok := row["day"].(string); ok && s != "" {
				if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
					day = t
				}
			}
		}
		key := day.Format("2006-01-02")
		byDate[key] = &DashboardTrend{
			Date:     key,
			Sessions: toInt64(row["sessions"]),
			Messages: toInt64(row["messages"]),
		}
	}
	full := make([]DashboardTrend, 0, 7)
	for i := 6; i >= 0; i-- {
		day := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		if t, ok := byDate[day]; ok {
			full = append(full, *t)
		} else {
			full = append(full, DashboardTrend{Date: day})
		}
	}
	d.Trend = full

	// 3. 工单状态分布
	var dist []TicketDist
	if err := config.DB.Model(&model.Ticket{}).
		Where("tenant_id = ?", tid).
		Select("status, COUNT(*) AS count").
		Group("status").Scan(&dist).Error; err != nil {
		return nil, err
	}
	names := map[int]string{0: "待处理", 1: "接管中", 2: "已取消", 3: "已关闭"}
	d.Tickets = make([]TicketDist, 0, len(dist))
	for _, x := range dist {
		x.Name = names[x.Status]
		d.Tickets = append(d.Tickets, x)
	}

	// 4. 客服处理量排行（按接管人聚合）
	var agents []AgentTicketStat
	if err := config.DB.Model(&model.Ticket{}).
		Where("tenant_id = ? AND handler_name != ''", tid).
		Select("handler_name, COUNT(*) AS total, " +
			"COALESCE(SUM(CASE WHEN status IN (0,1) THEN 1 ELSE 0 END), 0) AS open").
		Group("handler_name").
		Order("total DESC").
		Scan(&agents).Error; err != nil {
		return nil, err
	}
	d.Agents = agents

	return d, nil
}
