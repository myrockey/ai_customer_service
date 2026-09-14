/*!
 * CS-Chat SDK v2.0 — 智能客服多端集成脚本（无依赖，单文件，iframe 模式）
 *
 * 架构：SDK 负责浮窗按钮 + 弹窗容器 + API + 事件，聊天内核用 iframe 加载 pc.html
 *
 * 快速开始（最简单，data-* 自动初始化）：
 *   <script src="https://你的域名/sdk/cs-chat.js"
 *           data-server="https://你的域名"
 *           data-token="宿主后端换取的token"
 *           data-title="在线客服"
 *           data-theme="#1890ff"></script>
 *
 * 手动初始化：
 *   <script src="cs-chat.js"></script>
 *   <script>
 *     CSCHAT.init({
 *       server: 'https://你的域名',
 *       token: 'xxx',        // 推荐：宿主后端换取后传入
 *       title: '在线客服',
 *       theme: '#1890ff',
 *       position: 'right',   // right / left
 *       width: 380,
 *       height: 560
 *     });
 *     CSCHAT.on('message', function(msg){ console.log('新消息:', msg); });
 *     CSCHAT.open();  // 主动打开
 *   </script>
 *
 * API: init / config / open / close / toggle / sendText / setToken / on / off / destroy
 * 事件: ready / open / close / message / handoff / human / error
 */
