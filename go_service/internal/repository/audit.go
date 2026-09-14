package repository

import (
	"time"

	"customer_service/internal/config"
	"customer_service/internal/model"
	"customer_service/internal/pkg/logger"
)

// WriteAuditLog P1-3：写一条管理端操作审计日志（best-effort，失败仅记日志不影响主流程）
func WriteAuditLog(tenantID, operator, action, objectType, objectID, detail, ip string) {
	if tenantID == "" {
		tenantID = "system"
	}
	log := model.AuditLog{
		TenantID:   tenantID,
		Operator:   operator,
		Action:     action,
		ObjectType: objectType,
		ObjectID:   objectID,
		Detail:     detail,
		IP:         ip,
	}
	if err := config.DB.Create(&log).Error; err != nil {
		logger.Warn("write audit log failed", logger.Fields{"err": err.Error()})
	}
}

// AuditLogFilter 审计日志筛选条件
type AuditLogFilter struct {
	Action     string // create/update/delete...
	ObjectType string // ticket/prompt/knowledge...
	Operator   string // 操作人（app_key / 用户名，模糊）
	IP         string // 来源IP（模糊）
	TimeFrom   string // 起始时间，格式 2006-01-02 15:04:05
	TimeTo     string // 结束时间
}

// ListAuditLogs 分页查询审计日志（支持多维筛选）
// tenantID 为空 = 平台管理员全览；否则仅查本租户
func ListAuditLogs(tenantID string, f AuditLogFilter, page, pageSize int) ([]model.AuditLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	q := config.DB.Model(&model.AuditLog{})
	if tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.ObjectType != "" {
		q = q.Where("object_type = ?", f.ObjectType)
	}
	if f.Operator != "" {
		q = q.Where("operator LIKE ?", "%"+f.Operator+"%")
	}
	if f.IP != "" {
		q = q.Where("ip LIKE ?", "%"+f.IP+"%")
	}
	if f.TimeFrom != "" {
		q = q.Where("created_at >= ?", f.TimeFrom)
	}
	if f.TimeTo != "" {
		q = q.Where("created_at <= ?", f.TimeTo)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var logs []model.AuditLog
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// CleanupAuditLogs 清理超过保留天数的审计日志（保留期 90 天）
func CleanupAuditLogs(retentionDays int) int64 {
	if retentionDays <= 0 {
		retentionDays = 90
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	res := config.DB.Where("created_at < ?", cutoff).Delete(&model.AuditLog{})
	if res.Error != nil {
		logger.Warn("cleanup audit logs failed", logger.Fields{"err": res.Error.Error()})
		return 0
	}
	return res.RowsAffected
}
