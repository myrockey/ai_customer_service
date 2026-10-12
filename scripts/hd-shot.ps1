# hd-shot.ps1 - 无头 Chrome CDP 大图截图（1920x1080，自动登录注入 localStorage）
# 用法: powershell -File hd-shot.ps1 -Route "#/tenants" -Out "docs\cs_images\平台-租户管理.PNG" -AppKey admin_key -Secret admin_secret
param(
    [string]$Route = "#/tenants",
    [string]$Out = "docs\cs_images\平台-租户管理.PNG",
    [string]$AppKey = "admin_key",
    [string]$Secret = "admin_secret",
    [string]$Tenant = "",          # 子账号登录：租户ID
    [string]$Username = "",        # 子账号登录：用户名
    [string]$UserPassword = "",    # 子账号登录：密码
    [int]$Width = 1920,
    [int]$Height = 1080,
    [string]$PageUrl = ""   # 自定义完整 URL（如 SDK 演示页），非空时忽略 Route
)

$ErrorActionPreference = "Stop"
$Chrome = "C:\Users\Administrator\AppData\Local\Google\Chrome\Application\chrome.exe"
$Port = 9333
$UserData = "C:\temp\hdshot-profile"
$Base = "http://127.0.0.1:8080"

if (Test-Path $UserData) { Remove-Item $UserData -Recurse -Force }
New-Item -ItemType Directory -Path $UserData -Force | Out-Null

# 1. 启动无头 Chrome
$args = @(
    "--headless=new",
    "--remote-debugging-port=$Port",
    "--user-data-dir=$UserData",
    "--window-size=$($Width),$($Height)",
    "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--disable-extensions",
    "about:blank"
)
$p = Start-Process -FilePath $Chrome -ArgumentList $args -PassThru -WindowStyle Hidden

# 2. 等待 devtools 就绪
$ver = $null
for ($i = 0; $i -lt 30; $i++) {
    try {
        $ver = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/json/version" -TimeoutSec 2
        break
    } catch { Start-Sleep -Milliseconds 500 }
}
if (-not $ver) { Write-Output "ERROR: devtools not ready"; Stop-Process -Id $p.Id -Force; exit 1 }
$wsUrl = $ver.webSocketDebuggerUrl

# 3. ClientWebSocket 连接
$ws = [System.Net.WebSockets.ClientWebSocket]::new()
$ws.ConnectAsync([Uri]$wsUrl, [Threading.CancellationToken]::None).GetAwaiter().GetResult()

$script:msgId = 0
$script:buffer = New-Object byte[] 1048576

function Send-Cdp($method, $params, $sessionId) {
    $script:msgId++
    $id = $script:msgId
    $obj = @{ id = $id; method = $method }
    if ($params) { $obj.params = $params }
    if ($sessionId) { $obj.sessionId = $sessionId }
    $json = $obj | ConvertTo-Json -Compress -Depth 10
    $bytes = [Text.Encoding]::UTF8.GetBytes($json)
    $seg = [ArraySegment[byte]]::new($bytes)
    $ws.SendAsync($seg, [System.Net.WebSockets.WebSocketMessageType]::Text, $true, [Threading.CancellationToken]::None).GetAwaiter().GetResult()
    # 等待匹配 id 的响应
    while ($true) {
        $ms = [System.IO.MemoryStream]::new()
        do {
            $seg2 = [ArraySegment[byte]]::new($script:buffer)
            $res = $ws.ReceiveAsync($seg2, [Threading.CancellationToken]::None).GetAwaiter().GetResult()
            $ms.Write($script:buffer, 0, $res.Count)
        } while (-not $res.EndOfMessage)
        $text = [Text.Encoding]::UTF8.GetString($ms.ToArray())
        $msg = $text | ConvertFrom-Json
        if ($msg.id -eq $id) { return $msg }
    }
}

