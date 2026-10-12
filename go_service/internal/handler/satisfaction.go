package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"customer_service/internal/config"
	"customer_service/internal/middleware"
	"customer_service/internal/model"

	"github.com/gin-gonic/gin"
)

// UserSubmitRating 用户端提交/覆盖满意度评价：POST /api/user/rating
// body: {user_id(=thread_id), score 1-5, tags?, comment?}
func UserSubmitRating(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未获取到租户信息"})
		return
	}
	var b struct {
		UserID  string `json:"user_id"`
		Score   int    `json:"score"`
		Tags    string `json:"tags"`
		Comment string `json:"comment"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	if b.UserID == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "缺少 user_id"})
		return
	}
	if b.Score < 1 || b.Score > 5 {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "评分需为 1-5 星"})
		return
	}
	// 同租户同会话唯一，重复提交覆盖（保留原创建时间语义：更新 score/tags/comment）
	var r model.SatisfactionRating
	err := config.DB.Where("tenant_id = ? AND thread_id = ?", tid, b.UserID).First(&r).Error
	if err != nil {
		r = model.SatisfactionRating{
			TenantID: tid,
			ThreadID: b.UserID,
			UserKey:  b.UserID,
			Score:    b.Score,
			Tags:     strings.TrimSpace(b.Tags),
			Comment:  strings.TrimSpace(b.Comment),
		}
		if len(r.Comment) > 500 {
			r.Comment = r.Comment[:500]
		}
		if len(r.Tags) > 255 {
			r.Tags = r.Tags[:255]
		}
		if err := config.DB.Create(&r).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "提交评价失败"})
			return
		}
	} else {
		fields := map[string]interface{}{
			"score":      b.Score,
			"tags":       strings.TrimSpace(b.Tags),
			"comment":    strings.TrimSpace(b.Comment),
			"updated_at": time.Now(),
		}
		if err := config.DB.Model(&r).Updates(fields).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "提交评价失败"})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "评价成功"})
}

// AdminRatingList 满意度评价列表：GET /api/admin/ratings
// 参数：score、q（会话ID/标签/留言关键词）、start/end（日期）、page、page_size
func AdminRatingList(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未获取到租户信息"})
		return
	}
	where := "tenant_id = ?"
	args := []interface{}{tid}
	if s := strings.TrimSpace(c.Query("score")); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v >= 1 && v <= 5 {
			where += " AND score = ?"
			args = append(args, v)
		}
	}
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		where += " AND (thread_id LIKE ? OR tags LIKE ? OR comment LIKE ?)"
		args = append(args, "%"+q+"%", "%"+q+"%", "%"+q+"%")
	}
	if start := strings.TrimSpace(c.Query("start")); start != "" {
		where += " AND created_at >= ?"
		args = append(args, start)
	}
	if end := strings.TrimSpace(c.Query("end")); end != "" {
		where += " AND created_at <= ?"
		args = append(args, end+" 23:59:59")
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 10
	}
	var total int64
	config.DB.Model(&model.SatisfactionRating{}).Where(where, args...).Count(&total)
	var list []model.SatisfactionRating
	if err := config.DB.Where(where, args...).Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询评价失败"})
		return
	}
	if list == nil {
		list = []model.SatisfactionRating{}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"list": list, "total": total}})
}

// AdminRatingStats 满意度评价统计：GET /api/admin/ratings/stats
// 返回：总数/平均分/1-5星分布/满意率(>=4)/近7天趋势
func AdminRatingStats(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未获取到租户信息"})
		return
	}
	type cnt struct {
		Score int `json:"score"`
		N     int `json:"n"`
	}
	var dist []cnt
	if err := config.DB.Raw("SELECT score, COUNT(*) AS n FROM satisfaction_ratings WHERE tenant_id = ? GROUP BY score", tid).Scan(&dist).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "统计失败"})
		return
	}
	total := 0
	sum := 0
	distMap := map[int]int{1: 0, 2: 0, 3: 0, 4: 0, 5: 0}
	for _, d := range dist {
		total += d.N
		sum += d.Score * d.N
		if _, ok := distMap[d.Score]; ok {
			distMap[d.Score] = d.N
		}
	}
	avg := 0.0
	sat4 := 0
	satisfyRate := 0.0
	if total > 0 {
		avg = float64(sum) / float64(total)
		sat4 = distMap[4] + distMap[5]
		satisfyRate = float64(sat4) / float64(total) * 100
	}
	// 近7天（含今天）
	now := time.Now()
	days := make([]gin.H, 0, 7)
	for i := 6; i >= 0; i-- {
		d := now.AddDate(0, 0, -i)
		dayStart := d.Format("2006-01-02")
		var n int
		var s int
		config.DB.Raw("SELECT COUNT(*), COALESCE(SUM(score),0) FROM satisfaction_ratings WHERE tenant_id = ? AND created_at >= ? AND created_at < ?",
			tid, dayStart, d.AddDate(0, 0, 1).Format("2006-01-02")).Row().Scan(&n, &s)
		dv := 0.0
		if n > 0 {
			dv = float64(s) / float64(n)
		}
		days = append(days, gin.H{"date": dayStart, "count": n, "avg": dv})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"total":       total,
		"avg_score":   avg,
		"dist":        distMap,
		"satisfied":   sat4,        // 4-5 星数量
		"satisfy_rate":  satisfyRate, // 满意率（4-5 星占比）
		"last7days":   days,
	}})
}
