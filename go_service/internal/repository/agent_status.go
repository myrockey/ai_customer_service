package repository

import (
	"time"

	"customer_service/internal/config"
	"customer_service/internal/model"

	"gorm.io/gorm"
)

// UpsertAgentStatus 客服上线/离线（WS 连接建立/断开时调用）
// status: 1 在线 0 离线；首次上线自动建记录（默认 max_concurrent=3）
func UpsertAgentStatus(tenantID string, uid uint64, status int) error {
	if tenantID == "" || uid == 0 {
		return nil
	}
	return config.DB.Exec(`INSERT INTO agent_status (tenant_id, user_id, status, handling_count, max_concurrent, updated_at)
		VALUES (?, ?, ?, 0, 3, ?)
		ON DUPLICATE KEY UPDATE status = ?, updated_at = ?`,
		tenantID, uid, status, time.Now(), status, time.Now()).Error
}

// SetAgentStatus 客服手动切换状态（在线/忙碌/离线），不动 handling_count
func SetAgentStatus(tenantID string, uid uint64, status int) error {
	if tenantID == "" || uid == 0 {
		return nil
	}
	return config.DB.Model(&model.AgentStatus{}).
		Where("tenant_id = ? AND user_id = ?", tenantID, uid).
		Update("status", status).Error
}

// AdjustHandlingCount 处理量 +delta，并联动状态：
// 达到 max_concurrent → 忙碌(2)；低于上限且当前非离线 → 在线(1)
// db 传事务则事务内执行（自动分配），否则传 config.DB（接管/关闭/转交）
func AdjustHandlingCount(db *gorm.DB, tenantID string, uid uint64, delta int) error {
	if tenantID == "" || uid == 0 {
		return nil
	}
	var st model.AgentStatus
	if err := db.Where("tenant_id = ? AND user_id = ?", tenantID, uid).First(&st).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil // 该客服不在池内（从未上线），忽略
		}
		return err
	}
	st.HandlingCount += delta
	if st.HandlingCount < 0 {
		st.HandlingCount = 0
	}
	if st.Status != 0 {
		if st.HandlingCount >= st.MaxConcurrent {
			st.Status = 2 // 忙碌
		} else {
			st.Status = 1 // 在线
		}
	}
	return db.Model(&model.AgentStatus{}).Where("id = ?", st.ID).
		Updates(map[string]interface{}{"handling_count": st.HandlingCount, "status": st.Status, "updated_at": time.Now()}).Error
}

// ListAgentStatus 租户在线客服视图（join 昵称）
func ListAgentStatus(tenantID string) ([]model.AgentStatusRow, error) {
	var rows []model.AgentStatusRow
	err := config.DB.Table("agent_status AS a").
		Select("a.*, u.nickname, u.username").
		Joins("LEFT JOIN users u ON u.id = a.user_id AND u.deleted_at = 0").
		Where("a.tenant_id = ?", tenantID).
		Order("a.status DESC, a.handling_count ASC, a.id ASC").
		Scan(&rows).Error
	return rows, err
}

// PickIdleAgent 选择可分配客服：在线且未满，处理量最少优先
// 返回 (userID, nickname)；无合适客服返回 (0, "")
func PickIdleAgent(tx *gorm.DB, tenantID string) (uint64, string, error) {
	var row struct {
		UserID   uint64
		Nickname string
	}
	err := tx.Table("agent_status AS a").
		Select("a.user_id, COALESCE(NULLIF(u.nickname, ''), u.username) AS nickname").
		Joins("JOIN users u ON u.id = a.user_id AND u.deleted_at = 0 AND u.status = 1").
		Where("a.tenant_id = ? AND a.status = 1 AND a.handling_count < a.max_concurrent", tenantID).
		Order("a.handling_count ASC, a.id ASC").
		Limit(1).
		Scan(&row).Error
	if err != nil {
		return 0, "", err
	}
	return row.UserID, row.Nickname, nil
}
