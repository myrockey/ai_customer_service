package service

import (
	"customer_service/internal/pkg/agent"
	"customer_service/internal/pkg/auth"
	"customer_service/internal/pkg/crypto"
	"customer_service/internal/pkg/ws"
	"customer_service/internal/config"
	"customer_service/internal/model"
	"encoding/json"
	"errors"
	"log"
	"time"
)

// TenantListInput 租户列表查询参数
type TenantListInput struct {
	Name   string
	AppKey string
	Status string // "", "0", "1"
	Sort   string // "created_at_desc"(默认), "created_at_asc", "name"
}

// TenantListItem 租户列表返回项（不含 secret 哈希；含配额与当前用量）
type TenantListItem struct {
	TenantID              string  `json:"tenant_id"`
	Name                  string  `json:"name"`
	AppKey                string  `json:"app_key"`
	Status                int     `json:"status"`
	CreatedAt             string  `json:"created_at"`
	MaxConcurrentSessions int     `json:"max_concurrent_sessions"`
	DailyMessageLimit     int     `json:"daily_message_limit"`
	MaxKbDocs             int     `json:"max_kb_docs"`
	MaxKbSizeMB           int     `json:"max_kb_size_mb"`
	ActiveSessions        int     `json:"active_sessions"`  // 最近30分钟活跃会话数
	TodayMessages         int     `json:"today_messages"`   // 今日用户消息数
	KbDocs                int     `json:"kb_docs"`          // 知识库文档数
	KbSizeMB              float64 `json:"kb_size_mb"`       // 知识库占用 MB
}

// ListTenants 查询租户列表（排除平台管理员）
func ListTenants(input TenantListInput) ([]TenantListItem, error) {
	q := config.DB.Model(&model.TenantRow{}).Where("is_platform_admin = ?", false)
	if input.Name != "" {
		q = q.Where("name LIKE ?", "%"+input.Name+"%")
	}
	if input.AppKey != "" {
		q = q.Where("app_key LIKE ?", "%"+input.AppKey+"%")
	}
	if input.Status == "0" || input.Status == "1" {
		q = q.Where("status = ?", input.Status)
	}
	switch input.Sort {
	case "created_at_asc":
		q = q.Order("created_at ASC")
	case "name":
		q = q.Order("name ASC")
	default:
		q = q.Order("created_at DESC")
	}

	var rows []model.TenantRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}

	// 用量统计（失败不阻塞列表，用量归零）
	usage := fetchTenantUsage()

	list := make([]TenantListItem, 0, len(rows))
	for _, r := range rows {
		u := usage[r.TenantID]
		list = append(list, TenantListItem{
			TenantID:              r.TenantID,
			Name:                  r.Name,
			AppKey:                r.AppKey,
			Status:                r.Status,
			CreatedAt:             r.CreatedAt.Format("2006-01-02 15:04:05"),
			MaxConcurrentSessions: r.MaxConcurrentSessions,
			DailyMessageLimit:     r.DailyMessageLimit,
			MaxKbDocs:             r.MaxKbDocs,
			MaxKbSizeMB:           r.MaxKbSizeMB,
			ActiveSessions:        u.ActiveSessions,
			TodayMessages:         u.TodayMessages,
			KbDocs:                u.KbDocs,
			KbSizeMB:              u.KbSizeMB,
		})
	}
	return list, nil
}

// tenantUsage 单个租户用量
type tenantUsage struct {
	ActiveSessions int
	TodayMessages  int
	KbDocs         int
	KbSizeMB       float64
}