function Send-Cdp-Eval($expr, $sessionId) {
    $r = Send-Cdp "Runtime.evaluate" @{ expression = $expr; returnByValue = $true; awaitPromise = $true } $sessionId
    return $r
}

# 4. 建 tab + attach
$t = Send-Cdp "Target.createTarget" @{ url = "about:blank" }
$targetId = $t.result.targetId
$att = Send-Cdp "Target.attachToTarget" @{ targetId = $targetId; flatten = $true }
$sid = $att.result.sessionId

# 5. 启用 + 视口
Send-Cdp "Page.enable" $null $sid | Out-Null
Send-Cdp "Runtime.enable" $null $sid | Out-Null
Send-Cdp "Emulation.setDeviceMetricsOverride" @{ width = $Width; height = $Height; deviceScaleFactor = 1; mobile = $false } $sid | Out-Null

# 6. 打开后台同源页并注入登录态
Send-Cdp "Page.navigate" @{ url = "$Base/admin/" } $sid | Out-Null
Start-Sleep -Seconds 2
$bodyExpr = if ($Username) { "{ tenant_id: '$Tenant', username: '$Username', password: '$UserPassword', encrypted: false }" } else { "{ app_key: '$AppKey', app_secret: '$Secret' }" }
$apiPath = if ($Username) { 'user-token' } else { 'token' }
$expr = @"
(async () => {
    const r = await fetch('$Base/api/auth/$apiPath', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify($bodyExpr)
    });
    const j = await r.json();
    if (j.code !== 0 || !j.data) return 'LOGIN_FAIL:' + j.code;
    const d = j.data;
    localStorage.setItem('cs_admin_token', d.token);
    localStorage.setItem('cs_admin_tenant', d.tenant_id || '');
    localStorage.setItem('cs_admin_plat', d.is_platform_admin ? '1' : '0');
    localStorage.setItem('cs_admin_is_admin', d.is_admin ? '1' : '0');
    localStorage.setItem('cs_admin_uid', String(d.user_id || 0));
    localStorage.setItem('cs_admin_role', d.role_code || d.role || '');
    localStorage.setItem('cs_admin_role_name', d.role_name || '');
    localStorage.setItem('cs_admin_tenant_name', d.tenant_name || d.tenant_id || '');
    localStorage.setItem('cs_admin_nickname', d.nickname || '');
    localStorage.setItem('cs_admin_perms', JSON.stringify(d.permissions || []));
    localStorage.setItem('cs_admin_roles', JSON.stringify(d.roles || []));
    localStorage.removeItem('cs_admin_menus');
    return 'OK';
})()
"@
$lr = Send-Cdp-Eval $expr $sid
$lrv = $lr.result.result.value
Write-Output "LOGIN: $lrv"

# 7. 跳转目标路由
$finalUrl = if ($PageUrl) { $PageUrl } else { "$Base/admin/?v=28$Route" }
Send-Cdp "Page.navigate" @{ url = $finalUrl } $sid | Out-Null
Start-Sleep -Seconds 3
# 等菜单渲染
Send-Cdp-Eval "new Promise(r => setTimeout(r, 1500))" $sid | Out-Null

# 8. 截图
$cap = Send-Cdp "Page.captureScreenshot" @{ format = "png" } $sid
$b64 = $cap.result.data
$bytes = [Convert]::FromBase64String($b64)
$outPath = Join-Path (Get-Location) $Out
$outDir = Split-Path $outPath -Parent
if ($outDir -and -not (Test-Path $outDir)) { New-Item -ItemType Directory -Path $outDir -Force | Out-Null }
[IO.File]::WriteAllBytes($outPath, $bytes)
Write-Output "SAVED: $outPath ($($bytes.Length) bytes)"

$ws.CloseAsync([System.Net.WebSockets.WebSocketCloseStatus]::NormalClosure, "done", [Threading.CancellationToken]::None).GetAwaiter().GetResult()
Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
