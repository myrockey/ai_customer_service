# 智能客服第三方接入示例（Go + SDK 方式）

本目录是一个**最简第三方接入示例**，模拟一个电商网站如何通过 **SDK（cs-chat.js）** 接入智能客服系统。

## 文件说明

| 文件 | 说明 |
|------|------|
| `main.go` | 第三方服务端（Go）：换 token 接口 + 页面托管 |
| `index.html` | 第三方网站页面：模拟电商首页 + SDK 接入 + API 演示 |
| `go.mod` | Go 模块定义（仅用标准库，无外部依赖） |

## 接入原理（SDK 方式）

```
用户浏览器                          第三方服务端(本程序)           智能客服系统
    │                                    │                          │
    │  1. 打开页面                       │                          │
    │───────────────────────────────────►│                          │
    │                                    │                          │
    │  2. 页面加载，引入 SDK             │                          │
    │  <script src="/sdk/cs-chat.js">   │                          │
    │                                    │                          │
    │  3. 调用 /get-token 获取 token     │                          │
    │───────────────────────────────────►│                          │
    │                                    │  4. POST /api/auth/token │
    │                                    │  (app_key + app_secret)  │
    │                                    │──────────────────────────►│
    │                                    │                          │
    │                                    │  5. 返回 access_token    │
    │                                    │◄──────────────────────────│
    │  6. 返回 token                    │                          │
    │◄───────────────────────────────────│                          │
    │                                    │                          │
    │  7. CSCHAT.init({token})          │                          │
    │     SDK 自动渲染浮窗按钮           │                          │
    │                                    │                          │
    │  8. 用户点击浮窗按钮               │                          │
    │     SDK 弹出弹窗，iframe 加载      │                          │
    │     /chat/pc.html?token=xxx        │                          │
    │──────────────────────────────────────────────────────────────►│
    │                                    │                          │
    │  9. pc.html 内部 WebSocket 连接    │                          │
    │     /ws/chat，实时收发消息         │                          │
    │◄──────────────────────────────────────────────────────────────►│
    │                                    │                          │
```

**核心安全原则**：`app_secret` 只存在于第三方服务端（`main.go`），前端只持有短期有效的 `access_token`。

**SDK 架构**：SDK 负责外壳（浮窗按钮 + 弹窗容器 + JS API + 事件监听），聊天内核用 iframe 加载 `pc.html`（WebSocket + 消息渲染），两者共用同一套代码。

## 快速开始

### 前置条件

1. 智能客服系统已启动（Go + Agent + MySQL + PG + Qdrant + Nginx）
2. 已在客服管理后台创建租户，获取 `app_key` 和 `app_secret`
3. 已安装 Go 1.21+

### 1. 修改配置

编辑 `main.go` 顶部的配置常量：

```go
const (
    CSBaseURL  = "http://localhost:8080"  // 客服系统地址，生产改为你的域名
    AppKey     = "demo_key"                 // 你的租户 app_key
    AppSecret  = "demo_secret"              // 你的租户 app_secret（生产用环境变量）
    ListenPort = ":8090"                    // 本示例服务端口
)
```

> ⚠️ 生产环境必须从环境变量读取 `app_secret`，不要硬编码在代码中：
> ```go
> AppSecret = os.Getenv("CS_APP_SECRET")
> ```

### 2. 运行

```bash
cd thirdDemo
go run main.go
```

输出：

```
========================================
  智能客服第三方接入示例（SDK 方式）
========================================
  本服务地址:  http://localhost:8090
  客服系统:    http://localhost:8080
  租户app_key: demo_key
========================================
  访问 http://localhost:8090 查看效果
  右下角 💬客服 按钮由 SDK 自动渲染
  点击按钮打开客服对话窗口
========================================
```

### 3. 测试

1. 浏览器访问 `http://localhost:8090`
2. 看到模拟电商首页（商品列表 + 接入说明 + SDK API 演示区）
3. 右下角 **💬客服** 按钮由 SDK 自动渲染
4. 点击按钮，SDK 弹出客服对话窗口，AI 客服自动应答
5. 在 API 演示区测试 `open/close/sendText` 等 API
6. 查看事件日志，观察 `ready/open/close/message/handoff` 等事件
7. 发送"转人工"可测试人工接管流程
8. 刷新页面后再次打开客服窗口，历史对话保留（user_id 持久化）

## 关键代码解析

### 服务端换 token（main.go）

```go
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
    // ... 解析响应，更新缓存
    return tokenCache, nil
}
```

**要点**：
- token 有效期 24 小时，内存缓存避免每次请求都换
- 提前 30 分钟刷新，避免用户遇到过期
- 互斥锁防止并发换 token

### 前端引入 SDK（index.html）

