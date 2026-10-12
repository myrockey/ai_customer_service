package repository

import (
	"customer_service/internal/config"
)

// ====================== 会话摘要（AI 自动生成） ======================

// SummaryMessage 摘要用消息（role/content）
type SummaryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// PendingSummaryCandidate 待生成摘要的候选会话（含最近消息，供 agent 生成摘要）
type PendingSummaryCandidate struct {
	ThreadID string           `json:"thread_id"`
	TenantID string           `json:"tenant_id"`
	Messages []SummaryMessage `json:"messages"`
}

// PendingSummaryCandidates 返回"最后活跃超过 30 分钟且尚无摘要"的会话（最多 limit 个）。
// 按最后活跃倒序优先处理刚结束的会话；agent 生成摘要后经 UpsertSessionSummary 回写。
func PendingSummaryCandidates(limit int) ([]PendingSummaryCandidate, error) {
	var threads []struct {
		ThreadID string
		TenantID string
	}
	if err := config.DB.Raw(`
		SELECT m.thread_id, m.tenant_id, MAX(m.created_at) AS last_active
		FROM messages m
		LEFT JOIN session_summaries s ON s.thread_id = m.thread_id AND s.tenant_id = m.tenant_id
		WHERE s.thread_id IS NULL AND m.created_at < DATE_SUB(NOW(), INTERVAL 30 MINUTE)
		GROUP BY m.thread_id, m.tenant_id
		ORDER BY last_active DESC
		LIMIT ?`, limit).Scan(&threads).Error; err != nil {
		return nil, err
	}
	if len(threads) == 0 {
		return []PendingSummaryCandidate{}, nil
	}
	cands := make([]PendingSummaryCandidate, 0, len(threads))
	for _, t := range threads {
		// 取最近 40 条（倒序取，再反转成正序）
		var msgs []SummaryMessage
		if err := config.DB.Raw(`
			SELECT role, content FROM messages
			WHERE thread_id = ? AND tenant_id = ?
			ORDER BY id DESC LIMIT 40`, t.ThreadID, t.TenantID).Scan(&msgs).Error; err != nil {
			continue
		}
		for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
			msgs[i], msgs[j] = msgs[j], msgs[i]
		}
		cands = append(cands, PendingSummaryCandidate{
			ThreadID: t.ThreadID,
			TenantID: t.TenantID,
			Messages: msgs,
		})
	}
	return cands, nil
}

// UpsertSessionSummary 写入/更新会话摘要（幂等，UNIQUE 键冲突则更新）
func UpsertSessionSummary(tenantID, threadID, summary string) error {
	return config.DB.Exec(`
		INSERT INTO session_summaries (tenant_id, thread_id, summary)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE summary = VALUES(summary)`, tenantID, threadID, summary).Error
}
