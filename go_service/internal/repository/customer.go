package repository

import (
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"customer_service/internal/config"
	"customer_service/internal/model"

	"gorm.io/gorm/clause"
)

// ====================== 客户信息提取规则 ======================
// 规则存 platform_config（key=cs_extract_rule_{field}），未配置时用内置默认；
// 包级原子缓存，规则更新接口写库后同步刷新，避免每轮消息都查 DB。

// defaultExtractRules 内置默认规则（零配置即可用）：
//   - phone：中国大陆手机号（容忍空格/短横线/86 前缀）
//   - email：通用邮箱
var defaultExtractRules = []model.ExtractRule{
	// phone：中国大陆手机号（容忍空格/短横线/86 前缀）。
	// 注意：Go regexp(RE2) 不支持 lookbehind/lookahead，数字边界在 extractFromContent 里代码校验
	{Field: "phone", Pattern: `(?:\+?86[\s-]?)?1[3-9]\d[\s-]?\d{4}[\s-]?\d{4}`},
	{Field: "email", Pattern: `[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`},
}

var extractRulesCache atomic.Value // []model.ExtractRule

func loadExtractRules() []model.ExtractRule {
	if v, ok := extractRulesCache.Load().([]model.ExtractRule); ok && len(v) > 0 {
		return v
	}
	var rules []model.ExtractRule
	var rows []model.PlatformConfig
	if err := config.DB.Where("config_key LIKE ?", "cs_extract_rule_%").Find(&rows).Error; err == nil {
		for _, r := range rows {
			field := strings.TrimPrefix(r.ConfigKey, "cs_extract_rule_")
			if field != "" && r.ConfigValue != "" {
				rules = append(rules, model.ExtractRule{Field: field, Pattern: r.ConfigValue})
			}
		}
	}
	if len(rules) == 0 {
		rules = defaultExtractRules
	}
	extractRulesCache.Store(rules)
	return rules
}

// GetExtractRules 返回当前生效的提取规则
func GetExtractRules() []model.ExtractRule {
	return loadExtractRules()
}

// SaveExtractRules 更新提取规则（先删旧规则再写新规则），并刷新缓存
func SaveExtractRules(rules []model.ExtractRule) error {
	err := config.DB.Where("config_key LIKE ?", "cs_extract_rule_%").Delete(&model.PlatformConfig{}).Error
	if err != nil {
		return err
	}
	now := time.Now()
	for _, r := range rules {
		if r.Field == "" || r.Pattern == "" {
			continue
		}
		// 校验正则合法性，非法规则直接拒绝（避免注入非法表达式导致提取 panic）
		if _, err := regexp.Compile(r.Pattern); err != nil {
			return err
		}
		if err := config.DB.Create(&model.PlatformConfig{
			ConfigKey:   "cs_extract_rule_" + r.Field,
			ConfigValue: r.Pattern,
			Description: "客户信息提取规则-" + r.Field,
			CreatedAt:   now,
			UpdatedAt:   now,
		}).Error; err != nil {
			return err
		}
	}
	extractRulesCache.Store(rules)
	return nil
}

// ====================== 正则提取 ======================

// cleanPhone 清洗手机号：去空格/短横线，去掉 +86/86 前缀
func cleanPhone(s string) string {
	s = strings.NewReplacer(" ", "", "-", "", "\u00a0", "").Replace(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "+86")
	s = strings.TrimPrefix(s, "86")
	return s
}

// extractFromContent 按规则从消息内容提取字段值（返回字段->值，只取首个命中）
func extractFromContent(content string) map[string]string {
	out := map[string]string{}
	for _, r := range loadExtractRules() {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			continue // 非法规则跳过（保存时已校验，此处仅防御）
		}
		m := re.FindString(content)
		if m == "" {
			continue
		}
		if r.Field == "phone" {
			// RE2 不支持 lookbehind/lookahead：手动校验数字边界，
			// 防止从长数字串（文件 URL 的 unixNano 时间戳、订单号等）中截取 11 位误匹配
			if idx := re.FindStringIndex(content); idx != nil {
				prevDigit := idx[0] > 0 && content[idx[0]-1] >= '0' && content[idx[0]-1] <= '9'
				nextDigit := idx[1] < len(content) && content[idx[1]] >= '0' && content[idx[1]] <= '9'
				if prevDigit || nextDigit {
					continue
				}
			}
			m = cleanPhone(m)
		}
		if m != "" {
			out[r.Field] = m
		}
	}
	return out
}

