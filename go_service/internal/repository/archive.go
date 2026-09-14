package repository

import (
	"strconv"
	"time"

	"customer_service/internal/config"
	"customer_service/internal/model"
	"customer_service/internal/pkg/logger"

	"gorm.io/gorm"
)

// ArchiveBatchSize 归档分批大小，避免大事务锁表
const ArchiveBatchSize = 500

// archivableTicketStatuses 可归档消息的工单状态（已关闭/已取消），返回字符串切片供 GORM IN 展开
func archivableTicketStatuses() []string {
	return []string{strconv.Itoa(model.TicketStatusClosed), strconv.Itoa(model.TicketStatusCanceled)}
}

// ArchiveOldMessages P1-4：将超过保留天数的消息分批迁移到 messages_archive
// 仅归档"无工单归属 或 工单已关闭/取消"的消息（活跃工单上下文保留在 messages 表，保证客服可查历史）
// tenantID 为空表示全租户归档（平台管理员），非空仅归档该租户消息。
// 返回本次归档条数。分批事务，单批失败不影响已归档批次。
func ArchiveOldMessages(retentionDays int, tenantID string) int64 {
	if retentionDays <= 0 {
		retentionDays = 90
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	var total int64
	for {
		// 取一批待归档记录（排除活跃工单消息）
		var batch []model.Message
		q := config.DB.Table("messages AS m").
			Select("m.*").
			Joins("LEFT JOIN tickets t ON m.ticket_id = t.id").
			Where("m.created_at < ? AND (m.ticket_id IS NULL OR t.status IN ?)", cutoff, archivableTicketStatuses())
		if tenantID != "" {
			q = q.Where("m.tenant_id = ?", tenantID)
		}
		if err := q.Order("m.id ASC").Limit(ArchiveBatchSize).Scan(&batch).Error; err != nil {
			logger.Warn("archive query failed", logger.Fields{"err": err.Error()})
			return total
		}
		if len(batch) == 0 {
			break
		}
		ids := make([]uint64, 0, len(batch))
		archived := make([]model.ArchivedMessage, 0, len(batch))
		for _, m := range batch {
			ids = append(ids, m.ID)
			archived = append(archived, model.ArchivedMessage{
				ThreadID: m.ThreadID, Role: m.Role, Content: m.Content,
				TenantID: m.TenantID, TicketID: m.TicketID, IsRead: m.IsRead,
				CreatedAt: m.CreatedAt, ArchivedAt: time.Now(),
			})
		}
		// 单事务：先插入归档，再删除原表（保留原 ID 便于审计）
		err := config.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&archived).Error; err != nil {
				return err
			}
			return tx.Where("id IN ?", ids).Delete(&model.Message{}).Error
		})
		if err != nil {
			logger.Warn("archive batch failed", logger.Fields{"err": err.Error(), "batch": len(batch)})
			return total
		}
		total += int64(len(batch))
		logger.Info("消息归档批次完成", logger.Fields{"count": len(batch), "cutoff_days": retentionDays})
	}
	return total
}

// CountArchivedMessages 已归档消息总数（管理端展示用）
func CountArchivedMessages(tenantID string) (int64, error) {
	q := config.DB.Model(&model.ArchivedMessage{})
	if tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	var cnt int64
	err := q.Count(&cnt).Error
	return cnt, err
}

// CountArchivableMessages 当前可归档消息数（超过保留期 且 无工单归属/工单已关闭取消）
// 与 ArchiveOldMessages 条件一致，供管理端"可归档 N 条"展示
func CountArchivableMessages(retentionDays int, tenantID string) (int64, error) {
	if retentionDays <= 0 {
		retentionDays = 90
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	q := config.DB.Table("messages AS m").
		Joins("LEFT JOIN tickets t ON m.ticket_id = t.id").
		Where("m.created_at < ? AND (m.ticket_id IS NULL OR t.status IN ?)", cutoff, archivableTicketStatuses())
	if tenantID != "" {
		q = q.Where("m.tenant_id = ?", tenantID)
	}
	var cnt int64
	err := q.Count(&cnt).Error
	return cnt, err
}
