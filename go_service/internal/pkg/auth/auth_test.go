package auth

import (
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"

	"customer_service/internal/pkg/crypto"
)

// 测试用租户表（避免 DB 依赖）
func setupTestTenants(t *testing.T) {
	t.Helper()
	authEnabled = true
	authTTL = time.Hour
	tenantByKey = map[string]RuntimeTenant{
		"t1_key": {TenantID: "t_alpha", Name: "Alpha", AppKey: "t1_key", AppSecretHash: crypto.HashSecret("secret_alpha"), Status: 1},
		"t2_key": {TenantID: "t_beta", Name: "Beta", AppKey: "t2_key", AppSecretHash: crypto.HashSecret("secret_beta"), Status: 1},
		"t3_key": {TenantID: "t_disabled", Name: "Disabled", AppKey: "t3_key", AppSecretHash: crypto.HashSecret("secret_disabled"), Status: 0},
	}
	TenantsList = []RuntimeTenant{}
	for _, v := range tenantByKey {
		TenantsList = append(TenantsList, v)
	}
	tokenBlacklist = map[string]int64{}
}

// ============ IssueToken / VerifyToken ============

func TestIssueAndVerifyToken(t *testing.T) {
	setupTestTenants(t)
	tok, tid, exp, err := IssueToken("t1_key", "secret_alpha")
	if err != nil {
		t.Fatalf("IssueToken 失败: %v", err)
	}
	if tid != "t_alpha" {
		t.Errorf("tenant_id 应为 t_alpha, 实际 %s", tid)
	}
	if exp <= 0 {
		t.Errorf("expires_in 应为正数, 实际 %d", exp)
	}
	gotTid, err := VerifyToken(tok)
	if err != nil {
		t.Fatalf("VerifyToken 失败: %v", err)
	}
	if gotTid != "t_alpha" {
		t.Errorf("VerifyToken 返回租户 %s, 期望 t_alpha", gotTid)
	}
}

func TestIssueTokenRejectsBadSecret(t *testing.T) {
	setupTestTenants(t)
	if _, _, _, err := IssueToken("t1_key", "wrong_secret"); err == nil {
		t.Error("错误 secret 应返回 error")
	}
}

func TestIssueTokenRejectsUnknownKey(t *testing.T) {
	setupTestTenants(t)
	if _, _, _, err := IssueToken("nope_key", "whatever"); err == nil {
		t.Error("未知 app_key 应返回 error")
	}
}

func TestIssueTokenRejectsDisabledTenant(t *testing.T) {
	setupTestTenants(t)
	if _, _, _, err := IssueToken("t3_key", "secret_disabled"); err == nil {
		t.Error("禁用租户应返回 error")
	}
}

func TestVerifyTokenRejectsTampered(t *testing.T) {
	setupTestTenants(t)
	tok, _, _, err := IssueToken("t1_key", "secret_alpha")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	tampered := parts[0] + "." + "A" + parts[1][1:]
	if _, err := VerifyToken(tampered); err == nil {
		t.Error("篡改 token 应校验失败")
	}
}

func TestVerifyTokenRejectsGarbage(t *testing.T) {
	setupTestTenants(t)
	if _, err := VerifyToken("not.a.token"); err == nil {
		t.Error("垃圾 token 应校验失败")
	}
	if _, err := VerifyToken(""); err == nil {
		t.Error("空 token 应校验失败")
	}
}

func TestVerifyTokenExpired(t *testing.T) {
	setupTestTenants(t)
	pl := tokenPayload{TenantID: "t_alpha", Exp: time.Now().Add(-time.Minute).Unix(), Nonce: "n"}
	raw := `{"tenant_id":"t_alpha","exp":` + strconv.FormatInt(pl.Exp, 10) + `,"nonce":"n"}`
	expired := base64.RawURLEncoding.EncodeToString([]byte(raw)) + "." + signPayload(crypto.HashSecret("secret_alpha"), []byte(raw))
	if _, err := VerifyToken(expired); err == nil {
		t.Error("过期 token 应校验失败")
	}
}

func TestCrossTenantTokenInvalid(t *testing.T) {
	setupTestTenants(t)
	raw := `{"tenant_id":"t_alpha","exp":` + strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10) + `,"nonce":"n"}`
	sig := signPayload(crypto.HashSecret("secret_beta"), []byte(raw)) // 用错误租户签名
	tok := base64.RawURLEncoding.EncodeToString([]byte(raw)) + "." + sig
	if _, err := VerifyToken(tok); err == nil {
		t.Error("跨租户伪造 token 应校验失败")
	}
}

