package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"customer_service/internal/config"
)

type ChatReq struct {
	ThreadID    string `json:"thread_id"`
	UserMessage string `json:"user_message"`
	// HasPendingTicket 线程存在待处理工单（未接管）：agent 不再重复触发转人工中断，正常回答用户
	HasPendingTicket bool `json:"has_pending_ticket"`
}

type ChatResp struct {
	Success       bool                   `json:"success"`
	IsInterrupt   bool                   `json:"is_interrupt"`
	InterruptData map[string]interface{} `json:"interrupt_data"`
	Content       string                 `json:"content"`
}

type ResumeReq struct {
	ThreadID  string `json:"thread_id"`
	Confirmed bool   `json:"confirmed"`
	WaitTime  int    `json:"wait_time"`
	TicketID  string `json:"ticket_id"`
}

var httpClient = &http.Client{Timeout: 60 * time.Second}

// adminHTTPClient 用于 Prompt/知识库/会话清理等管理操作（可能耗时较长）
var adminHTTPClient = &http.Client{Timeout: 120 * time.Second}

// PrewarmTenant 预热指定租户的 Agent（Qdrant 集合 + Prompt + Agent 实例懒构建）
func PrewarmTenant(tenantID string) error {
	b, _ := json.Marshal(map[string]string{"tenant_id": tenantID})
	_, err := AgentRequest(http.MethodPost, "/agent/tenants/prewarm", "",
		"application/json", bytes.NewReader(b), "prewarm", "")
	return err
}

// AgentRequest 向 Python Agent 发起任意 HTTP 请求并返回响应体。
// 用于管理后台透传（Prompt管理、知识库、会话清理等）。tenantID 透传租户上下文。
func AgentRequest(method, urlPath, query, contentType string, body io.Reader, traceID, tenantID string) ([]byte, error) {
	full := config.GlobalConfig.AgentURL + urlPath
	if query != "" {
		full += "?" + query
	}
	req, err := http.NewRequest(method, full, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("X-Trace-Id", traceID)
	if tenantID != "" {
		req.Header.Set("X-Tenant-Id", tenantID)
	}
	// 管理后台透传请求统一携带内部 token（agent 侧内部接口校验，如 platform-settings）
	if token := config.GlobalConfig.InternalAPIToken; token != "" {
		req.Header.Set("X-Internal-Token", token)
	}
	resp, err := adminHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func postWithTrace(url string, payload any, traceId string) (*http.Response, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trace-Id", traceId)
	return httpClient.Do(req)
}

// 简单重试，最多2次
func CallAgentChat(threadID, msg string, traceId string, hasPending bool) (*ChatResp, error) {
	payload := ChatReq{ThreadID: threadID, UserMessage: msg, HasPendingTicket: hasPending}
	url := fmt.Sprintf("%s/agent/chat", config.GlobalConfig.AgentURL)
	var resp *http.Response
	var err error
	for i := 0; i < 2; i++ {
		resp, err = postWithTrace(url, payload, traceId)
		if err == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var res ChatResp
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return &res, nil
}

// StreamChunk SSE 流式事件
type StreamChunk struct {
	Type    string                 `json:"type"`    // chunk / end / error / interrupt
	Content string                 `json:"content"` // chunk 或 end 的内容
	Msg     string                 `json:"msg"`     // error 消息
	Data    map[string]interface{} `json:"data"`    // interrupt 数据
}

// CallAgentChatStream P2-1：流式调用 Agent，逐 token 回调
// onChunk: 每个 token 片段回调
// onEnd: 流式结束回调，参数为完整内容
// onError: 错误回调
// onInterrupt: 人工接管中断回调
// 新增 ctx：上层WS的context，前端断开会自动cancel
func CallAgentChatStream(ctx context.Context, threadID, msg, traceId string, hasPending bool,
	onChunk func(string), onEnd func(string),
	onError func(error), onInterrupt func(map[string]interface{})) error {

	payload := ChatReq{ThreadID: threadID, UserMessage: msg, HasPendingTicket: hasPending}
	url := fmt.Sprintf("%s/agent/chat/stream", config.GlobalConfig.AgentURL)

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal chat req: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trace-Id", traceId)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agent stream status %d: %s", resp.StatusCode, string(body))
	}

	// 逐行读取 SSE
	reader := io.Reader(resp.Body)
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	for {
		n, err := reader.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			// 处理完整的行
			for {
				idx := -1
				for i, b := range buf {
					if b == '\n' {
						idx = i
						break
					}
				}
				if idx < 0 {
					break
				}
				line := string(buf[:idx])
				buf = buf[idx+1:]

				// 解析 SSE 行：data: {...}
				if len(line) > 6 && line[:6] == "data: " {
					jsonStr := line[6:]
					var chunk StreamChunk
					if err := json.Unmarshal([]byte(jsonStr), &chunk); err == nil {
						switch chunk.Type {
						case "chunk":
							if onChunk != nil {
								onChunk(chunk.Content)
							}
						case "end":
							if onEnd != nil {
								onEnd(chunk.Content)
							}
							return nil
						case "error":
							if onError != nil {
								onError(fmt.Errorf("%s", chunk.Msg))
							}
							return nil
						case "interrupt":
							if onInterrupt != nil {
								onInterrupt(chunk.Data)
							}
							return nil
						}
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func CallAgentResume(req ResumeReq, traceId string) (*ChatResp, error) {
	url := fmt.Sprintf("%s/agent/resume", config.GlobalConfig.AgentURL)
	var resp *http.Response
	var err error
	for i := 0; i < 2; i++ {
		resp, err = postWithTrace(url, req, traceId)
		if err == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var res ChatResp
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return &res, nil
}
