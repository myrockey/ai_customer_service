package repository

import (
	"time"

	"customer_service/internal/config"
	"customer_service/internal/model"
)

// QuotaResult 配额校验结果
type QuotaResult struct {
	OK      bool   `json:"ok"`
	Limit   int    `json:"limit"`
	Current int    `json:"current"`
	Reason  string `json:"reason"`
}

// CheckDailyMessageQuota P1-2：校验租户今日消息数是否超限（limit<=0 表示不限制）
func CheckDailyMessageQuota(tenantID string) (*QuotaResult, error) {
	var row model.TenantRow
	if err := config.DB.Where("tenant_id = ?", tenantID).First(&row).Error; err != nil {
		return nil, err
	}
	limit := row.DailyMessageLimit
	if limit <= 0 {
		return &QuotaResult{OK: true, Limit: limit, Current: 0, Reason: "unlimited"}, nil
	}
	start := time.Now().Truncate(24 * time.Hour)
	var cnt int64
	if err := config.DB.Model(&model.Message{}).
		Where("tenant_id = ? AND created_at >= ?", tenantID, start).
		Count(&cnt).Error; err != nil {
		return nil, err
	}
	if cnt >= int64(limit) {
		return &QuotaResult{OK: false, Limit: limit, Current: int(cnt),
			Reason: "今日消息数已达上限"}, nil
	}
	return &QuotaResult{OK: true, Limit: limit, Current: int(cnt)}, nil
}

// GetTenantQuota 读取租户配额（租户管理/校验用）
func GetTenantQuota(tenantID string) (*model.TenantRow, error) {
	var row model.TenantRow
	if err := config.DB.Where("tenant_id = ?", tenantID).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}
