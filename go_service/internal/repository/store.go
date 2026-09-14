package repository

import (
	"errors"
	"time"

	"customer_service/internal/config"
	"customer_service/internal/model"
	"customer_service/internal/pkg/auth"
	"customer_service/internal/pkg/ws"

	"gorm.io/gorm"
)

// ErrTicketNotFound 工单不存在
var ErrTicketNotFound = errors.New("ticket not found")

// HumanSessionTimeout 人工接管活跃超时：超过该时长无任何人工活动，
// 自动结束人工接管并转回智能客服，避免用户被无限期卡在“人工接管”状态。
const HumanSessionTimeout = 10 * time.Minute

// SaveMessage 保存一条会话消息（租户从 thread_id 前缀提取）
// 不归属任何工单：用于待处理/未接管期间的 AI 对话
func SaveMessage(threadID, role, content string) error {
	return saveMessage(threadID, role, content, nil)
}

// SaveTicketMessage 保存一条归属指定工单的会话消息（人工会话专用）
func SaveTicketMessage(threadID, role, content string, ticketID uint64) error {
	return saveMessage(threadID, role, content, &ticketID)
}

func saveMessage(threadID, role, content string, ticketID *uint64) error {
	if content == "" {
		return nil
	}
	// 用户发送的消息默认已读；AI/人工/系统消息默认未读
	isRead := 0
	if role == "user" {
		isRead = 1
	}
	m := model.Message{
		ThreadID: threadID,
		Role:     role,
		Content:  content,
		TenantID: auth.TenantFromThreadID(threadID),
		TicketID: ticketID,
		IsRead:   isRead,
	}
	return config.DB.Create(&m).Error
}

// AssignLastUserMessage 将某会话最新一条用户消息归属到指定工单（转接诉求消息）
func AssignLastUserMessage(threadID string, ticketID uint64) error {
	return config.DB.Model(&model.Message{}).
		Where("thread_id = ? AND role = ? AND (ticket_id IS NULL OR ticket_id = 0)", threadID, "user").
		Order("id DESC").Limit(1).
		Update("ticket_id", ticketID).Error
}

// GetHandlingTicket 获取会话当前人工接管中的工单（无则返回 nil）
func GetHandlingTicket(threadID string) (*model.Ticket, error) {
	var tk model.Ticket
	err := config.DB.Where("thread_id = ? AND status = ?", threadID, model.TicketStatusHandling).
		Order("id DESC").First(&tk).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &tk, nil
}

// HasPendingTicket 判断会话是否存在待处理（未接管）工单
func HasPendingTicket(threadID string) (bool, error) {
	var count int64
	err := config.DB.Model(&model.Ticket{}).
		Where("thread_id = ? AND status = ?", threadID, model.TicketStatusPending).
		Count(&count).Error
	return count > 0, err
}

// ListTicketMessages 返回某工单归属的全部消息（按时间正序）
func ListTicketMessages(ticketID uint64) ([]model.Message, error) {
	var msgs []model.Message
	err := config.DB.Where("ticket_id = ?", ticketID).Order("id ASC").Find(&msgs).Error
	return msgs, err
}

// ListMessages 按时间正序返回某会话全部消息（线程级，用户端历史用）
func ListMessages(threadID string) ([]model.Message, error) {
	var msgs []model.Message
	err := config.DB.Where("thread_id = ?", threadID).Order("id ASC").Find(&msgs).Error
	return msgs, err
}

// MarkMessagesRead 标记某会话的所有非用户消息为已读
func MarkMessagesRead(threadID string) error {
	return config.DB.Model(&model.Message{}).
		Where("thread_id = ? AND role != ? AND is_read = ?", threadID, "user", 0).
		Update("is_read", 1).Error
}

// GetUnreadCount 获取某会话的未读消息数
func GetUnreadCount(threadID string) (int64, error) {
	var count int64
	err := config.DB.Model(&model.Message{}).
		Where("thread_id = ? AND role != ? AND is_read = ?", threadID, "user", 0).
		Count(&count).Error
	return count, err
}

// HasOpenTicket 判断会话是否存在未关闭工单（待处理/接管中）
func HasOpenTicket(threadID string) (bool, error) {
	var count int64
	err := config.DB.Model(&model.Ticket{}).
		Where("thread_id = ? AND status IN ?", threadID,
			[]int{model.TicketStatusPending, model.TicketStatusHandling}).
		Count(&count).Error
	return count > 0, err
}

