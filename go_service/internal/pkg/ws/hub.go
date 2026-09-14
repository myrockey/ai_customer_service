package ws

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

// Client 表示一条 WebSocket 连接（用户端或管理端）
type Client struct {
	hub     *Hub
	conn    *websocket.Conn
	send    chan []byte
	done    chan struct{}
	doneOnce sync.Once
	userID   string // 用户端为用户ID；管理端为空
	isAdmin  bool
	tenantID string // 管理端所属租户（空=全局/演示模式）
}

// closeDone 保证 done 通道只关闭一次
func (cl *Client) closeDone() {
	cl.doneOnce.Do(func() { close(cl.done) })
}

// WriteLoop 单协程写循环，串行写入，避免并发写冲突
func (cl *Client) WriteLoop() {
	defer cl.conn.Close()
	for {
		select {
		case data, ok := <-cl.send:
			if !ok {
				return
			}
			if err := cl.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-cl.done:
			return
		}
	}
}

// Hub 管理所有 WebSocket 连接
type Hub struct {
	mu     sync.RWMutex
	users  map[string]*Client // userID -> client（每个用户仅保留一条活跃连接）
	admins map[*Client]bool

	// 租户实时连接数（用户端，用于并发会话配额校验）
	tenantConns map[string]int

	threadLocks   map[string]*sync.Mutex
	threadLocksMu sync.Mutex
}

func NewHub() *Hub {
	return &Hub{
		users:       make(map[string]*Client),
		admins:      make(map[*Client]bool),
		tenantConns: make(map[string]int),
		threadLocks: make(map[string]*sync.Mutex),
	}
}

// WSHub 全局 WebSocket Hub
var WSHub = NewHub()

// RegisterUser 注册用户连接，若已有旧连接则先断开并通知（防止旧连接无限重连互踢）
func (h *Hub) RegisterUser(userID, tenantID string, conn *websocket.Conn) *Client {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old := h.users[userID]; old != nil {
		// 通知旧连接：已在其他窗口建立新连接，停止自动重连
		if kick, err := json.Marshal(map[string]interface{}{
			"type": "kicked",
			"msg":  "连接已在其他窗口建立",
		}); err == nil {
			select {
			case old.send <- kick:
			default:
			}
		}
		old.closeDone()
		delete(h.users, userID)
		// 旧连接占用计数由 UnregisterUser（defer）释放
	}
	cl := &Client{
		hub: h, conn: conn, send: make(chan []byte, 32),
		done: make(chan struct{}), userID: userID, tenantID: tenantID,
	}
	h.users[userID] = cl
	if tenantID != "" {
		h.tenantConns[tenantID]++
	}
	return cl
}

func (h *Hub) UnregisterUser(userID string, cl *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.users[userID] == cl {
		delete(h.users, userID)
	}
	if cl.tenantID != "" {
		if h.tenantConns[cl.tenantID] > 0 {
			h.tenantConns[cl.tenantID]--
		}
	}
	cl.closeDone()
}

// CountTenantConns 返回租户当前在线连接数（用户端）
func (h *Hub) CountTenantConns(tenantID string) int {
	if tenantID == "" {
		return 0
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.tenantConns[tenantID]
}

// CountTenantConnsExcluding 返回租户当前在线连接数（排除指定 user_id 自身的旧连接，用于重连场景）
func (h *Hub) CountTenantConnsExcluding(tenantID, userID string) int {
	if tenantID == "" {
		return 0
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := h.tenantConns[tenantID]
	if _, ok := h.users[userID]; ok && n > 0 {
		n--
	}
	return n
}

// CountAllTenantConns 返回各租户实时连接数（用户端）
func (h *Hub) CountAllTenantConns() map[string]int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[string]int, len(h.tenantConns))
	for k, v := range h.tenantConns {
		out[k] = v
	}
	return out
}

func (h *Hub) RegisterAdmin(conn *websocket.Conn, tenantID string) *Client {
	h.mu.Lock()
	defer h.mu.Unlock()
	cl := &Client{
		hub: h, conn: conn, send: make(chan []byte, 64),
		done: make(chan struct{}), isAdmin: true, tenantID: tenantID,
	}
	h.admins[cl] = true
	return cl
}

func (h *Hub) UnregisterAdmin(cl *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.admins[cl] {
		delete(h.admins, cl)
	}
	cl.closeDone()
}

// SendToUser 向指定用户推送 JSON 消息
func (h *Hub) SendToUser(userID string, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.RLock()
	cl := h.users[userID]
	h.mu.RUnlock()
	if cl == nil {
		return
	}
	select {
	case cl.send <- data:
	case <-cl.done:
	default: // 发送缓冲已满，丢弃避免阻塞
	}
}

// SendToAdmins 向所有管理端广播 JSON 消息
func (h *Hub) SendToAdmins(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.admins))
	for cl := range h.admins {
		clients = append(clients, cl)
	}
	h.mu.RUnlock()
	for _, cl := range clients {
		select {
		case cl.send <- data:
		case <-cl.done:
		default:
		}
	}
}

// SendToAdminsByTenant 向指定租户的管理端广播 JSON 消息；tenantID 为空时广播所有管理端
func (h *Hub) SendToAdminsByTenant(tenantID string, v interface{}) {
	if tenantID == "" {
		h.SendToAdmins(v)
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.admins))
	for cl := range h.admins {
		if cl.tenantID == tenantID {
			clients = append(clients, cl)
		}
	}
	h.mu.RUnlock()
	for _, cl := range clients {
		select {
		case cl.send <- data:
		case <-cl.done:
		default:
		}
	}
}

// TryLockThread 尝试获取线程级互斥锁，避免同一会话并发调用 Agent
func (h *Hub) TryLockThread(threadID string) bool {
	h.threadLocksMu.Lock()
	m, ok := h.threadLocks[threadID]
	if !ok {
		m = &sync.Mutex{}
		h.threadLocks[threadID] = m
	}
	h.threadLocksMu.Unlock()
	return m.TryLock()
}

func (h *Hub) UnlockThread(threadID string) {
	h.threadLocksMu.Lock()
	m := h.threadLocks[threadID]
	h.threadLocksMu.Unlock()
	if m != nil {
		m.Unlock()
	}
}
