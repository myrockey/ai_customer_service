// 多端接入代码示例（单独文件，避免 </script> 与页面冲突）
window.CODE_EXAMPLES = {
  pc: `<!-- ============================================ -->
<!-- PC 网站接入：引入 SDK，自动渲染浮窗按钮       -->
<!-- ============================================ -->

<!-- 方式一：最简单，data-* 自动初始化 -->
<script src="https://你的客服域名/sdk/cs-chat.js"
        data-server="https://你的客服域名"
        data-token="服务端换取的access_token"
        data-title="在线客服"
        data-theme="#1890ff"
        data-button-icon="💬"
        data-button-text="客服">
</script>

<!-- 方式二：手动初始化（推荐，可调用 API） -->
<script src="https://你的客服域名/sdk/cs-chat.js" data-manual="true"></script>
<script>
  // 从你的服务端获取 token（app_secret 只存在服务端）
  async function initCustomerService() {
    const resp = await fetch('/api/get-cs-token');
    const data = await resp.json();

    CSCHAT.init({
      server: 'https://你的客服域名',
      token: data.token,              // 服务端换取的 token
      tenant_id: data.tenant_id,      // 租户ID
      title: '在线客服',
      theme: '#1890ff',
      position: 'right',              // right / left
      width: 380,
      height: 560,
      button_icon: '💬',
      button_text: '客服'
    });

    // 事件监听
    CSCHAT.on('message', function(msg) {
      console.log('收到消息:', msg.role, msg.content);
    });
    CSCHAT.on('handoff', function(data) {
      console.log('转人工:', data.ticket_no);
    });
  }
  initCustomerService();

  // API 调用示例
  // CSCHAT.open();              // 打开客服窗口
  // CSCHAT.close();             // 关闭客服窗口
  // CSCHAT.sendText('你好');    // 主动发送消息
  // CSCHAT.setToken(newToken);  // 更新 token
</script>`,

  h5: `<!-- ============================================ -->
<!-- H5 网页接入：跳转全屏客服页面 h5/chat.html    -->
<!-- ============================================ -->

<!-- 1. 页面上放一个"联系客服"入口 -->
<a href="javascript:openCustomerService()" class="cs-entry">
  💬 联系客服
</a>

<script>
const CS_CONFIG = {
  baseUrl: 'https://你的客服域名',
  tokenApi: '/api/get-cs-token',  // 你自己服务端的换token接口
  tenantId: 't_your_tenant',       // 你的租户ID
};

// user_id 管理（localStorage 持久化，同一浏览器同一用户）
function getCsUserId() {
  // 已登录用户：用系统用户ID（加前缀防枚举）
  if (window.userInfo && window.userInfo.id) {
    return CS_CONFIG.tenantId + ':user_' + window.userInfo.id;
  }
  // 游客：localStorage 持久化
  let uid = localStorage.getItem('cs_h5_user_id');
  if (!uid) {
    uid = 'h5_' + Date.now() + '_' + Math.random().toString(36).slice(2, 8);
    localStorage.setItem('cs_h5_user_id', uid);
  }
  return CS_CONFIG.tenantId + ':' + uid;
}

// 从服务端获取 token
async function fetchCsToken() {
  const resp = await fetch(CS_CONFIG.tokenApi);
  const data = await resp.json();
  return data.token;
}

// 打开客服页面（全屏）
async function openCustomerService() {
  const [token, userId] = await Promise.all([fetchCsToken(), getCsUserId()]);

  // 拼接 h5/chat.html URL（移动端专用页面）
  const url = CS_CONFIG.baseUrl + '/chat/h5.html'
    + '?token=' + encodeURIComponent(token)
    + '&user_id=' + encodeURIComponent(userId)
    + '&title=' + encodeURIComponent('在线客服')
    + '&theme=' + encodeURIComponent('#1890ff');

  // 当前页面跳转（推荐，体验类似微信对话）
  window.location.href = url;
}
</script>`,

  android: `// ============================================
// Android App 接入：WebView 加载 h5/chat.html
// ============================================

// CustomerServiceActivity.java
package com.yourcompany.app;

import android.annotation.SuppressLint;
import android.os.Bundle;
import android.webkit.WebChromeClient;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import androidx.appcompat.app.AppCompatActivity;
import java.net.URLEncoder;

public class CustomerServiceActivity extends AppCompatActivity {
    private WebView webView;
    private static final String CS_BASE_URL = "https://你的客服域名";
    private static final String TENANT_ID = "t_your_tenant";

    @SuppressLint("SetJavaScriptEnabled")
    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_customer_service);

        webView = findViewById(R.id.webview);

        // WebView 配置（必须开启 JS 和 DOM 存储）
        WebSettings settings = webView.getSettings();
        settings.setJavaScriptEnabled(true);
        settings.setDomStorageEnabled(true);
        settings.setCacheMode(WebSettings.LOAD_DEFAULT);

        webView.setWebViewClient(new WebViewClient());
        webView.setWebChromeClient(new WebChromeClient());

        // 注入 JSBridge（让 H5 能调用原生方法，如关闭 WebView）
        webView.addJavascriptInterface(new JsBridge(), "AndroidBridge");

        // 从服务端获取 token（异步）
        new Thread(() -> {
            String token = fetchTokenFromServer(); // 调用你的后端接口
            String userId = getUserId();             // 登录用用户ID，未登录用设备ID
            String url = CS_BASE_URL + "/chat/h5.html"
                + "?token=" + URLEncoder.encode(token, "UTF-8")
                + "&user_id=" + URLEncoder.encode(TENANT_ID + ":" + userId, "UTF-8")
                + "&title=" + URLEncoder.encode("在线客服", "UTF-8");
            runOnUiThread(() -> webView.loadUrl(url));
        }).start();
    }

    // user_id：登录用户用用户ID，未登录用设备ID
    private String getUserId() {
        String deviceId = android.provider.Settings.Secure.getString(
            getContentResolver(), android.provider.Settings.Secure.ANDROID_ID);
        return "app_" + deviceId;
    }

    // JSBridge：H5 点击返回按钮时关闭 Activity
    public class JsBridge {
        @android.webkit.JavascriptInterface
        public void close() {
            runOnUiThread(() -> finish());
        }
    }

    @Override
    public void onBackPressed() {
        if (webView.canGoBack()) webView.goBack();
        else super.onBackPressed();
    }
}`,

  ios: `// ============================================
// iOS App 接入：WKWebView 加载 h5/chat.html
// ============================================

// CustomerServiceViewController.swift
import UIKit
import WebKit

class CustomerServiceViewController: UIViewController {
    var webView: WKWebView!
    let CS_BASE_URL = "https://你的客服域名"
    let TENANT_ID = "t_your_tenant"

    override func viewDidLoad() {
        super.viewDidLoad()
        title = "在线客服"

        // WKWebView 配置
        let config = WKWebViewConfiguration()
        config.preferences.javaScriptEnabled = true

        // 注入 JSBridge（H5 调用原生方法）
        let userContentController = WKUserContentController()
        userContentController.add(self, name: "csClose")
        config.userContentController = userContentController

        webView = WKWebView(frame: view.bounds, configuration: config)
        webView.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        webView.navigationDelegate = self
        view.addSubview(webView)

        // 从服务端获取 token
        fetchToken { [weak self] token in
            guard let self = self else { return }
            let userId = self.getUserId()
            var urlStr = "\\(self.CS_BASE_URL)/chat/h5.html"
            urlStr += "?token=\\(token)"
            urlStr += "&user_id=\\(self.TENANT_ID):\\(userId)"
            urlStr += "&title=在线客服"

            if let url = URL(string: urlStr.addingPercentEncoding(
                withAllowedCharacters: .urlQueryAllowed)!) {
                self.webView.load(URLRequest(url: url))
            }
        }
    }

    // 从服务端获取 token
    func fetchToken(completion: @escaping (String) -> Void) {
        guard let url = URL(string: "https://你的后端域名/api/get-cs-token") else { return }
        URLSession.shared.dataTask(with: url) { data, _, _ in
            if let data = data,
               let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
               let token = json["token"] as? String {
                DispatchQueue.main.async { completion(token) }
            }
        }.resume()
    }

    // user_id：登录用户用用户ID，未登录用设备ID（IDFV）
    func getUserId() -> String {
        if let idfv = UIDevice.current.identifierForVendor?.uuidString {
            return "app_\\(idfv)"
        }
        return "app_guest_\\(Date().timeIntervalSince1970)"
    }
}

// MARK: - WKScriptMessageHandler（JSBridge）
extension CustomerServiceViewController: WKScriptMessageHandler {
    func userContentController(_ userContentController: WKUserContentController,
                                didReceive message: WKScriptMessage) {
        if message.name == "csClose" {
            // H5 点击返回按钮时，关闭当前 ViewController
            navigationController?.popViewController(animated: true)
        }
    }
}

extension CustomerServiceViewController: WKNavigationDelegate {}`,

  mini: `<!-- ============================================ -->
<!-- 微信小程序接入：web-view 加载 h5/chat.html    -->
<!-- ============================================ -->

<!-- 1. 页面结构 customer-service.wxml -->
<web-view src="{{csUrl}}" bindmessage="onMessage"></web-view>

<!-- 2. 页面逻辑 customer-service.js -->
Page({
  data: {
    csUrl: '',
    tenantId: 't_your_tenant'  // 你的租户ID
  },

  onLoad() {
    this.loadCustomerService();
  },

  async loadCustomerService() {
    try {
      // 1. 从你的服务端获取 token（小程序不能存 app_secret）
      const tokenRes = await wx.request({
        url: 'https://你的后端域名/api/get-cs-token',
        method: 'GET'
      });
      const token = tokenRes.data.token;

      // 2. 获取 user_id（用 openid，同一用户固定）
      const userId = this.getUserId();

      // 3. 拼接 h5/chat.html URL
      const url = 'https://你的客服域名/chat/h5.html'
        + '?token=' + encodeURIComponent(token)
        + '&user_id=' + encodeURIComponent(this.data.tenantId + ':' + userId)
        + '&title=' + encodeURIComponent('在线客服')
        + '&theme=' + encodeURIComponent('#1890ff');

      this.setData({ csUrl: url });
    } catch (err) {
      wx.showToast({ title: '客服暂时不可用', icon: 'none' });
    }
  },

  // user_id：用 openid，同一用户固定不变
  getUserId() {
    let openid = wx.getStorageSync('cs_openid');
    if (!openid) {
      // 实际项目中应通过 wx.login → 后端换 openid
      openid = 'wx_guest_' + Date.now() + '_' + Math.random().toString(36).slice(2, 8);
      wx.setStorageSync('cs_openid', openid);
    }
    return openid;
  },

  // 接收 web-view 发来的消息（用户点击关闭时触发）
  onMessage(e) {
    const msg = e.detail.data[e.detail.data.length - 1];
    if (msg && msg.type === 'cs_close') {
      wx.navigateBack();  // 返回小程序上一页
    }
  }
});

<!-- 3. 页面配置 customer-service.json -->
{
  "navigationBarTitleText": "在线客服",
  "navigationBarBackgroundColor": "#1890ff",
  "navigationBarTextStyle": "white"
}

<!-- 4. 从其他页面跳转 -->
wx.navigateTo({
  url: '/pages/customer-service/customer-service'
});

<!-- ⚠️ 前置配置：微信公众平台 → 开发 → 业务域名，添加客服系统域名 -->`
};
