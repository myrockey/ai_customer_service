package handler

import (
	"context"
	"customer_service/internal/config"
	"customer_service/internal/middleware"
	"customer_service/internal/pkg/agent"
	"customer_service/internal/pkg/auth"
	"customer_service/internal/pkg/ws"
	"customer_service/internal/repository"
	"customer_service/internal/service"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	// 开发环境放行跨域；生产由网关层处理
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleUserWS 用户端 WebSocket：/ws/chat?user_id=xxx
// 客户端消息协议：{"type":"chat","msg":"..."} / {"type":"ping"}
func HandleUserWS() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Query("user_id")
		if userID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "缺少 user_id"})
			return
		}

		// P0-2：WebSocket 连接数限制（单 IP + 单 user_id）
		clientIP := c.ClientIP()
		allowed, reason := ws.GlobalWSLimiter.TryAcquireConn(clientIP, userID)
		if !allowed {
			c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "msg": reason})
			return
		}
		defer ws.GlobalWSLimiter.ReleaseConn(clientIP, userID)

		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}

		// P1-2：租户并发会话配额（实时连接数）校验，超限拒绝并通知前端停止重连
		tenantID := auth.TenantFromThreadID(userID)
		if quota, qErr := repository.GetTenantQuota(tenantID); qErr == nil && quota.MaxConcurrentSessions > 0 {
			current := ws.WSHub.CountTenantConnsExcluding(tenantID, userID)
			if current >= quota.MaxConcurrentSessions {
				msg := "{\"type\":\"error\",\"code\":\"concurrency_full\",\"msg\":\"当前在线会话数已达上限（" + strconv.Itoa(quota.MaxConcurrentSessions) + "），请稍后再试\"}"
				_ = conn.WriteMessage(websocket.TextMessage, []byte(msg))
				_ = conn.Close()
				return
			}
		}

		cl := ws.WSHub.RegisterUser(userID, tenantID, conn)
		defer ws.WSHub.UnregisterUser(userID, cl)
		go cl.WriteLoop()

		// 读超时：客户端异常断开（未发 close 帧）时及时释放连接与租户配额
		// 前端每 30s 心跳，90s 未收到任何消息视为断开
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))

		// 连接建立后统一推送历史消息（空历史也发，前端据此渲染"暂无历史/欢迎语"）
		if msgs, err := repository.ListMessages(userID); err == nil {
			ws.WSHub.SendToUser(userID, map[string]interface{}{
				"type": "history", "messages": msgs,
			})
		}

		for {
			_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				Type string `json:"type"`
				Msg  string `json:"msg"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			switch msg.Type {
			case "ping":
				ws.WSHub.SendToUser(userID, map[string]interface{}{"type": "pong"})
			case "read":
				// P1-1：用户上报已读，标记该会话所有非用户消息为已读
				// 并通过管理端 WebSocket 推送已读回执
				if err := repository.MarkMessagesRead(userID); err == nil {
					tenantID := auth.TenantFromThreadID(userID)
					ws.WSHub.SendToAdminsByTenant(tenantID, map[string]interface{}{
						"type":      "read_receipt",
						"thread_id": userID,
						"timestamp": time.Now().Unix(),
					})
				}
			case "chat":
				if msg.Msg == "" {
					continue
				}
				// P0-2：消息发送频率限制（每秒最多 5 条）
				allowed, reason := ws.GlobalWSLimiter.TryAcquireMsg(userID)
				if !allowed {
					ws.WSHub.SendToUser(userID, map[string]interface{}{
						"type": "rate_limit", "msg": reason,
					})
					continue
				}
				traceID := c.GetString(config.TraceIDKey)
				// P2-1：流式输出处理
				// 1. 落库用户消息
				_ = repository.SaveMessage(userID, "user", msg.Msg)

				// 2. 人工接管中：消息归属当前工单，直接转人工
				htk, _ := repository.GetHandlingTicket(userID)
				if htk != nil {
					_ = repository.AssignLastUserMessage(userID, htk.ID)
					ws.WSHub.SendToAdminsByTenant(auth.TenantFromThreadID(userID), map[string]interface{}{
						"type": "user_msg", "thread_id": userID, "user_id": userID, "msg": msg.Msg,
					})
					ws.WSHub.SendToUser(userID, map[string]interface{}{
						"type": "human_notice", "msg": "您当前由人工客服接待，消息已转给人工客服，请稍候",
					})
					continue
				}

				// 3. 线程级串行锁
				if !ws.WSHub.TryLockThread(userID) {
					ws.WSHub.SendToUser(userID, map[string]interface{}{
						"type": "notice", "msg": "上一条消息还在处理中，请稍候再发送",
					})
					continue
				}

				// P1-2：每日消息数配额校验（超限直接拒绝，需先释放线程锁）
				if quota, qErr := repository.CheckDailyMessageQuota(auth.TenantFromUserID(userID)); qErr == nil && !quota.OK {
					ws.WSHub.UnlockThread(userID)
					ws.WSHub.SendToUser(userID, map[string]interface{}{
						"type": "quota", "msg": "今日咨询量已达上限，请明天再试或联系人工客服",
					})
					continue
				}

				fullContent := ""
				pending, _ := repository.HasPendingTicket(userID)

				// ✅ 继承WS请求的context，连接断开自动取消SSE请求
				ctx := c.Request.Context()

				// ✅ 新开goroutine跑SSE流式，**不阻塞WS主循环**
				// ✅ 线程锁在子协程内释放：流式结束/异常/中断后自动解锁，避免占用后续消息
				go func() {
					// 子协程内执行SSE
					defer ws.WSHub.UnlockThread(userID)
					streamErr := agent.CallAgentChatStream(ctx, userID, msg.Msg, traceID, pending,
						// onChunk: 逐 token 推送给前端
						func(chunk string) {
							fullContent += chunk
							ws.WSHub.SendToUser(userID, map[string]interface{}{
								"type": "chat_chunk", "chunk": chunk,
							})
						},
						// onEnd: 流式结束，推送完整消息并落库
						func(content string) {
							if htk, _ := repository.GetHandlingTicket(userID); htk != nil {
								_ = repository.SaveTicketMessage(userID, "ai", content, htk.ID)
							} else {
								_ = repository.SaveMessage(userID, "ai", content)
							}
							ws.WSHub.SendToUser(userID, map[string]interface{}{
								"type": "chat_end", "msg": content,
							})
						},
						// onError: Agent业务侧返回SSE error事件，仅这里推送业务错误
						func(err error) {
							ws.WSHub.SendToUser(userID, map[string]interface{}{
								"type": "error", "msg": "调用agent失败:" + err.Error(),
							})
						},
						// onInterrupt: 人工接管中断，复用/创建工单（事务+行锁，与 HTTP 通道共用）
						func(interruptData map[string]interface{}) {
							reason := ""
							if interruptData != nil {
								if r, ok := interruptData["reason"].(string); ok {
									reason = r
								}
							}
							t, reused, err := service.EnsureTicket(userID, userID, reason)
							if err != nil {
								ws.WSHub.SendToUser(userID, map[string]interface{}{
									"type": "error", "msg": "创建工单失败:" + err.Error(),
								})
								return
							}
							if !reused {
								_ = repository.SaveTicketMessage(userID, "system", "已提交人工客服，工单号："+t.TicketNo, t.ID)
								// 转接诉求消息归属新工单
								_ = repository.AssignLastUserMessage(userID, t.ID)
								ws.WSHub.SendToAdminsByTenant(auth.TenantFromUserID(userID), map[string]interface{}{
									"type": "new_ticket",
									"ticket": map[string]interface{}{
										"id": t.ID, "ticket_no": t.TicketNo, "thread_id": userID,
										"user_id": userID, "reason": reason, "status": t.Status,
										"created_at": t.CreatedAt.Format(time.RFC3339),
									},
								})
							}
							ws.WSHub.SendToUser(userID, map[string]interface{}{
								"type": "ticket", "ticket_no": t.TicketNo,
								"reused": reused, "msg": "已提交人工客服，请等待处理",
							})
							_, _ = agent.CallAgentResume(agent.ResumeReq{
								ThreadID: userID, Confirmed: true, WaitTime: 3, TicketID: t.TicketNo,
							}, traceID)
						},
					)
					// ✅ 只处理【网络层面错误】，**排除ctx取消（用户主动关闭页面，不需要推送错误）**
					if streamErr != nil {
						// context取消属于正常关闭，不向前端报错
						if !errors.Is(streamErr, context.Canceled) && !errors.Is(streamErr, context.DeadlineExceeded) {
							ws.WSHub.SendToUser(userID, map[string]interface{}{
								"type": "error", "msg": "agent流式连接异常:" + streamErr.Error(),
							})
						}
					}
				}()

			}
		}
	}
}

// handleAdminWS 管理端 WebSocket：/ws/admin?token=xxx
// 用于实时接收：新工单(new_ticket)、工单状态变更(ticket_update)、用户消息(user_msg)、人工回复(human_reply)
// 按 token 租户订阅：仅收到本租户的推送
func HandleAdminWS() gin.HandlerFunc {
	return func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		cl := ws.WSHub.RegisterAdmin(conn, middleware.TenantOf(c))
		defer ws.WSHub.UnregisterAdmin(cl)
		go cl.WriteLoop()

		// 管理端只接收服务端推送，忽略客户端消息
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}
}
