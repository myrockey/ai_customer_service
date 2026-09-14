// thirdDemo — 智能客服系统第三方接入最简示例
//
// 本程序模拟一个第三方网站（如电商官网），演示如何接入智能客服系统：
//  1. 服务端用 app_key/app_secret 换取 access_token（带缓存）
//  2. 前端页面嵌入客服浮窗（iframe 方式）
//  3. 前端通过 /get-token 接口从服务端获取 token（不接触 app_secret）
//
// 运行：go run main.go
// 访问：http://localhost:8090
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// ============ 配置（实际部署时用环境变量） ============
const (
	// 客服系统地址（部署后改为你的域名，如 https://cs.yourcompany.com）
	CSBaseURL = "http://localhost:8080"
	// 第三方租户的 app_key（从客服管理后台创建租户后获取）
	AppKey = "demo_key"
	// 第三方租户的 app_secret（仅此一次显示，务必妥善保存）
	// 生产环境必须从环境变量读取：os.Getenv("CS_APP_SECRET")
	AppSecret = "demo_secret"
	// 本示例服务监听端口
	ListenPort = ":8090"
)

// ============ Token 缓存（内存 + 互斥锁） ============
var (
	tokenCache  string
	tenantCache string
	tokenExpiry time.Time
	tokenMutex  sync.Mutex
)

// tokenResp 换 token 接口响应
type tokenResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
		TenantID  string `json:"tenant_id"`
	} `json:"data"`
}

// getToken 获取客服系统 access_token（带缓存，提前30分钟刷新）
func getToken() (string, error) {
	tokenMutex.Lock()
	defer tokenMutex.Unlock()

	// 缓存有效且距过期超过30分钟，直接返回
	if tokenCache != "" && time.Now().Before(tokenExpiry.Add(-30*time.Minute)) {
		return tokenCache, nil
	}

	// 调用客服系统换 token
	body, _ := json.Marshal(map[string]string{
		"app_key":    AppKey,
		"app_secret": AppSecret,
	})
	resp, err := http.Post(CSBaseURL+"/api/auth/token", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("请求客服系统失败: %w", err)
	}
	defer resp.Body.Close()

	var result tokenResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}
	if result.Code != 0 {
		return "", fmt.Errorf("换token失败: %s", result.Msg)
	}

	// 更新缓存
	tokenCache = result.Data.Token
	tenantCache = result.Data.TenantID
	tokenExpiry = time.Now().Add(time.Duration(result.Data.ExpiresIn) * time.Second)
	log.Printf("[token] 换取新token成功, 租户=%s, 有效期=%d秒", result.Data.TenantID, result.Data.ExpiresIn)
	return tokenCache, nil
}

// ============ HTTP 处理函数 ============

// tokenHandler 给前端返回 token（前端不直接接触 app_secret）
func tokenHandler(w http.ResponseWriter, r *http.Request) {
	token, err := getToken()
	if err != nil {
		log.Printf("[token] 换取失败: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// 允许前端跨域调用（生产环境应限制具体域名）
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]string{
		"token":     token,
		"tenant_id": tenantCache,
	})
}

// indexHandler 托管首页
func indexHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func main() {
	// 路由
	http.HandleFunc("/get-token", tokenHandler)
	http.HandleFunc("/", indexHandler)

	// 启动时预取一次 token
	if _, err := getToken(); err != nil {
		log.Printf("[warn] 启动时预取token失败: %v (前端请求时会自动重试)", err)
	}

	log.Println("========================================")
	log.Println("  智能客服第三方接入示例（SDK 方式）")
	log.Println("========================================")
	log.Printf("  本服务地址:  http://localhost%s", ListenPort)
	log.Printf("  客服系统:    %s", CSBaseURL)
	log.Printf("  租户app_key: %s", AppKey)
	log.Println("========================================")
	log.Println("  访问 http://localhost:8090 查看效果")
	log.Println("  右下角 💬客服 按钮由 SDK 自动渲染")
	log.Println("  点击按钮打开客服对话窗口")
	log.Println("========================================")

	log.Fatal(http.ListenAndServe(ListenPort, nil))
}
