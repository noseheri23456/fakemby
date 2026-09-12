// Package ws 实现 Emby 兼容的 WebSocket 端点（RFC6455）。
//
// 与官方客户端的交互契约（读源码 `Emby Theater/electronapp/www/modules/emby-apiclient/apiclient.js` 得出）：
//  1. 连接地址为 `/embywebsocket?api_key=<token>&deviceId=<id>`，鉴权走查询参数而非请求头；
//  2. 连接建立后客户端会为已注册的监听器补发 `<Name>Start` 订阅消息（Data 是形如
//     "0,1500,0,true,true" 的字符串），服务端应立刻回一条**同名**快照消息；
//     不回包不会报错，但客户端会退化成定时轮询（`isMessageChannelOpen() || poll()`）；
//  3. 断开前发 `<Name>Stop`；
//  4. 业务事件以 `{"MessageType":<名称>,"Data":<负载>}` 广播，客户端对 Data 零容错——
//     凡会被读到的 Data 必须是数组或对象，不能是 null（见 docs/ROADMAP.md §10）。
package ws

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Event 是所有 websocket 消息的统一信封。
// Data 使用 omitempty：KeepAlive / ForceKeepAlive 这类无负载消息序列化为
// `{"MessageType":"KeepAlive"}`，与官方服务器一致。
// 注意：凡客户端会读取 Data 的事件，调用方必须传非 nil 值（数组或对象）。
type Event struct {
	MessageType string `json:"MessageType"`
	Data        any    `json:"Data,omitempty"`
}

// Options 控制心跳与背压行为。生产用 DefaultOptions，测试可缩短间隔。
type Options struct {
	PingInterval time.Duration // 服务端主动 ping 的间隔
	PongWait     time.Duration // 读超时；收到任何数据帧或 pong 即续期
	WriteWait    time.Duration // 单帧写超时
	QueueSize    int           // 每客户端待发队列长度
	MaxClients   int           // 全局并发连接上限，超过直接拒绝握手
	ReadLimit    int64         // 单帧最大字节数

	// KeepAliveSeconds 为连接建立后下发的 ForceKeepAlive 秒数（官方服务器行为）。
	// <= 0 表示不下发。
	KeepAliveSeconds int
}

// DefaultOptions 返回生产默认值：25 秒一次 ping，70 秒无响应即判定死连接。
func DefaultOptions() Options {
	return Options{
		PingInterval:     25 * time.Second,
		PongWait:         70 * time.Second,
		WriteWait:        10 * time.Second,
		QueueSize:        32,
		MaxClients:       100,
		ReadLimit:        64 << 10,
		KeepAliveSeconds: 30,
	}
}

func (o Options) withDefaults() Options {
	d := DefaultOptions()
	if o.PingInterval <= 0 {
		o.PingInterval = d.PingInterval
	}
	if o.PongWait <= 0 {
		o.PongWait = d.PongWait
	}
	if o.WriteWait <= 0 {
		o.WriteWait = d.WriteWait
	}
	if o.QueueSize <= 0 {
		o.QueueSize = d.QueueSize
	}
	if o.MaxClients <= 0 {
		o.MaxClients = d.MaxClients
	}
	if o.ReadLimit <= 0 {
		o.ReadLimit = d.ReadLimit
	}
	return o
}

// SnapshotFunc 为订阅请求生成首帧快照。
// ok=false 表示该订阅名不受支持（服务端静默忽略，不回包）。
// 返回的 data 必须是数组或对象，禁止返回 nil。
type SnapshotFunc func(user, name, options string) (data any, ok bool)

type client struct {
	conn *websocket.Conn
	user string
	send chan []byte

	// mu 保护下面的订阅表与 sendClosed 标志。
	// send 通道只能在 Serve 收尾里关一次，且 Broadcast/Close 可能并发往里写，
	// 因此所有写通道的操作都必须走 trySend，不允许裸 send。
	mu         sync.Mutex
	subs       map[string]string
	sendClosed bool
}

