package chat

import (
	"errors"
	"time"

	"customer_service/internal/pkg/agent"
	"customer_service/internal/pkg/auth"
	"customer_service/internal/pkg/ws"
	"customer_service/internal/repository"
	"customer_service/internal/service"
)

// ChatResult 用户消息处理结果
type ChatResult struct {
	Kind     string // reply / ticket / human / busy / error
	Msg      string
	TicketNo string
	Err      error
}

// ProcessUserMessage 统一处理用户消息（HTTP 与 WebSocket 共用）。
// 流程：
//  1. 落库用户消息
//  2. 若该线程处于人工接管状态 → 转给管理端，不再调用 Agent
//  3. 否则调用 Python Agent 获取回复；遇 HITL 中断则创建工单并通知管理端
func ProcessUserMessage(threadID, userID, msg, traceID string) ChatResult {
	_ = repository.SaveMessage(threadID, "user", msg)

	// 人工接管中：用户消息归属当前工单，直接转人工（按租户推送给对应管理端）
	htk, err := repository.GetHandlingTicket(threadID)
	if err == nil && htk != nil {
		_ = repository.AssignLastUserMessage(threadID, htk.ID)
		ws.WSHub.SendToAdminsByTenant(auth.TenantFromUserID(userID), map[string]interface{}{
			"type": "user_msg", "thread_id": threadID, "user_id": userID, "msg": msg,
		})
		return ChatResult{Kind: "human", Msg: "您当前由人工客服接待，消息已转给人工客服，请稍候"}
	}

	// 待处理（未接管）工单：用户消息继续由 AI 接待，不推送管理端、不归属工单——
	// 客服只有真正接管后才看到用户消息，待处理工单详情不显示后续对话
	_ = htk

	// P1-2：每日消息数配额校验（超限直接拒绝，不调用 Agent）
	tenantID := auth.TenantFromUserID(userID)
	if quota, qErr := repository.CheckDailyMessageQuota(tenantID); qErr == nil && !quota.OK {
		return ChatResult{Kind: "quota", Msg: "今日咨询量已达上限，请明天再试或联系人工客服"}
	}

	// 线程级串行，避免同一会话并发调用 Agent
	if !ws.WSHub.TryLockThread(threadID) {
		return ChatResult{Kind: "busy", Msg: "上一条消息还在处理中，请稍候再发送"}
	}
	defer ws.WSHub.UnlockThread(threadID)

	// 存在待处理工单（排队中未接管）：agent 正常回答，不再重复触发转人工中断
	pending, _ := repository.HasPendingTicket(threadID)
	agentResp, err := agent.CallAgentChat(threadID, msg, traceID, pending)
	if err != nil {
		return ChatResult{Kind: "error", Err: err}
	}

	// Agent 明确失败（限流/超时/内部异常）→ 不再透传错误文本当正常回复
	if !agentResp.Success {
		return ChatResult{Kind: "error", Err: errors.New(agentResp.Content)}
	}

	if agentResp.IsInterrupt {
		reason := ""
		if agentResp.InterruptData != nil {
			if r, ok := agentResp.InterruptData["reason"].(string); ok {
				reason = r
			}
		}
		tenantID := auth.TenantFromUserID(userID)

		// 复用/新建未关闭工单（事务+行锁，HTTP/WS 通道共用，避免并发重复建单）
		t, reused, err := service.EnsureTicket(threadID, userID, reason)
		if err != nil {
			return ChatResult{Kind: "error", Err: err}
		}
		if reused {
			ws.WSHub.SendToAdminsByTenant(tenantID, map[string]interface{}{
				"type": "ticket_update", "ticket": t,
			})
			return ChatResult{Kind: "ticket", TicketNo: t.TicketNo, Msg: "您已提交人工客服，正在等待处理（工单号：" + t.TicketNo + "）"}
		}
		_ = repository.SaveTicketMessage(threadID, "system", "已提交人工客服，工单号："+t.TicketNo, t.ID)
		// 转接诉求消息归属新工单（工单会话隔离：详情只显示本工单消息）
		_ = repository.AssignLastUserMessage(threadID, t.ID)

		// 立即恢复 Agent 会话（best effort）：清掉挂起中断，排队期间用户消息走正常 AI 回答
		_, _ = agent.CallAgentResume(agent.ResumeReq{
			ThreadID: threadID, Confirmed: true, WaitTime: 3, TicketID: t.TicketNo,
		}, traceID)

		// 通知对应租户的管理端有新工单
		ws.WSHub.SendToAdminsByTenant(tenantID, map[string]interface{}{
			"type": "new_ticket",
			"ticket": map[string]interface{}{
				"id": t.ID, "ticket_no": t.TicketNo, "thread_id": threadID,
				"user_id": userID, "reason": reason, "status": t.Status,
				"created_at": t.CreatedAt.Format(time.RFC3339),
			},
		})
		return ChatResult{Kind: "ticket", TicketNo: t.TicketNo, Msg: "已提交人工客服，请等待处理"}
	}

	content := agentResp.Content
	// AI 回复：接管中归属当前工单（人工会话记录）；待处理/普通对话不归属
	if htk, _ := repository.GetHandlingTicket(threadID); htk != nil {
		_ = repository.SaveTicketMessage(threadID, "ai", content, htk.ID)
	} else {
		_ = repository.SaveMessage(threadID, "ai", content)
	}
	return ChatResult{Kind: "reply", Msg: content}
}