(function (global) {
  'use strict';

  // P1-5：SDK 版本常量（配合版本化 URL 使用，如 /sdk/cs-chat@2.0.0.js）
  var SDK_VERSION = '2.0.0';
  if (global) {
    try { global.CS_CHAT_SDK_VERSION = SDK_VERSION; } catch (e) {}
  }

  var NS = 'cs_sdk_uid';
  var DEFAULTS = {
    server: '',              // 客服系统地址（必填，如 https://你的域名）
    token: '',               // 访问令牌（推荐：宿主后端换取后传入）
    app_key: '',             // 或传 app_key + app_secret，SDK 自动换票（仅演示，生产勿用）
    app_secret: '',
    tenant_id: '',           // 直传 token 时建议传租户ID（用于 user_id 前缀）
    title: '在线客服',
    subtitle: '',
    theme: '#1890ff',
    position: 'right',       // right / left
    width: 380,
    height: 560,
    offsetX: 20,             // 距侧边距离 px
    offsetY: 20,             // 距底部距离 px
    autoShow: true,          // 是否自动显示浮窗按钮
    buttonText: '',          // 按钮文字（留空只显示图标）
    buttonIcon: '💬',        // 按钮图标
    zIndex: 9999,
    mobileThreshold: 768     // 小于此宽度视为移动端，全屏弹窗
  };

  var S = {
    cfg: null,
    uid: '',
    token: '',
    tenant: '',
    root: null,
    shadow: null,
    els: {},
    isOpen: false,
    iframeLoaded: false,
    listeners: {}
  };

  /* ================= 工具 ================= */
  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }
  function merge(base, ext) {
    var o = {}, k;
    for (k in base) o[k] = base[k];
    for (k in (ext || {})) if (ext[k] !== undefined && ext[k] !== '') o[k] = ext[k];
    return o;
  }
  function darken(hex) {
    try {
      var r = parseInt(hex.slice(1,3),16), g = parseInt(hex.slice(3,5),16), b = parseInt(hex.slice(5,7),16);
      return '#' + [r,g,b].map(function(x){return Math.max(0,x-30).toString(16).padStart(2,'0')}).join('');
    } catch(e) { return hex; }
  }
  function parseTenantId(token) {
    try {
      var p = token.split('.');
      var b64 = p.length === 3 ? p[1] : p[0];
      b64 = b64.replace(/-/g,'+').replace(/_/g,'/');
      while (b64.length % 4) b64 += '=';
      var d = JSON.parse(atob(b64));
      return d.tid || d.tenant_id || '';
    } catch(e) { return ''; }
  }

  /* ================= user_id ================= */
  function getUserId() {
    if (S.cfg.user_id) return S.cfg.user_id;
    var tid = S.tenant || 'unknown';
    var key = NS + '_' + tid;
    var id = null;
    try { id = localStorage.getItem(key); } catch(e) {}
    if (!id) {
      id = 'u_' + Date.now().toString(36) + Math.random().toString(36).slice(2,8);
      try { localStorage.setItem(key, id); } catch(e) {}
    }
    return tid + ':' + id;
  }

  /* ================= token ================= */
  function ensureToken(cb) {
    if (S.cfg.token) {
      S.token = S.cfg.token;
      S.tenant = S.cfg.tenant_id || parseTenantId(S.token);
      cb();
      return;
    }
    if (S.cfg.app_key && S.cfg.app_secret) {
      var xhr = new XMLHttpRequest();
      xhr.open('POST', S.cfg.server.replace(/\/$/,'') + '/api/auth/token', true);
      xhr.setRequestHeader('Content-Type', 'application/json');
      xhr.onreadystatechange = function() {
        if (xhr.readyState === 4) {
          try {
            var data = JSON.parse(xhr.responseText);
            if (data.code === 0 && data.data && data.data.token) {
              S.token = data.data.token;
              S.tenant = data.data.tenant_id || parseTenantId(S.token);
              cb();
            } else {
              console.error('[CS-Chat] 换token失败:', data.msg);
              cb(data.msg || '换token失败');
            }
          } catch(e) { cb('解析响应失败'); }
        }
      };
      xhr.send(JSON.stringify({app_key: S.cfg.app_key, app_secret: S.cfg.app_secret}));
      return;
    }
    cb('缺少 token 或 app_key/app_secret');
  }

  /* ================= 事件 ================= */
  function emit(event, data) {
    var fns = S.listeners[event] || [];
    for (var i = 0; i < fns.length; i++) {
      try { fns[i](data); } catch(e) { console.error('[CS-Chat] 事件回调异常:', e); }
    }
  }

  /* ================= DOM 渲染 ================= */
  function buildUI() {
    if (S.root) return;

    S.root = document.createElement('div');
    S.root.id = 'cs-chat-sdk-root';
    S.root.style.cssText = 'position:fixed;z-index:' + S.cfg.zIndex + ';';
    document.body.appendChild(S.root);

    S.shadow = S.root.attachShadow({mode: 'open'});

    var style = document.createElement('style');
    style.textContent = getCSS();
    S.shadow.appendChild(style);

    // 浮窗按钮
    var btn = document.createElement('button');
    btn.className = 'cs-float-btn';
    btn.innerHTML = '<span class="cs-btn-icon">' + esc(S.cfg.buttonIcon) + '</span>' +
                    (S.cfg.buttonText ? '<span class="cs-btn-text">' + esc(S.cfg.buttonText) + '</span>' : '');
    btn.addEventListener('click', toggle);
    S.shadow.appendChild(btn);
    S.els.btn = btn;

    // 弹窗容器
    var panel = document.createElement('div');
    panel.className = 'cs-panel';
    panel.innerHTML =
      '<div class="cs-panel-header">' +
        '<span class="cs-panel-title">' + esc(S.cfg.title) + '</span>' +
        '<button class="cs-panel-close" aria-label="关闭">✕</button>' +
      '</div>' +
      '<div class="cs-panel-body">' +
        '<iframe class="cs-iframe" sandbox="allow-scripts allow-same-origin allow-forms" frameborder="0"></iframe>' +
      '</div>';
    S.shadow.appendChild(panel);
    S.els.panel = panel;
    S.els.iframe = panel.querySelector('.cs-iframe');
    S.els.closeBtn = panel.querySelector('.cs-panel-close');
    S.els.closeBtn.addEventListener('click', close);

    applyPosition();

    // 监听 iframe postMessage
    window.addEventListener('message', handleIframeMessage);
  }

  function applyPosition() {
    var isMobile = window.innerWidth < S.cfg.mobileThreshold;
    var pos = S.cfg.position === 'left' ? 'left' : 'right';
    var btn = S.els.btn;
    var panel = S.els.panel;

    if (isMobile) {
      btn.style[pos] = '16px';
      btn.style.bottom = '16px';
      panel.style.cssText += ';left:0;right:0;bottom:0;top:0;width:100%;height:100%;border-radius:0;';
    } else {
      btn.style[pos] = S.cfg.offsetX + 'px';
      btn.style.bottom = S.cfg.offsetY + 'px';
      panel.style[pos] = S.cfg.offsetX + 'px';
      panel.style.bottom = (S.cfg.offsetY + 64) + 'px';
      panel.style.width = S.cfg.width + 'px';
      panel.style.height = S.cfg.height + 'px';
    }

    // 设置主题色
    S.shadow.querySelectorAll('.cs-float-btn').forEach(function(el){
      el.style.background = 'linear-gradient(135deg,' + S.cfg.theme + ',' + darken(S.cfg.theme) + ')';
    });
  }

  function getCSS() {
    return '.cs-float-btn{position:fixed;display:flex;align-items:center;gap:6px;padding:12px 16px;' +
      'border:none;border-radius:28px;color:#fff;font-size:14px;font-weight:600;cursor:pointer;' +
      'box-shadow:0 4px 16px rgba(0,0,0,.15);transition:transform .2s,box-shadow .2s;font-family:inherit;}' +
      '.cs-float-btn:hover{transform:scale(1.05);box-shadow:0 6px 24px rgba(0,0,0,.2);}' +
      '.cs-float-btn:active{transform:scale(.98);}' +
      '.cs-btn-icon{font-size:18px;line-height:1;}' +
      '.cs-panel{position:fixed;display:none;flex-direction:column;background:#fff;' +
      'border-radius:12px;overflow:hidden;box-shadow:0 8px 40px rgba(0,0,0,.18);' +
      'animation:csSlideUp .25s ease;}' +
      '@keyframes csSlideUp{from{opacity:0;transform:translateY(16px);}to{opacity:1;transform:translateY(0);}}' +
      '.cs-panel.open{display:flex;}' +
      '.cs-panel-header{display:flex;align-items:center;justify-content:space-between;' +
      'padding:12px 16px;background:linear-gradient(135deg,' + S.cfg.theme + ',' + darken(S.cfg.theme) + ');color:#fff;flex-shrink:0;}' +
      '.cs-panel-title{font-size:15px;font-weight:600;}' +
      '.cs-panel-close{background:rgba(255,255,255,.15);border:none;color:#fff;width:28px;height:28px;' +
      'border-radius:6px;cursor:pointer;font-size:14px;display:flex;align-items:center;justify-content:center;}' +
      '.cs-panel-close:hover{background:rgba(255,255,255,.3);}' +
      '.cs-panel-body{flex:1;overflow:hidden;position:relative;}' +
      '.cs-iframe{width:100%;height:100%;border:none;display:block;}';
  }

  /* ================= iframe 通信 ================= */
  function handleIframeMessage(e) {
    if (!e.data || typeof e.data !== 'object') return;
    if (e.data.source !== 'cs-chat') return;

    switch(e.data.type) {
      case 'cs_ready':
        S.iframeLoaded = true;
        emit('ready', e.data);
        break;
      case 'cs_message':
        emit('message', e.data);
        break;
      case 'cs_handoff':
        emit('handoff', e.data);
        break;
      case 'cs_human':
        emit('human', e.data);
        break;
      case 'cs_error':
        emit('error', e.data);
        break;
      case 'cs_close':
        // iframe 内点击关闭按钮
        close();
        break;
    }
  }

  function postToIframe(type, data) {
    if (S.els.iframe && S.els.iframe.contentWindow) {
      try {
        S.els.iframe.contentWindow.postMessage(Object.assign({type: type}, data || {}), '*');
      } catch(e) {}
    }
  }

  /* ================= 核心 API ================= */
  function init(cfg) {
    if (S.cfg) { console.warn('[CS-Chat] 已初始化，调用 config() 更新配置'); return; }
    S.cfg = merge(DEFAULTS, cfg || {});
    if (!S.cfg.server) {
      var scripts = document.querySelectorAll('script[data-server]');
      for (var i = 0; i < scripts.length; i++) {
        if (scripts[i].src.indexOf('cs-chat') >= 0) {
          S.cfg.server = scripts[i].getAttribute('data-server') || '';
          break;
        }
      }
    }
    if (!S.cfg.server) {
      // 同域部署回退：SDK 与客服系统同源（经 nginx 反代 /api、/ws）时自动推导
      S.cfg.server = location.origin;
    }
    if (!S.cfg.server) {
      console.error('[CS-Chat] 缺少 server 配置');
      return;
    }

    // 先渲染浮窗按钮（即使 token 还没获取到）
    if (S.cfg.autoShow !== false) buildUI();

    // 异步获取 token
    ensureToken(function(err) {
      if (err) {
        console.warn('[CS-Chat] token 获取失败，调用 setToken() 后可使用:', err);
        return;
      }
      S.uid = getUserId();
      emit('init', {user_id: S.uid, tenant_id: S.tenant});
    });
  }

  function config(cfg) {
    if (!S.cfg) { init(cfg); return; }
    S.cfg = merge(S.cfg, cfg);
    if (S.els.btn) {
      // 更新按钮文字
      S.els.btn.innerHTML = '<span class="cs-btn-icon">' + esc(S.cfg.buttonIcon) + '</span>' +
        (S.cfg.buttonText ? '<span class="cs-btn-text">' + esc(S.cfg.buttonText) + '</span>' : '');
    }
    if (S.els.panel) {
      S.els.panel.querySelector('.cs-panel-title').textContent = S.cfg.title;
      applyPosition();
      postToIframe('cs_config', {title: S.cfg.title, theme: S.cfg.theme});
    }
    if (cfg.token) {
      S.token = cfg.token;
      S.tenant = cfg.tenant_id || parseTenantId(cfg.token);
      S.uid = getUserId();
      if (!S.root && S.cfg.autoShow !== false) buildUI();
      if (S.isOpen) loadIframe();
    }
  }

  function loadIframe() {
    if (!S.els.iframe) return;
    var url = S.cfg.server.replace(/\/$/,'') + '/chat/pc.html' +
      '?token=' + encodeURIComponent(S.token) +
      '&user_id=' + encodeURIComponent(S.uid) +
      '&title=' + encodeURIComponent(S.cfg.title) +
      '&theme=' + encodeURIComponent(S.cfg.theme);
    if (S.cfg.subtitle) url += '&subtitle=' + encodeURIComponent(S.cfg.subtitle);
    S.els.iframe.src = url;
    S.iframeLoaded = false;
  }

  function open() {
    if (!S.cfg) { console.error('[CS-Chat] 请先调用 init()'); return; }
    if (!S.root) buildUI();
    if (S.isOpen) return;
    if (!S.els.iframe.src) loadIframe();
    S.els.panel.classList.add('open');
    S.isOpen = true;
    emit('open');
  }

  function close() {
    if (!S.isOpen) return;
    S.els.panel.classList.remove('open');
    S.isOpen = false;
    emit('close');
  }

  function toggle() {
    if (S.isOpen) close(); else open();
  }

  function sendText(text) {
    if (!text) return;
    if (!S.isOpen) open();
    // 等待 iframe 加载完成
    if (S.iframeLoaded) {
      postToIframe('cs_send', {text: text});
    } else {
      var check = setInterval(function() {
        if (S.iframeLoaded) {
          postToIframe('cs_send', {text: text});
          clearInterval(check);
        }
      }, 100);
      setTimeout(function(){ clearInterval(check); }, 10000);
    }
  }

  function setToken(token, tenantId) {
    config({token: token, tenant_id: tenantId || ''});
  }

  function on(event, fn) {
    if (!S.listeners[event]) S.listeners[event] = [];
    S.listeners[event].push(fn);
  }

  function off(event, fn) {
    if (!S.listeners[event]) return;
    if (!fn) { S.listeners[event] = []; return; }
    S.listeners[event] = S.listeners[event].filter(function(f){ return f !== fn; });
  }

  function destroy() {
    window.removeEventListener('message', handleIframeMessage);
    if (S.root) { S.root.remove(); S.root = null; }
    S.cfg = null;
    S.isOpen = false;
    S.iframeLoaded = false;
    S.listeners = {};
  }

  /* ================= data-* 自动初始化 ================= */
  function autoInit() {
    var scripts = document.querySelectorAll('script[src*="cs-chat"]');
    for (var i = 0; i < scripts.length; i++) {
      var s = scripts[i];
      if (s.getAttribute('data-manual') === 'true') continue;
      var cfg = {};
      var attrs = ['server','token','app-key','app-secret','tenant-id','title','subtitle','theme',
                   'position','width','height','offset-x','offset-y','button-text','button-icon','auto-show'];
      for (var j = 0; j < attrs.length; j++) {
        var val = s.getAttribute('data-' + attrs[j]);
        if (val !== null && val !== '') {
          var key = attrs[j].replace(/-([a-z])/g, function(_,c){ return c.toUpperCase(); });
          if (key === 'autoShow') cfg.autoShow = val !== 'false';
          else if (key === 'width' || key === 'height' || key === 'offsetX' || key === 'offsetY') cfg[key] = parseInt(val,10);
          else cfg[key] = val;
        }
      }
      if (cfg.server || cfg.token) {
        init(cfg);
        break;
      }
    }
  }

  // DOM 就绪后自动初始化
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', autoInit);
  } else {
    autoInit();
  }

  /* ================= 导出 API ================= */
  var CSCHAT = {
    version: '2.0.0',
    init: init,
    config: config,
    open: open,
    close: close,
    toggle: toggle,
    sendText: sendText,
    setToken: setToken,
    on: on,
    off: off,
    destroy: destroy,
    getUserId: function(){ return S.uid; },
    isOpen: function(){ return S.isOpen; }
  };

  global.CSCHAT = CSCHAT;
  if (typeof module !== 'undefined' && module.exports) module.exports = CSCHAT;

})(typeof window !== 'undefined' ? window : this);
