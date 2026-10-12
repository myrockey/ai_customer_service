package service

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"customer_service/internal/config"
	"customer_service/internal/model"
	"customer_service/internal/pkg/agent"
	"customer_service/internal/pkg/auth"
	"customer_service/internal/pkg/ws"
	"customer_service/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TicketListInput 工单列表查询参数
type TicketListInput struct {
	Status     int    // -1 表示全部
	Page       int
	Size       int
	HandlerUID uint64 // >0 时只看该客服的工单（scope=mine）
	Keyword    string // 工单号 / 用户ID / 接管人 模糊匹配
	Priority   int    // -1 表示全部；0 低 1 中 2 高 3 紧急
}

// TicketListResult 工单列表返回结果
type TicketListResult struct {
	List  []model.Ticket `json:"list"`
	Total int64          `json:"total"`
	Page  int            `json:"page"`
	Size  int            `json:"size"`
}

// ListTickets 查询工单列表（含未读消息数填充）
func ListTickets(tenantID string, input TicketListInput) (*TicketListResult, error) {
	if input.Page < 1 {
		input.Page = 1
	}
	if input.Size < 1 || input.Size > 100 {
		input.Size = 20
	}
	list, total, err := repository.ListTickets(tenantID, input.Status, input.Page, input.Size,
		repository.TicketListOpt{HandlerUID: input.HandlerUID, Keyword: input.Keyword, Priority: input.Priority})
	if err != nil {
		return nil, err
	}	// 填充每个工单的未读消息数
	for i := range list {
		if count, err := repository.GetUnreadCount(list[i].ThreadID); err == nil {
			list[i].UnreadCount = count
		}
	}
	return &TicketListResult{
		List:  list,
		Total: total,
		Page:  input.Page,
		Size:  input.Size,
	}, nil
}

// GetTicketByID 获取工单（不含权限校验，由调用方判断）
func GetTicketByID(id uint64) (*model.Ticket, error) {
	return repository.GetTicket(id)
}

// GetTicketByNo 根据工单号获取工单
func GetTicketByNo(ticketNo string) (*model.Ticket, error) {
	return repository.GetTicketByNo(ticketNo)
}

// GetTicketMessages 获取工单会话消息
func GetTicketMessages(tk *model.Ticket) ([]model.Message, error) {
	// 工单会话窗口：人工客服可查看该工单之前/期间的用户完整历史（上下文），
	// 同时按窗口上界隔离后续工单/未接管期间的消息。
	// 窗口上界：待处理=创建时刻（不显示创建后的对话）；接管中=当前；已关闭/取消=关闭/取消时刻
	var upper time.Time
	switch tk.Status {
	case model.TicketStatusPending:
		upper = tk.CreatedAt.Add(2 * time.Second) // +2s 覆盖"已提交"系统消息边界
	case model.TicketStatusHandling:
		upper = time.Now()
	default:
		if tk.ClosedAt != nil {
			upper = tk.ClosedAt.Add(2 * time.Second)
		} else {
			upper = tk.CreatedAt.Add(2 * time.Second)
		}
	}
	var msgs []model.Message
	err := config.DB.Where("thread_id = ? AND created_at <= ?", tk.ThreadID, upper).
		Order("id ASC").Find(&msgs).Error
	if err != nil {
		return nil, err
	}
	// 合并查归档表：超过保留期被归档的旧消息仍需在工单历史中可见
	var archived []model.ArchivedMessage
	if err := config.DB.Where("thread_id = ? AND created_at <= ?", tk.ThreadID, upper).
		Order("id ASC").Find(&archived).Error; err == nil && len(archived) > 0 {
		for _, a := range archived {
			msgs = append(msgs, model.Message{
				ID: a.ID, ThreadID: a.ThreadID, Role: a.Role, Content: a.Content,
				TenantID: a.TenantID, TicketID: a.TicketID, IsRead: a.IsRead, CreatedAt: a.CreatedAt,
			})
		}
		// 按时间合并排序（主表与归档表 ID 可能重叠，按 created_at+id 稳定排序）
		sort.SliceStable(msgs, func(i, j int) bool {
			if msgs[i].CreatedAt.Equal(msgs[j].CreatedAt) {
				return msgs[i].ID < msgs[j].ID
			}
			return msgs[i].CreatedAt.Before(msgs[j].CreatedAt)
		})
	}
	return msgs, nil
}