```html
<!-- 引入 SDK（data-manual=true 表示手动初始化） -->
<script src="http://localhost:8080/sdk/cs-chat.js" data-manual="true"></script>

<script>
// 从服务端获取 token（不接触 app_secret）
async function initCustomerService() {
    const resp = await fetch('/get-token');
    const data = await resp.json();

    // 初始化 SDK（自动渲染浮窗按钮）
    CSCHAT.init({
        server: 'http://localhost:8080',
        token: data.token,           // 服务端换取的 token
        tenant_id: data.tenant_id,   // 租户ID
        title: 'XX商城客服',
        theme: '#ee5a24',
        position: 'right',
        width: 380,
        height: 560,
        button_icon: '💬',
        button_text: '客服'
    });
}
initCustomerService();
</script>
```

### SDK 事件监听

```javascript
// 监听 SDK 事件
CSCHAT.on('ready', function(data) {
    console.log('客服就绪，user_id:', data.user_id);
});
CSCHAT.on('message', function(data) {
    console.log('收到消息:', data.role, data.content);
});
CSCHAT.on('handoff', function(data) {
    console.log('转人工成功，工单号:', data.ticket_no);
});
CSCHAT.on('open', function() { console.log('客服窗口打开'); });
CSCHAT.on('close', function() { console.log('客服窗口关闭'); });
```

### SDK API 调用

```javascript
// 打开客服窗口
CSCHAT.open();

// 关闭客服窗口
CSCHAT.close();

// 切换开关
CSCHAT.toggle();

// 主动发送消息（会自动打开窗口）
CSCHAT.sendText('你好，我想咨询一下订单');

// 设置/更新 token（token 过期时调用）
CSCHAT.setToken(newToken, tenantId);

// 获取当前 user_id
CSCHAT.getUserId();

// 是否打开
CSCHAT.isOpen();

// 销毁 SDK
CSCHAT.destroy();
```

## SDK vs 直接 iframe 对比

| 维度 | SDK (cs-chat.js) | 直接 iframe 嵌入 pc.html |
|------|-------------------|------------------------|
| 浮窗按钮 | ✅ 内置，可定制 | ❌ 需自己实现 |
| 弹窗动画 | ✅ 内置 | ❌ 需自己实现 |
| JS API | ✅ open/close/sendText | ❌ 需 postMessage |
| 事件监听 | ✅ message/handoff | ❌ 需 postMessage |
| 移动端适配 | ✅ 自动全屏 | ❌ 需自己处理 |
| 接入成本 | 低（引入 JS） | 极低（一行标签） |
| 定制能力 | 强 | 弱 |
| 适用场景 | 生产环境 | 快速验证、App WebView |

> **结论**：生产环境推荐 SDK 方式；快速验证或 App 端 WebView 可用直接 iframe。

## 生产环境部署建议

### 1. 安全加固

- `app_secret` 必须从环境变量读取，不要硬编码
- 服务端换 token 接口应校验用户身份（如登录态），避免被恶意调用
- token 缓存建议用 Redis（分布式部署时多实例共享）
- 生产环境必须使用 HTTPS

### 2. 高可用

- 第三方服务端多实例部署，token 缓存用 Redis 共享
- 换 token 接口失败时重试 2-3 次，间隔 1 秒
- SDK 端 token 过期时，调用 `CSCHAT.setToken()` 刷新

### 3. 性能优化

- SDK 文件（cs-chat.js）建议 CDN 加速，设置长缓存
- 客服系统静态资源（pc.html 等）开启 gzip 压缩
- 浮窗按钮延迟加载，不影响首屏性能

### 4. 监控告警

- 监控换 token 接口的成功率和响应时间
- 监控 SDK 加载失败率
- 客服系统不可用时，SDK 应显示友好提示

## 常见问题

### Q: token 过期了怎么办？
A: 服务端换 token 接口会自动刷新（提前 30 分钟）。前端检测到 token 过期时，重新调用 `/get-token` 获取新 token，然后调用 `CSCHAT.setToken(newToken)` 更新。

### Q: 同一用户在不同设备上的对话历史能同步吗？
A: 可以。将 `user_id` 与你的系统用户ID绑定（如 `user_12345`），同一用户在不同设备上使用相同的 `user_id`，即可同步历史对话。

### Q: 能自定义客服窗口的样式吗？
A: 可以。SDK 支持 `theme`（主题色）、`title`（标题）、`button_icon`（按钮图标）、`button_text`（按钮文字）等配置。更深度的定制可以直接修改 `pc.html`。

### Q: SDK 会污染我的页面样式吗？
A: 不会。SDK 使用 Shadow DOM 渲染浮窗按钮和弹窗，样式完全隔离，不会影响你的页面。

### Q: 移动端怎么接入？
A: SDK 会自动检测屏幕宽度，移动端（<768px）自动全屏弹窗。也可以直接用 `/chat/h5.html` 作为移动端全屏客服页面。