// upsertExtractedProfile 将自动提取结果合并进客户档案（保留人工填写字段，不覆盖）
func upsertExtractedProfile(threadID, tenantID string, extracted map[string]string) error {
	if len(extracted) == 0 {
		return nil
	}
	// 读取现有档案：人工字段（name/address/remark）与既有 phone/email 保留合并
	var cp model.CustomerProfile
	notFound := config.DB.Where("thread_id = ?", threadID).First(&cp).Error
	now := time.Now()
	if notFound != nil {
		cp = model.CustomerProfile{
			ThreadID: threadID, TenantID: tenantID, CreatedAt: now,
			ExtractStatus: 1,
		}
	}
	for field, val := range extracted {
		switch field {
		case "phone":
			cp.Phone = val
		case "email":
			cp.Email = val
		}
	}
	if cp.ExtractStatus == 0 {
		cp.ExtractStatus = 1
	}
	cp.UpdatedAt = now
	if cp.ID > 0 {
		return config.DB.Model(&model.CustomerProfile{}).Where("id = ?", cp.ID).
			Updates(map[string]interface{}{
				"phone": cp.Phone, "email": cp.Email,
				"extract_status": cp.ExtractStatus, "updated_at": now,
			}).Error
	}
	return config.DB.Create(&cp).Error
}

// ExtractAndSaveCustomerInfo 消息落库时调用：仅对用户消息做正则提取并合并档案
func ExtractAndSaveCustomerInfo(threadID, role, content string) {
	if role != "user" || content == "" {
		return
	}
	extracted := extractFromContent(content)
	if len(extracted) == 0 {
		return
	}
	tenantID := ""
	if i := strings.Index(threadID, ":"); i > 0 {
		tenantID = threadID[:i]
	}
	_ = upsertExtractedProfile(threadID, tenantID, extracted)
}

// ReextractSession 手动对指定会话重跑提取（规则变更后批量补数据用）
func ReextractSession(threadID string) error {
	var msgs []model.Message
	if err := config.DB.Where("thread_id = ? AND role = ?", threadID, "user").
		Order("id ASC").Find(&msgs).Error; err != nil {
		return err
	}
	merged := map[string]string{}
	for _, m := range msgs {
		for k, v := range extractFromContent(m.Content) {
			if v != "" {
				merged[k] = v
			}
		}
	}
	if len(merged) == 0 {
		return nil
	}
	tenantID := ""
	if i := strings.Index(threadID, ":"); i > 0 {
		tenantID = threadID[:i]
	}
	return upsertExtractedProfile(threadID, tenantID, merged)
}

// ====================== 会话列表与客户档案查询 ======================

// SessionItem 会话列表项（messages 聚合 + 客户档案 LEFT JOIN）
type SessionItem struct {
	ThreadID       string    `json:"thread_id"`
	TenantID       string    `json:"tenant_id"`
	MessageCount   int64     `json:"message_count"`
	UserCount      int64     `json:"user_count"`
	UnreadCount    int64     `json:"unread_count"`
	LastContent    string    `json:"last_content"`
	LastActiveAt   time.Time `json:"last_active_at"`
	HasOpenTicket  bool      `json:"has_open_ticket"`
	Name           string    `json:"name"`
	Phone          string    `json:"phone"`
	Address        string    `json:"address"`
	Email          string    `json:"email"`
	Remark         string    `json:"remark"`
	ExtractStatus  int       `json:"extract_status"`
	Tags           string    `json:"tags"` // 会话标签，逗号分隔
	Summary        string    `json:"summary"` // 会话摘要（AI 自动生成，可能为空）
}

// SessionListOpt 会话列表筛选条件
type SessionListOpt struct {
	Keyword  string // thread_id / 客户档案（姓名/电话/邮箱/地址）模糊匹配
	DateFrom string // 最后活跃起始（YYYY-MM-DD）
	DateTo   string // 最后活跃截止（YYYY-MM-DD）
	Handoff  int    // 1=仅看已转人工；-1/0=全部
	Tag      string // 按会话标签筛选（LIKE 模糊）
}