// EnsureTicket 获取或创建该会话的未关闭工单（HTTP 与 WS 通道共用，事务 + 行锁保证并发安全）
// 返回 (ticket, reused, error)：reused=true 表示复用了已有未关闭工单
func EnsureTicket(threadID, userID, reason string) (*model.Ticket, bool, error) {
	var t model.Ticket
	reused := false
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var ex model.Ticket
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("thread_id = ? AND status IN ?",
				threadID, []int{model.TicketStatusPending, model.TicketStatusHandling}).
			Order("id ASC").First(&ex).Error
		if err == nil {
			// 复用已有工单，更新最新转接诉求
			reused = true
			t = ex
			return tx.Model(&ex).Update("reason", reason).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// 无未关闭工单 → 新建
		nt := model.Ticket{
			ThreadID: threadID,
			TicketNo: "TK-" + uuid.NewString()[:8],
			UserID:   userID,
			Reason:   reason,
			Status:   model.TicketStatusPending,
			TenantID: auth.TenantFromUserID(userID),
		}
		if err := tx.Create(&nt).Error; err != nil {
			return err
		}
		// 工单创建日志（事务内写入）
		tx.Create(&model.TicketLog{
			TicketID:  nt.ID,
			ThreadID:  threadID,
			TenantID:  nt.TenantID,
			Action:    "create",
			OldStatus: -1, NewStatus: model.TicketStatusPending,
			OldPriority: 0, NewPriority: nt.Priority,
			Operator: "system", Remark: "用户发起转人工，创建工单",
		})
		// 自动分配：优先在线且未满的客服（处理量最少）；无合适客服保持待处理，由人工接管兜底
		if targetUID, targetName, err := repository.PickIdleAgent(tx, nt.TenantID); err == nil && targetUID > 0 {
			nt.HandlerUID = targetUID
			nt.HandlerName = targetName
			nt.Status = model.TicketStatusHandling // 已分配接管人 → 直接进入接管中，前端显示聊天框
			if err := tx.Model(&nt).Updates(map[string]interface{}{
				"handler_uid": targetUID, "handler_name": targetName, "status": model.TicketStatusHandling,
			}).Error; err == nil {
				tx.Create(&model.TicketLog{
					TicketID: nt.ID, ThreadID: threadID, TenantID: nt.TenantID,
					Action: "assign", OldStatus: model.TicketStatusPending, NewStatus: model.TicketStatusHandling,
					OldPriority: 0, NewPriority: nt.Priority,
					Operator: targetName, Remark: "系统自动分配（处理量最少优先）",
				})
				_ = repository.AdjustHandlingCount(tx, nt.TenantID, targetUID, 1)
			}
		}
		t = nt
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &t, reused, nil
}

// TakeoverTicket 人工接管工单（含 Agent 恢复、状态更新、WebSocket 推送）
// handlerUID/handlerName：接管客服身份（子账号 uid+名；app_key 管理员传 0 与租户ID），
// 写入 tickets.handler_uid/handler_name，操作日志 operator 记真实客服
func TakeoverTicket(tk *model.Ticket, waitTime int, traceID string, handlerUID uint64, handlerName string) (string, error) {
	msg := ""
	if tk.Status == model.TicketStatusHandling {
		return "该工单已在人工接管中", nil
	}
	if tk.Status == model.TicketStatusPending {
		// 线程处于 HITL 中断，先恢复 Agent 会话（best effort，失败不阻断人工接管）
		agentResp, err := agent.CallAgentResume(agent.ResumeReq{
			ThreadID:  tk.ThreadID,
			Confirmed: true,
			WaitTime:  waitTime,
			TicketID:  tk.TicketNo,
		}, traceID)
		if err != nil {
			msg = "接管成功，但恢复Agent会话失败:" + err.Error()
		} else {
			msg = agentResp.Content
		}
	}
	if handlerName == "" {
		handlerName = "system"
	}
	// 记录接管客服身份（冗余快照，客服改名不影响历史工单归属）
	tk.HandlerUID = handlerUID
	tk.HandlerName = handlerName
	if err := config.DB.Model(tk).Updates(map[string]interface{}{
		"handler_uid": handlerUID, "handler_name": handlerName,
	}).Error; err != nil {
		return "", fmt.Errorf("记录接管客服失败: %w", err)
	}
	// 客服处理量 +1（子账号客服；app_key 管理员 uid=0 不统计）
	_ = repository.AdjustHandlingCount(config.DB, tk.TenantID, handlerUID, 1)
	_ = repository.UpdateTicketStatus(tk, model.TicketStatusHandling)
	_ = repository.AddTicketLog(tk.ID, tk.ThreadID, tk.TenantID, "takeover",
		model.TicketStatusPending, model.TicketStatusHandling, tk.Priority, tk.Priority,
		handlerName, "人工接管工单")
	_ = repository.SaveTicketMessage(tk.ThreadID, "system", "人工客服已接管对话，工单号："+tk.TicketNo, tk.ID)

	// 推送给用户端
	ws.WSHub.SendToUser(tk.ThreadID, map[string]interface{}{
		"type": "human_notice", "msg": "人工客服已接管对话，工单号：" + tk.TicketNo,
	})
	// 广播对应租户管理端同步
	ws.WSHub.SendToAdminsByTenant(auth.TenantFromUserID(tk.UserID), map[string]interface{}{
		"type": "ticket_update", "ticket": tk,
	})
	if msg == "" {
		msg = "已接管"
	}
	return msg, nil
}

// ReplyTicket 人工回复工单（含消息保存、WebSocket 推送）
func ReplyTicket(tk *model.Ticket, content string) error {
	if err := repository.SaveTicketMessage(tk.ThreadID, "human", content, tk.ID); err != nil {
		return err
	}
	// 实时推送给用户端
	ws.WSHub.SendToUser(tk.ThreadID, map[string]interface{}{
		"type": "human_reply", "msg": content, "ticket_no": tk.TicketNo,
	})
	// 广播对应租户管理端同步（多管理端）
	ws.WSHub.SendToAdminsByTenant(auth.TenantFromUserID(tk.UserID), map[string]interface{}{
		"type": "human_reply", "thread_id": tk.ThreadID, "msg": content,
	})
	return nil
}

// CloseTicket 关闭工单（含状态更新、system消息、WebSocket 推送）
// 关闭后该线程恢复智能客服接待；operator 为操作客服（空则沿用 system）
func CloseTicket(tk *model.Ticket, operator string) error {
	if tk.Status == model.TicketStatusClosed {
		return nil // 已关闭，幂等
	}
	if operator == "" {
		operator = "system"
	}
	_ = repository.UpdateTicketStatus(tk, model.TicketStatusClosed)
	// 释放客服处理量 -1
	_ = repository.AdjustHandlingCount(config.DB, tk.TenantID, tk.HandlerUID, -1)
	_ = repository.AddTicketLog(tk.ID, tk.ThreadID, tk.TenantID, "close",
		model.TicketStatusHandling, model.TicketStatusClosed, tk.Priority, tk.Priority,
		operator, "关闭工单，转回智能客服")
	_ = repository.SaveTicketMessage(tk.ThreadID, "system", "本次人工会话已结束，转回智能客服", tk.ID)

	ws.WSHub.SendToUser(tk.ThreadID, map[string]interface{}{
		"type": "human_notice", "msg": "本次人工会话已结束，智能客服继续为您服务",
	})
	ws.WSHub.SendToAdminsByTenant(auth.TenantFromUserID(tk.UserID), map[string]interface{}{
		"type": "ticket_update", "ticket": tk,
	})
	return nil
}

// ApproveTicketInput 审批工单输入
type ApproveTicketInput struct {
	TicketNo  string
	Confirmed bool
	WaitTime  int
	TraceID   string
}

// TransferTicket 转交工单给其他客服（更新接管人归属、操作日志、system 消息、WebSocket 推送）
// 调用方须已校验：接管中 + 转交人身份（接管人/管理员）
func TransferTicket(tk *model.Ticket, toUID uint64, toName, operator string) error {
	if tk.Status != model.TicketStatusHandling && tk.Status != model.TicketStatusPending {
		return fmt.Errorf("仅待处理或接管中的工单可转交")
	}
	if toName == "" {
		toName = "system"
	}
	oldUID := tk.HandlerUID // 转出客服（更新前取值，用于处理量 -1）
	oldStatus := tk.Status
	newStatus := tk.Status
	if newStatus == model.TicketStatusPending {
		newStatus = model.TicketStatusHandling // 待处理工单被指派 → 进入接管中
	}
	// 更新接管人归属（冗余快照，客服改名不影响历史归属）
	tk.HandlerUID = toUID
	tk.HandlerName = toName
	tk.Status = newStatus
	if err := config.DB.Model(tk).Updates(map[string]interface{}{
		"handler_uid": toUID, "handler_name": toName, "status": newStatus,
	}).Error; err != nil {
		return fmt.Errorf("更新接管人失败: %w", err)
	}
	// 处理量：原客服 -1（无则不更新），目标客服 +1
	_ = repository.AdjustHandlingCount(config.DB, tk.TenantID, oldUID, -1)
	_ = repository.AdjustHandlingCount(config.DB, tk.TenantID, toUID, 1)
	_ = repository.AddTicketLog(tk.ID, tk.ThreadID, tk.TenantID, "transfer",
		oldStatus, newStatus, tk.Priority, tk.Priority, operator, "转交工单给客服 "+toName)
	_ = repository.SaveTicketMessage(tk.ThreadID, "system", "工单已转交给客服 "+toName+"，请继续为您服务", tk.ID)
	ws.WSHub.SendToAdminsByTenant(tk.TenantID,
		map[string]interface{}{"type": "ticket_update", "ticket": tk})
	return nil
}

// ApproveTicket 审批转接申请（确认转接或取消转接）
func ApproveTicket(tk *model.Ticket, input ApproveTicketInput) (string, error) {
	if !input.Confirmed {
		// 取消转接
		_ = repository.UpdateTicketStatus(tk, model.TicketStatusCanceled)
		_ = repository.AddTicketLog(tk.ID, tk.ThreadID, tk.TenantID, "cancel",
			model.TicketStatusPending, model.TicketStatusCanceled, tk.Priority, tk.Priority,
			"system", "取消转接申请，智能客服继续服务")
		agentResp, err := agent.CallAgentResume(agent.ResumeReq{
			ThreadID:  tk.ThreadID,
			Confirmed: false,
			WaitTime:  input.WaitTime,
			TicketID:  tk.TicketNo,
		}, input.TraceID)
		if err != nil {
			return "", err
		}
		_ = repository.SaveTicketMessage(tk.ThreadID, "system", "转接申请已取消，智能客服继续服务", tk.ID)
		ws.WSHub.SendToUser(tk.ThreadID, map[string]interface{}{
			"type": "human_notice", "msg": "转接申请已取消，智能客服继续为您服务",
		})
		ws.WSHub.SendToAdminsByTenant(auth.TenantFromUserID(tk.UserID),
			map[string]interface{}{"type": "ticket_update", "ticket": tk})
		return agentResp.Content, nil
	}
	// 确认转接 → 人工接管
	return TakeoverTicket(tk, input.WaitTime, input.TraceID, 0, "system")
}

// SetTicketPriority 设置工单优先级（含操作日志、WebSocket 推送）
func SetTicketPriority(tk *model.Ticket, priority int, operator, remark string) error {
	if priority < 0 || priority > 3 {
		return ErrInvalidParam
	}
	oldPriority := tk.Priority
	if err := config.DB.Model(tk).Update("priority", priority).Error; err != nil {
		return fmt.Errorf("更新优先级失败: %w", err)
	}
	tk.Priority = priority
	_ = repository.AddTicketLog(tk.ID, tk.ThreadID, tk.TenantID, "priority",
		tk.Status, tk.Status, oldPriority, priority, operator, remark)
	ws.WSHub.SendToAdminsByTenant(tk.TenantID,
		map[string]interface{}{"type": "ticket_update", "ticket": tk})
	return nil
}

// GetTicketLogs 获取工单操作日志
func GetTicketLogs(ticketID uint64) ([]model.TicketLog, error) {
	return repository.ListTicketLogs(ticketID)
}