func (c *client) subscribe(name, options string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.subs == nil {
		c.subs = map[string]string{}
	}
	c.subs[name] = options
}

func (c *client) unsubscribe(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.subs, name)
}

// subscriptions 返回当前订阅的副本，供测试与排障使用。
func (c *client) subscriptions() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.subs))
	for k, v := range c.subs {
		out[k] = v
	}
	return out
}

// trySend 非阻塞投递一条已序列化消息，通道已关闭或队列满时返回 false。
func (c *client) trySend(b []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sendClosed {
		return false
	}
	select {
	case c.send <- b:
		return true
	default:
		return false
	}
}

// closeSend 关闭发送通道，可重复调用。
func (c *client) closeSend() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sendClosed {
		return
	}
	c.sendClosed = true
	close(c.send)
}

// Hub 维护所有在线连接，负责心跳、订阅与广播。
type Hub struct {
	opt      Options
	mu       sync.Mutex
	clients  map[*client]struct{}
	closed   bool
	snapshot SnapshotFunc

	dropped atomic.Uint64 // 因队列满被丢弃的消息数
	served  atomic.Uint64 // 累计接受的连接数
}

// New 创建 Hub。传入零值 Options 会得到 DefaultOptions。
func New(opt Options) *Hub {
	return &Hub{opt: opt.withDefaults(), clients: map[*client]struct{}{}}
}

// SetSnapshotProvider 注入订阅快照回调，连接建立前调用一次即可。
func (h *Hub) SetSnapshotProvider(fn SnapshotFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snapshot = fn
}

// Clients 返回当前连接数。
func (h *Hub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Stats 返回累计连接数与丢弃消息数，供 /metrics 与测试使用。
func (h *Hub) Stats() (served, dropped uint64) {
	return h.served.Load(), h.dropped.Load()
}

// Broadcast 向 user 的所有连接广播事件；user 为空表示广播给所有人。
// 返回实际入队（未丢弃）的连接数。
func (h *Hub) Broadcast(user, kind string, data any) int {
	b, err := json.Marshal(Event{MessageType: kind, Data: data})
	if err != nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return 0
	}
	delivered := 0
	for c := range h.clients {
		if user != "" && c.user != user {
			continue
		}
		if c.trySend(b) {
			delivered++
			continue
		}
		// 队列满说明对端消费不过来（多半已经半死）。丢弃本条并关闭连接，
		// 由 readPump 收尾注销——绝不能为了等它而阻塞业务请求。
		h.dropped.Add(1)
		_ = c.conn.Close()
	}
	return delivered
}

// sendTo 向单个连接投递一条事件。
func (h *Hub) sendTo(c *client, kind string, data any) {
	b, err := json.Marshal(Event{MessageType: kind, Data: data})
	if err != nil {
		return
	}
	if c.trySend(b) {
		return
	}
	h.dropped.Add(1)
	_ = c.conn.Close()
}

// Close 关闭所有连接：先尽力广播 ServerShuttingDown，再关 send 通道，
// 由各 writePump 排空后发出关闭控制帧。这里不能同步写关闭帧——那会与 writePump 抢 conn，
// 很可能先于文本帧发出，导致告别消息根本没送达。
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()

	if len(clients) > 0 {
		if b, err := json.Marshal(Event{MessageType: "ServerShuttingDown"}); err == nil {
			for _, c := range clients {
				c.trySend(b)
			}
		}
	}
	// 关 send 通道让 writePump 排空后再发关闭帧。
	// 这里不能同步写 close 控制帧——那会与 writePump 抢 conn，
	// 很可能先把关闭帧发出去，导致 ServerShuttingDown 根本没送达。
	for _, c := range clients {
		c.closeSend()
	}
}