// ListSessions 会话列表：按租户聚合 messages，LEFT JOIN 客户档案，按最后活跃倒序分页
func ListSessions(tenantID string, page, size int, opts ...SessionListOpt) ([]SessionItem, int64, error) {
	var o SessionListOpt
	if len(opts) > 0 {
		o = opts[0]
	}
	// 动态过滤条件（COUNT 与主查询共用）
	where := "m.tenant_id = ?"
	args := []interface{}{tenantID}
	if kw := strings.TrimSpace(o.Keyword); kw != "" {
		where += " AND (m.thread_id LIKE ? OR cp.name LIKE ? OR cp.phone LIKE ? OR cp.email LIKE ? OR cp.address LIKE ?)"
		for i := 0; i < 5; i++ {
			args = append(args, "%"+kw+"%")
		}
	}
	if o.DateFrom != "" {
		where += " AND m.created_at >= ?"
		args = append(args, o.DateFrom+" 00:00:00")
	}
	if o.DateTo != "" {
		where += " AND m.created_at < ?"
		args = append(args, o.DateTo+" 23:59:59")
	}
	handoffFilter := ""
	if o.Handoff == 1 {
		handoffFilter = " HAVING COALESCE(MAX(CASE WHEN t.status IN (0,1) THEN 1 ELSE 0 END), 0) = 1"
	}
	tagFilter := ""
	if tag := strings.TrimSpace(o.Tag); tag != "" {
		tagFilter = " AND m.thread_id IN (SELECT thread_id FROM session_tags WHERE tenant_id = ? AND tags LIKE ?)"
		args = append(args, tenantID, "%"+tag+"%")
	}
	where += tagFilter

	var total int64
	if err := config.DB.Table("messages m").
		Joins("LEFT JOIN customer_profiles cp ON cp.thread_id = m.thread_id").
		Where(where, args...).
		Distinct("m.thread_id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	// 该租户全部客户档案（一次查出，内存映射避免 N+1）
	profileByThread := map[string]model.CustomerProfile{}
	var profiles []model.CustomerProfile
	if err := config.DB.Where("tenant_id = ?", tenantID).Find(&profiles).Error; err != nil {
		return nil, 0, err
	}
	for _, p := range profiles {
		profileByThread[p.ThreadID] = p
	}

	var rows []SessionItem
	offset := (page - 1) * size
	err := config.DB.Raw(`
		SELECT m.thread_id,
		       MAX(m.tenant_id) AS tenant_id,
		       COUNT(*) AS message_count,
		       COALESCE(SUM(CASE WHEN m.role = 'user' THEN 1 ELSE 0 END), 0) AS user_count,
		       COALESCE(SUM(CASE WHEN m.role != 'user' AND m.is_read = 0 THEN 1 ELSE 0 END), 0) AS unread_count,
		       MAX(m.created_at) AS last_active_at,
		       COALESCE(MAX(CASE WHEN t.status IN (0,1) THEN 1 ELSE 0 END), 0) AS has_open_ticket,
		       MAX(ss.summary) AS summary
		FROM messages m
		LEFT JOIN tickets t ON t.thread_id = m.thread_id AND t.status IN (0,1)
		LEFT JOIN customer_profiles cp ON cp.thread_id = m.thread_id
		LEFT JOIN session_summaries ss ON ss.thread_id = m.thread_id AND ss.tenant_id = m.tenant_id
		WHERE `+where+`
		GROUP BY m.thread_id`+handoffFilter+`
		ORDER BY last_active_at DESC
		LIMIT ? OFFSET ?`, append(args, size, offset)...).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	// 补最后一条消息内容（按会话逐条取，分页量小可接受）
	tagByThread := map[string]string{}
	var sts []model.SessionTag
	if err := config.DB.Where("tenant_id = ?", tenantID).Find(&sts).Error; err == nil {
		for _, st := range sts {
			tagByThread[st.ThreadID] = st.Tags
		}
	}
	items := make([]SessionItem, 0, len(rows))
	for i := range rows {
		var last model.Message
		if err := config.DB.Where("thread_id = ?", rows[i].ThreadID).
			Order("id DESC").First(&last).Error; err == nil {
			rows[i].LastContent = last.Content
		}
		rows[i].Tags = tagByThread[rows[i].ThreadID]
		if p, ok := profileByThread[rows[i].ThreadID]; ok {
			rows[i].Name = p.Name
			rows[i].Phone = p.Phone
			rows[i].Address = p.Address
			rows[i].Email = p.Email
			rows[i].Remark = p.Remark
			rows[i].ExtractStatus = p.ExtractStatus
		}
		items = append(items, rows[i])
	}
	return items, total, nil
}

// GetSessionTags 查询会话标签（无记录返回空串）
func GetSessionTags(tenantID, threadID string) string {
	var st model.SessionTag
	if err := config.DB.Where("tenant_id = ? AND thread_id = ?", tenantID, threadID).First(&st).Error; err != nil {
		return ""
	}
	return st.Tags
}

// SetSessionTags 设置会话标签（upsert，按租户+会话唯一）
func SetSessionTags(tenantID, threadID, tags string) error {
	tags = strings.TrimSpace(tags)
	var st model.SessionTag
	if err := config.DB.Where("tenant_id = ? AND thread_id = ?", tenantID, threadID).First(&st).Error; err == nil {
		return config.DB.Model(&st).Update("tags", tags).Error
	}
	st = model.SessionTag{TenantID: tenantID, ThreadID: threadID, Tags: tags}
	return config.DB.Create(&st).Error
}

// GetCustomerProfile 查询会话客户档案
func GetCustomerProfile(threadID string) (*model.CustomerProfile, error) {
	var cp model.CustomerProfile
	err := config.DB.Where("thread_id = ?", threadID).First(&cp).Error
	if err != nil {
		return nil, err
	}
	return &cp, nil
}

// SaveCustomerProfile 人工填写/更新客户档案（upsert：thread_id 唯一）
func SaveCustomerProfile(cp *model.CustomerProfile) error {
	now := time.Now()
	cp.UpdatedAt = now
	if cp.ExtractStatus == 0 {
		cp.ExtractStatus = 2 // 人工填写标记
	}
	return config.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "thread_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "phone", "address", "email", "remark", "extract_status", "updated_at"}),
	}).Create(cp).Error
}

