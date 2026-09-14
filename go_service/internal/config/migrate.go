package config

import (
	"fmt"
	"os"
	"strings"

	"customer_service/internal/model"
	"customer_service/internal/pkg/crypto"
)

// seedTenants 种子租户初始化（幂等）：
//   - tenants 表为空时，将 config.yaml auth.tenants（含平台管理员 t_admin）写入，
//     之后以 DB 为准（后台可增删租户），不会覆盖已有数据。
//   - app_secret 安全策略：支持环境变量覆盖（TENANT_{TENANT_ID大写}_SECRET，如 TENANT_T_ADMIN_SECRET），
//     config.yaml 仅放占位值，生产环境通过 .env / 环境变量注入真实 secret，避免明文进 git。
//   - 表结构由 InitDB 的 AutoMigrate 负责（与 deploy/production/init/mysql/01_schema.sql 对齐），
//     新项目无需任何历史迁移逻辑。
func seedTenants() error {
	var cnt int64
	if err := DB.Model(&model.TenantRow{}).Count(&cnt).Error; err != nil {
		return err
	}
	if cnt > 0 {
		return nil
	}
	for _, t := range GlobalConfig.Auth.Tenants {
		if t.TenantID == "" || t.AppKey == "" || t.AppSecret == "" {
			continue
		}
		secret := t.AppSecret
		if env := os.Getenv("TENANT_" + strings.ToUpper(t.TenantID) + "_SECRET"); env != "" {
			secret = env
		}
		if secret == "" {
			continue
		}
		row := model.TenantRow{
			TenantID:        t.TenantID,
			Name:            t.Name,
			AppKey:          t.AppKey,
			AppSecretHash:   crypto.HashSecret(secret),
			IsPlatformAdmin: t.IsPlatformAdmin,
			Status:          1,
		}
		if err := DB.Create(&row).Error; err != nil {
			return fmt.Errorf("seed tenant %s: %w", t.TenantID, err)
		}
	}
	return nil
}