// Serve 升级连接并接管读写，直到对端断开。
// user 由调用方在鉴权后传入（空串表示匿名，通常不应出现）。
// origins 非空时用于校验浏览器 Origin。
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, user string, origins []string) {
	if h.Clients() >= h.opt.MaxClients {
		http.Error(w, "too many websocket connections", http.StatusServiceUnavailable)
		return
	}
	up := websocket.Upgrader{HandshakeTimeout: 5 * time.Second}
	if len(origins) > 0 {
		up.CheckOrigin = func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			for _, o := range origins {
				if o == origin {
					return true
				}
			}
			return false
		}
	}
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{conn: conn, user: user, send: make(chan []byte, h.opt.QueueSize)}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = conn.Close()
		return
	}
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	h.served.Add(1)

	if h.opt.KeepAliveSeconds > 0 {
		h.sendTo(c, "ForceKeepAlive", h.opt.KeepAliveSeconds)
	}

	done := make(chan struct{})
	go h.writePump(c, done)
	h.readPump(c)

	// 收尾顺序很关键：先在锁内摘掉注册表，再关 send 通道。
	// Broadcast 全程持锁遍历，摘除后不会再有人往这个通道写，因此不会 send on closed channel。
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	c.closeSend()
	// 等 writePump 把队列里剩余消息写完并发出关闭帧，再释放连接。
	// 提前 Close 会把尚未发出的消息（含 ServerShuttingDown）直接丢掉。
	<-done
	_ = conn.Close()
}

func (h *Hub) writePump(c *client, done chan struct{}) {
	defer func() {
		// 正常收尾也发一次关闭控制帧，客户端才能区分「服务端主动关」和「网络断了」。
		_ = c.conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(h.opt.WriteWait))
		_ = c.conn.Close()
		close(done)
	}()
	ticker := time.NewTicker(h.opt.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case b, ok := <-c.send:
			if !ok {
				return
			}
			if err := c.conn.SetWriteDeadline(time.Now().Add(h.opt.WriteWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, b); err != nil {
				_ = c.conn.Close()
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(h.opt.WriteWait)); err != nil {
				_ = c.conn.Close()
				return
			}
		}
	}
}

func (h *Hub) readPump(c *client) {
	conn := c.conn
	conn.SetReadLimit(h.opt.ReadLimit)
	_ = conn.SetReadDeadline(time.Now().Add(h.opt.PongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(h.opt.PongWait))
	})
	for {
		_, b, err := conn.ReadMessage()
		if err != nil {
			return
		}
		// 任何来自对端的帧都证明连接活着，续期读超时；
		// 这样即便中间设备吞掉 ping，只要客户端还在发包就不会被误杀。
		_ = conn.SetReadDeadline(time.Now().Add(h.opt.PongWait))
		h.handleMessage(c, b)
	}
}

func (h *Hub) handleMessage(c *client, b []byte) {
	var e Event
	if err := json.Unmarshal(b, &e); err != nil || e.MessageType == "" {
		return
	}
	switch {
	case strings.HasSuffix(e.MessageType, "Start"):
		name := strings.TrimSuffix(e.MessageType, "Start")
		if name == "" {
			return
		}
		// 官方客户端把订阅参数作为字符串放在 Data 里。
		opts, _ := e.Data.(string)
		c.subscribe(name, opts)
		h.sendSnapshot(c, name, opts)
	case strings.HasSuffix(e.MessageType, "Stop"):
		if name := strings.TrimSuffix(e.MessageType, "Stop"); name != "" {
			c.unsubscribe(name)
		}
	case e.MessageType == "KeepAlive":
		h.sendTo(c, "KeepAlive", nil)
	}
}

func (h *Hub) sendSnapshot(c *client, name, options string) {
	h.mu.Lock()
	fn := h.snapshot
	h.mu.Unlock()
	if fn == nil {
		return
	}
	data, ok := fn(c.user, name, options)
	if !ok {
		return
	}
	h.sendTo(c, name, data)
}

// clientsOf 返回指定用户的连接，测试用。内部会加 h.mu，调用方不得已持有该锁。
func (h *Hub) clientsOf(user string) []*client {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*client
	for c := range h.clients {
		if user == "" || c.user == user {
			out = append(out, c)
		}
	}
	return out
}