// fetchTenantUsage 统计各租户用量：
//   - 今日用户消息数：MySQL messages（role=user 且今日）
//   - 活跃会话/知识库文档数/容量：Python Agent（PG，与配额校验口径一致）
func fetchTenantUsage() map[string]tenantUsage {
	out := make(map[string]tenantUsage)

	// 1) MySQL 今日用户消息数
	type dailyCnt struct {
		TenantID string
		Cnt      int
	}
	var daily []dailyCnt
	todayStart := time.Now().Truncate(24 * time.Hour)
	config.DB.Model(&model.Message{}).
		Select("tenant_id, COUNT(*) AS cnt").
		Where("role = ? AND created_at >= ?", "user", todayStart).
		Group("tenant_id").
		Scan(&daily)
	for _, d := range daily {
		u := out[d.TenantID]
		u.TodayMessages = d.Cnt
		out[d.TenantID] = u
	}

	// 2) Agent 侧用量（PG：活跃会话/知识库）
	body, err := agent.AgentRequest("GET", "/agent/tenant-usage", "", "", nil, "tenant-usage", "")
	if err != nil {
		log.Printf("[tenant] agent tenant-usage 调用失败: %v", err)
		return out
	}
	var resp struct {
		Code int `json:"code"`
		Data map[string]struct {
			ActiveSessions int   `json:"active_sessions"`
			KbDocs         int   `json:"kb_docs"`
			KbSizeBytes    int64 `json:"kb_size_bytes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Code != 0 {
		log.Printf("[tenant] agent tenant-usage 解析失败: %v", err)
		return out
	}
	for tid, u := range resp.Data {
		cur := out[tid]
		cur.ActiveSessions = u.ActiveSessions
		cur.KbDocs = u.KbDocs
		cur.KbSizeMB = float64(u.KbSizeBytes) / 1024 / 1024
		out[tid] = cur
	}

	// 3) 并发会话覆盖为 Go ws hub 实时连接数（与配额校验口径一致）
	for tid, n := range ws.WSHub.CountAllTenantConns() {
		cur := out[tid]
		cur.ActiveSessions = n
		out[tid] = cur
	}
	return out
}

// CreateTenantResult 创建租户返回结果
type CreateTenantResult struct {
	TenantID  string
	Name      string
	AppKey    string
	AppSecret string // 明文，仅此一次返回
}

// CreateTenant 创建租户（自动生成 tenant_id / app_secret）
func CreateTenant(name, appKey string) (*CreateTenantResult, error) {
	if appKey == "" {
		appKey = auth.GenAppKey()
	}
	appSecret := auth.GenSecret()
	tenantID := "t_" + appKey

	row := model.TenantRow{
		TenantID:      tenantID,
		Name:          name,
		AppKey:        appKey,
		AppSecretHash: crypto.HashSecret(appSecret),
		Status:        1,
		// 默认跟随平台模型（provider=platform），避免空值歧义
		ModelProvider:       "platform",
		EmbeddingProvider:   "platform",
		MaxConcurrentSessions: 100,
		DailyMessageLimit:     1000,
		MaxKbDocs:             100,
		MaxKbSizeMB:           100,
	}
	if err := config.DB.Create(&row).Error; err != nil {
		return nil, err
	}
	if err := auth.RefreshTenants(); err != nil {
		return nil, err
	}

	// 异步预热新租户 Agent
	go func() {
		if err := agent.PrewarmTenant(tenantID); err != nil {
			log.Printf("prewarm new tenant %s: %v", tenantID, err)
		}
	}()

	return &CreateTenantResult{
		TenantID:  tenantID,
		Name:      name,
		AppKey:    appKey,
		AppSecret: appSecret,
	}, nil
}

// ResetTenantSecret 重置租户密钥，返回新的明文密钥
func ResetTenantSecret(tid string) (string, error) {
	var row model.TenantRow
	if err := config.DB.Where("tenant_id = ?", tid).First(&row).Error; err != nil {
		return "", err
	}
	appSecret := auth.GenSecret()
	if err := config.DB.Model(&row).
		Update("app_secret_hash", crypto.HashSecret(appSecret)).Error; err != nil {
		return "", err
	}
	if err := auth.RefreshTenants(); err != nil {
		return "", err
	}
	return appSecret, nil
}

// ChangeTenantSecret 修改当前租户自己的登录密钥（校验旧密钥，防止越权改密）
// 更新后刷新 auth 内存缓存 → 旧 token 签名失效，需重新登录
func ChangeTenantSecret(tid, oldSecret, newSecret string) error {
	var row model.TenantRow
	if err := config.DB.Where("tenant_id = ?", tid).First(&row).Error; err != nil {
		return errors.New("租户不存在")
	}
	if row.AppSecretHash != crypto.HashSecret(oldSecret) {
		return errors.New("旧密钥不正确")
	}
	if err := config.DB.Model(&row).
		Update("app_secret_hash", crypto.HashSecret(newSecret)).Error; err != nil {
		return errors.New("密钥保存失败")
	}
	if err := auth.RefreshTenants(); err != nil {
		return errors.New("刷新租户缓存失败，请稍后重试")
	}
	return nil
}

// ToggleTenantStatus 切换租户启用/禁用状态，返回新状态
func ToggleTenantStatus(tid string) (int, error) {
	var row model.TenantRow
	if err := config.DB.Where("tenant_id = ?", tid).First(&row).Error; err != nil {
		return 0, err
	}
	newStatus := 1
	if row.Status == 1 {
		newStatus = 0
	}
	if err := config.DB.Model(&row).Update("status", newStatus).Error; err != nil {
		return 0, err
	}
	if err := auth.RefreshTenants(); err != nil {
		return 0, err
	}
	return newStatus, nil
}

// GetTenantDetail 获取租户详情（不含 secret 哈希）
func GetTenantDetail(tid string) (*model.TenantRow, error) {
	var row model.TenantRow
	if err := config.DB.Where("tenant_id = ?", tid).First(&row).Error; err != nil {
		return nil, err
	}
	row.AppSecretHash = "" // 不返回 secret 哈希
	return &row, nil
}

// UpdateTenant 更新租户名称
func UpdateTenant(tid, name string) error {
	var row model.TenantRow
	if err := config.DB.Where("tenant_id = ?", tid).First(&row).Error; err != nil {
		return err
	}
	if err := config.DB.Model(&row).Update("name", name).Error; err != nil {
		return err
	}
	return auth.RefreshTenants()
}

// UpdateTenantQuota P1-2：更新租户配额（0 表示不限制）
func UpdateTenantQuota(tid string, maxConcurrentSessions, dailyMessageLimit, maxKbDocs, maxKbSizeMB int) error {
	var row model.TenantRow
	if err := config.DB.Where("tenant_id = ?", tid).First(&row).Error; err != nil {
		return err
	}
	updates := map[string]interface{}{
		"max_concurrent_sessions": maxConcurrentSessions,
		"daily_message_limit":     dailyMessageLimit,
		"max_kb_docs":             maxKbDocs,
		"max_kb_size_mb":          maxKbSizeMB,
	}
	if err := config.DB.Model(&row).Updates(updates).Error; err != nil {
		return err
	}
	return auth.RefreshTenants()
}

// DeleteTenant 删除租户（含级联删除工单、消息、Prompt、知识库数据）
func DeleteTenant(tid string) error {
	var row model.TenantRow
	if err := config.DB.Where("tenant_id = ?", tid).First(&row).Error; err != nil {
		return err
	}

	// 1. 删除工单
	if err := config.DB.Where("tenant_id = ?", tid).Delete(&model.Ticket{}).Error; err != nil {
		return err
	}
	// 2. 删除消息
	if err := config.DB.Where("tenant_id = ?", tid).Delete(&model.Message{}).Error; err != nil {
		return err
	}
	// 3. 删除租户
	if err := config.DB.Delete(&row).Error; err != nil {
		return err
	}
	// 4. 刷新内存租户缓存
	if err := auth.RefreshTenants(); err != nil {
		return err
	}
	// 5. 异步清理 Agent 侧数据（Prompt / LangGraph checkpoint / Qdrant 知识库）
	go func() {
		agentResp, err := agent.AgentRequest("POST", "/agent/tenant/cleanup", "",
			"application/json", nil, "", tid)
		if err != nil {
			log.Printf("cleanup agent data for tenant %s failed: %v", tid, err)
			return
		}
		log.Printf("cleanup agent data for tenant %s done: %s", tid, string(agentResp))
	}()

	// 等待一小段时间，确保级联删除完成
	time.Sleep(100 * time.Millisecond)
	return nil
}