// ====================== 客户列表（按人聚合，供后台客户名册） ======================

// CustomerItem 客户列表条目：档案字段 + 会话活跃度（来自 messages 聚合，可为空）
type CustomerItem struct {
	ThreadID      string     `json:"thread_id"`
	TenantID      string     `json:"tenant_id"`
	Name          string     `json:"name"`
	Phone         string     `json:"phone"`
	Address       string     `json:"address"`
	Email         string     `json:"email"`
	Remark        string     `json:"remark"`
	ExtractStatus int        `json:"extract_status"`
	LastActiveAt  *time.Time `json:"last_active_at"`
	MessageCount  int64      `json:"message_count"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// ListCustomers 客户列表：customer_profiles LEFT JOIN messages 活跃度聚合，
// 支持按 姓名/电话/邮箱/地址/备注 模糊搜索；会话全归档也不影响本表（独立数据源）。
// 归档后消息聚合可能为空 → last_active_at 取档案 updated_at 兜底。
func ListCustomers(tenantID string, page, size int, q string) ([]CustomerItem, int64, error) {
	where := "p.tenant_id = ?"
	args := []interface{}{tenantID}
	if q != "" {
		like := "%" + q + "%"
		where += " AND (p.name LIKE ? OR p.phone LIKE ? OR p.email LIKE ? OR p.address LIKE ? OR p.remark LIKE ?)"
		args = append(args, like, like, like, like, like)
	}
	var total int64
	if err := config.DB.Table("customer_profiles p").
		Where(where, args...).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * size
	// 子查询需要两个 tenant_id（messages 与 messages_archive），再跟 where 参数和 limit/offset
	orderArgs := append([]interface{}{tenantID, tenantID}, args...)
	orderArgs = append(orderArgs, size, offset)
	var items []CustomerItem
	err := config.DB.Raw(`
		SELECT p.thread_id, p.tenant_id, p.name, p.phone, p.address, p.email, p.remark,
		       p.extract_status, p.created_at, p.updated_at,
		       m.last_active_at, m.msg_count
		FROM customer_profiles p
		LEFT JOIN (
			SELECT thread_id, MAX(created_at) AS last_active_at, COUNT(*) AS msg_count
			FROM (
				SELECT thread_id, created_at FROM messages WHERE tenant_id = ?
				UNION ALL
				SELECT thread_id, created_at FROM messages_archive WHERE tenant_id = ?
			) u
			GROUP BY thread_id
		) m ON m.thread_id = p.thread_id
		WHERE `+where+`
		ORDER BY COALESCE(m.last_active_at, p.updated_at) DESC
		LIMIT ? OFFSET ?`,
		orderArgs...).
		Scan(&items).Error
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