// IsThreadHumanServed 判断线程当前是否处于「有效」的人工接管状态。
// 以最后一次人工活动时间为基准（人工回复 / 人工接管提示 / 工单创建，取最晚）：
// 若超过 HumanSessionTimeout 仍无人处理，则自动关闭该工单并转回智能客服，
// 避免历史遗留的“接管中”工单把用户的普通消息全部吞进人工队列。
func IsThreadHumanServed(threadID string) (bool, error) {
	var tk model.Ticket
	err := config.DB.Where("thread_id = ? AND status = ?", threadID, model.TicketStatusHandling).
		Order("id DESC").First(&tk).Error
	if tk.ID == 0 {
		return false, nil // 无接管中工单
	}
	if err != nil {
		return false, err
	}

	// 最后一次人工活动时间：人工回复 / 接管提示 / 工单创建，取最晚
	lastActive := tk.CreatedAt
	if t := lastHumanActivity(threadID); !t.IsZero() && t.After(lastActive) {
		lastActive = t
	}
	if t := lastTakeoverTime(threadID); !t.IsZero() && t.After(lastActive) {
		lastActive = t
	}

	// 超时无人处理 → 自动结束人工接管，转回智能客服
	if time.Since(lastActive) > HumanSessionTimeout {
		_ = UpdateTicketStatus(&tk, model.TicketStatusClosed)
		_ = SaveMessage(threadID, "system", "人工客服长时间未响应，已自动转回智能客服")
		ws.WSHub.SendToUser(threadID, map[string]interface{}{
			"type": "human_notice",
			"msg":  "人工客服暂时无法响应，已自动转回智能客服，我可以继续为您服务",
		})
		ws.WSHub.SendToAdmins(map[string]interface{}{"type": "ticket_update", "ticket": tk})
		return false, nil
	}

	return true, nil
}

type timeRow struct {
	T time.Time
}

// lastHumanActivity 该线程最后一条人工回复时间
func lastHumanActivity(threadID string) time.Time {
	var r timeRow
	config.DB.Model(&model.Message{}).
		Select("created_at AS t").
		Where("thread_id = ? AND role = ?", threadID, "human").
		Order("id DESC").Limit(1).
		Scan(&r)
	return r.T
}

// lastTakeoverTime 该线程人工接管系统提示时间
func lastTakeoverTime(threadID string) time.Time {
	var r timeRow
	config.DB.Model(&model.Message{}).
		Select("created_at AS t").
		Where("thread_id = ? AND role = ? AND content LIKE ?", threadID, "system", "%人工客服已接管%").
		Order("id DESC").Limit(1).
		Scan(&r)
	return r.T
}

// ListTickets 工单列表（status=-1 表示全部；tenantID 非空时按 tenant_id 精确过滤；
// 平台管理员传空字符串查看全部租户）
func ListTickets(tenantID string, status, page, size int) ([]model.Ticket, int64, error) {
	q := config.DB.Model(&model.Ticket{})
	if tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if status >= 0 {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.Ticket
	if err := q.Order("created_at DESC, id DESC").
		Offset((page - 1) * size).Limit(size).
		Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// GetTicket 按主键查询工单
func GetTicket(id uint64) (*model.Ticket, error) {
	var t model.Ticket
	if err := config.DB.First(&t, id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTicketByNo 按工单号查询工单
func GetTicketByNo(no string) (*model.Ticket, error) {
	var t model.Ticket
	if err := config.DB.Where("ticket_no = ?", no).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateTicketStatus 更新工单状态
func UpdateTicketStatus(t *model.Ticket, status int) error {
	t.Status = status
	// 关闭/取消时记录关闭时刻（工单详情消息窗口上界）
	if status == model.TicketStatusClosed || status == model.TicketStatusCanceled {
		now := time.Now()
		t.ClosedAt = &now
	}
	return config.DB.Save(t).Error
}

// AddTicketLog 记录工单操作历史
func AddTicketLog(ticketID uint64, threadID, tenantID, action string, oldStatus, newStatus, oldPriority, newPriority int, operator, remark string) error {
	log := model.TicketLog{
		TicketID: ticketID, ThreadID: threadID, TenantID: tenantID,
		Action: action, OldStatus: oldStatus, NewStatus: newStatus,
		OldPriority: oldPriority, NewPriority: newPriority,
		Operator: operator, Remark: remark,
	}
	return config.DB.Create(&log).Error
}

// ListTicketLogs 查询工单操作历史（按时间正序）
func ListTicketLogs(ticketID uint64) ([]model.TicketLog, error) {
	var logs []model.TicketLog
	err := config.DB.Where("ticket_id = ?", ticketID).Order("id ASC").Find(&logs).Error
	return logs, err
}