// ============ 黑名单 ============

func TestBlacklistAndVerify(t *testing.T) {
	setupTestTenants(t)
	tok, _, _, err := IssueToken("t1_key", "secret_alpha")
	if err != nil {
		t.Fatal(err)
	}
	BlacklistToken(tok)
	if !IsTokenBlacklisted(tok) {
		t.Error("黑名单中的 token 应返回 true")
	}
	if _, err := VerifyToken(tok); err == nil {
		t.Error("黑名单 token 校验应失败")
	}
}

func TestBlacklistExpiredEntryCleaned(t *testing.T) {
	setupTestTenants(t)
	tokenBlacklist["dummy"] = time.Now().Add(-time.Minute).Unix()
	if IsTokenBlacklisted("dummy") {
		t.Error("过期黑名单条目应自动清理并返回 false")
	}
	if _, ok := tokenBlacklist["dummy"]; ok {
		t.Error("过期黑名单条目应从 map 删除")
	}
}

// ============ RefreshToken ============

func TestRefreshToken(t *testing.T) {
	setupTestTenants(t)
	tok, _, _, err := IssueToken("t1_key", "secret_alpha")
	if err != nil {
		t.Fatal(err)
	}
	newTok, tid, _, err := RefreshToken(tok)
	if err != nil {
		t.Fatalf("RefreshToken 失败: %v", err)
	}
	if tid != "t_alpha" {
		t.Errorf("刷新后租户 %s, 期望 t_alpha", tid)
	}
	if _, err := VerifyToken(newTok); err != nil {
		t.Errorf("新 token 校验失败: %v", err)
	}
	if _, err := VerifyToken(tok); err == nil {
		t.Error("刷新后旧 token 应失效")
	}
}

// ============ 租户隔离（纯函数） ============

func TestUserIDBelongsToTenant(t *testing.T) {
	setupTestTenants(t)
	cases := []struct {
		tenantID, userID string
		want             bool
	}{
		{"t_alpha", "t_alpha:u1", true},
		{"t_alpha", "t_beta:u1", false},
		{"t_alpha", "t_alpha1:u1", false}, // 前缀撞车（t_alpha1 非 t_alpha）
		{"t_alpha", "", false},
		{"", "t_alpha:u1", false},
		{"t_beta", "t_beta:abc", true},
	}
	for _, c := range cases {
		if got := UserIDBelongsToTenant(c.tenantID, c.userID); got != c.want {
			t.Errorf("UserIDBelongsToTenant(%q,%q)=%v, want %v", c.tenantID, c.userID, got, c.want)
		}
	}
}

func TestTenantFromUserID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"t_demo:user123", "t_demo"},
		{"t_demo:user:with:colon", "t_demo"},
		{"nocolon", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := TenantFromUserID(c.in); got != c.want {
			t.Errorf("TenantFromUserID(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

// ============ 登录防爆破（P0-2） ============

func TestLoginLimiterLocksAfterMaxFails(t *testing.T) {
	limiter := &LoginLimiter{
		records:  make(map[string]*loginRecord),
		maxFails: 3,
		window:   time.Minute,
		lockTime: time.Minute,
	}
	for i := 0; i < 3; i++ {
		limiter.Fail("ip1:key1")
	}
	if ok, _ := limiter.Allow("ip1:key1"); ok {
		t.Error("达到失败阈值后应锁定")
	}
	if ok2, _ := limiter.Allow("ip1:key2"); !ok2 {
		t.Error("其他 key 不应被锁定")
	}
}

func TestLoginLimiterSuccessClears(t *testing.T) {
	limiter := &LoginLimiter{
		records:  make(map[string]*loginRecord),
		maxFails: 3,
		window:   time.Minute,
		lockTime: time.Minute,
	}
	limiter.Fail("ip:key")
	limiter.Fail("ip:key")
	limiter.Success("ip:key")
	if ok, _ := limiter.Allow("ip:key"); !ok {
		t.Error("成功后应清除失败记录")
	}
}

func TestLoginLimiterWindowReset(t *testing.T) {
	limiter := &LoginLimiter{
		records:  make(map[string]*loginRecord),
		maxFails: 3,
		window:   time.Minute,
		lockTime: time.Minute,
	}
	limiter.Fail("ip:key")
	limiter.records["ip:key"] = &loginRecord{fails: 2, windowStart: time.Now().Add(-2 * time.Minute)}
	if ok, _ := limiter.Allow("ip:key"); !ok {
		t.Error("窗口过期后应重置允许")
	}
}
