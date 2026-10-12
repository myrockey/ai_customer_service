package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"customer_service/internal/middleware"
)

// ============ 会话图文：图片/视频上传 ============
// P0：人工客服在工单/会话中发送图片消息（AdminSessionImageUpload）
// P1：用户端发送图片（UserSessionImageUpload）
// P1.5：支持视频（图片 5MB / 视频 50MB，格式 mp4/webm，参考美洽/七鱼行业规范）
// 经本接口上传后返回静态 URL，以 msg_type=image/video 消息发送，
// nginx 静态服务 /msg_files/（cs_msg_files 卷）。

const (
	MsgFileMaxMB = 5 // 图片最大 5MB（与七鱼/美洽一致）

	// 视频最大 50MB（美洽视频消息同规格）；格式 mp4/webm（浏览器原生播放）
	MsgVideoMaxMB   = 50
	msgFileRoot     = "/app/msg_files"
	msgImageMaxSize = MsgFileMaxMB * 1024 * 1024
	msgVideoMaxSize = MsgVideoMaxMB * 1024 * 1024
)

var msgImageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
}
var msgVideoExts = map[string]bool{
	".mp4": true, ".webm": true,
}

// mediaKind 返回文件类型：image / video / ""（不支持）
func mediaKind(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if msgImageExts[ext] {
		return "image"
	}
	if msgVideoExts[ext] {
		return "video"
	}
	return ""
}

// saveSessionMedia 通用保存逻辑：按类型校验大小/格式 → 按租户+月份建目录 → 落盘 → 返回 (相对URL, 类型)
// 安全：后缀白名单 + 大小限制 + 随机文件名（防路径穿越/覆盖）；不信任客户端文件名
func saveSessionMedia(c *gin.Context, tid string) (string, string, error) {
	file, err := c.FormFile("file")
	if err != nil {
		return "", "", fmt.Errorf("请选择要上传的文件")
	}
	kind := mediaKind(file.Filename)
	if kind == "" {
		return "", "", fmt.Errorf("仅支持图片(jpg/png/gif/webp)或视频(mp4/webm)")
	}
	if kind == "image" && file.Size > msgImageMaxSize {
		return "", "", fmt.Errorf("图片大小不能超过 %dMB", MsgFileMaxMB)
	}
	if kind == "video" && file.Size > msgVideoMaxSize {
		return "", "", fmt.Errorf("视频大小不能超过 %dMB", MsgVideoMaxMB)
	}
	// 目录：{root}/{tenant}/{yyyyMM}，按租户隔离
	month := time.Now().Format("200601")
	dir := filepath.Join(msgFileRoot, tid, month)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("创建存储目录失败")
	}
	// 容器 umask 可能为 0027：显式 chmod 0755，保证 nginx 用户（非属主）可读目录
	if err := os.Chmod(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("设置目录权限失败")
	}
	name := fmt.Sprintf("%d%s", time.Now().UnixNano(), strings.ToLower(filepath.Ext(file.Filename)))
	dst := filepath.Join(dir, name)
	if err := c.SaveUploadedFile(file, dst); err != nil {
		return "", "", fmt.Errorf("文件保存失败:%v", err)
	}
	// gin SaveUploadedFile 内部会 MkdirAll(父目录, 0750)：若目录恰在此刻重建会回到 750，
	// nginx 用户无法进入目录 → 静态 403。保存后再强制一次 0755 兜底。
	if err := os.Chmod(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("设置目录权限失败")
	}
	url := fmt.Sprintf("/msg_files/%s/%s/%s", tid, month, name)
	return url, kind, nil
}

// AdminSessionMediaUpload 客服端图片/视频上传：POST /api/admin/sessions/upload（权限 ticket:handle）
func AdminSessionMediaUpload(c *gin.Context) {
	tid := middleware.TenantOf(c)
	url, kind, err := saveSessionMedia(c, tid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"url": url, "msg_type": kind}})
}

// UserSessionMediaUpload 用户端图片/视频上传：POST /api/user/upload（UserAuth，用户 token）
// 与客服端同一存储与静态服务；文件按租户隔离，无法越权访问他人租户文件
func UserSessionMediaUpload(c *gin.Context) {
	tid := middleware.TenantOf(c)
	url, kind, err := saveSessionMedia(c, tid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"url": url, "msg_type": kind}})
}
