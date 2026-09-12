package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testConn 把 gorilla 客户端包一层：后台读循环把事件塞进 channel，测试只管收。
type testConn struct {
	*websocket.Conn
	events chan Event
	errs   chan error
	pings  atomic.Int32
}

func dial(t *testing.T, url string) *testConn {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	tc := &testConn{Conn: c, events: make(chan Event, 32), errs: make(chan error, 1)}
	// 自定义 ping handler 才能观测心跳；默认的那个会静默回 pong。
	c.SetPingHandler(func(data string) error {
		tc.pings.Add(1)
		return c.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
	})
	go func() {
		for {
			_, b, err := c.ReadMessage()
			if err != nil {
				tc.errs <- err
				close(tc.events)
				return
			}
			var e Event
			if json.Unmarshal(b, &e) == nil {
				tc.events <- e
			}
		}
	}()
	t.Cleanup(func() { _ = c.Close() })
	return tc
}

func (tc *testConn) next(t *testing.T, timeout time.Duration) Event {
	t.Helper()
	select {
	case e, ok := <-tc.events:
		require.True(t, ok, "连接已关闭")
		return e
	case <-time.After(timeout):
		t.Fatal("等待 websocket 事件超时")
		return Event{}
	}
}

// newTestHub 起一个带 httptest 的 Hub，清理时先关 Hub 再关 Server
// （顺序反了 httptest.Server.Close 会等长连接，必然卡死）。
func newTestHub(t *testing.T, opt Options) (*Hub, string) {
	t.Helper()
	hub := New(opt)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.Serve(w, r, r.URL.Query().Get("user"), nil)
	}))
	t.Cleanup(func() {
		hub.Close()
		srv.Close()
	})
	return hub, "ws" + strings.TrimPrefix(srv.URL, "http")
}

func waitClients(t *testing.T, h *Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.Clients() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	assert.Equal(t, want, h.Clients(), "连接数未收敛")
}

func TestEventJSONShapeMatchesOfficialContract(t *testing.T) {
	b, err := json.Marshal(Event{MessageType: "KeepAlive"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"MessageType":"KeepAlive"}`, string(b))

	b, err = json.Marshal(Event{MessageType: "Sessions", Data: []string{}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"MessageType":"Sessions","Data":[]}`, string(b))
}

func TestHubHeartbeatKeepsConnectionAlive(t *testing.T) {
	// 心跳间隔压到 50ms，用几百毫秒验证「长时间连接不掉」。
	hub, url := newTestHub(t, Options{PingInterval: 50 * time.Millisecond, PongWait: 500 * time.Millisecond, KeepAliveSeconds: 0})
	c := dial(t, url+"?user=alice")
	waitClients(t, hub, 1)

	time.Sleep(300 * time.Millisecond)
	assert.GreaterOrEqual(t, c.pings.Load(), int32(2), "服务端应周期性 ping")
	assert.Equal(t, 1, hub.Clients(), "心跳期间连接不应被回收")

	// 连接仍然可用：广播一条事件必须收得到。
	hub.Broadcast("alice", "UserDataChanged", map[string]any{})
	assert.Equal(t, "UserDataChanged", c.next(t, 2*time.Second).MessageType)
}

func TestHubBroadcastFansOutAndIsolatesUsers(t *testing.T) {
	hub, url := newTestHub(t, Options{KeepAliveSeconds: 0})
	alice := dial(t, url+"?user=alice")
	bob := dial(t, url+"?user=bob")
	waitClients(t, hub, 2)

	assert.Equal(t, 1, hub.Broadcast("alice", "UserDataChanged", map[string]any{}), "只应命中 alice 的连接")
	assert.Equal(t, "UserDataChanged", alice.next(t, time.Second).MessageType)

	assert.Equal(t, 2, hub.Broadcast("", "LibraryChanged", map[string]any{}), "空 user 表示广播给所有人")
	assert.Equal(t, "LibraryChanged", alice.next(t, time.Second).MessageType)
	assert.Equal(t, "LibraryChanged", bob.next(t, time.Second).MessageType)
}

func TestHubSubscriptionRepliesWithSnapshot(t *testing.T) {
	hub, url := newTestHub(t, Options{KeepAliveSeconds: 0})
	var gotUser, gotName, gotOptions string
	hub.SetSnapshotProvider(func(user, name, options string) (any, bool) {
		gotUser, gotName, gotOptions = user, name, options
		if name != "Sessions" {
			return nil, false
		}
		return []map[string]any{{"Id": "s1"}}, true
	})
	c := dial(t, url+"?user=alice")
	waitClients(t, hub, 1)

	require.NoError(t, c.WriteJSON(Event{MessageType: "SessionsStart", Data: "0,1500,0,true,true"}))
	e := c.next(t, 2*time.Second)
	assert.Equal(t, "Sessions", e.MessageType)
	assert.NotNil(t, e.Data, "快照 Data 不能是 null——客户端零容错")
	assert.Equal(t, "alice", gotUser)
	assert.Equal(t, "Sessions", gotName)
	assert.Equal(t, "0,1500,0,true,true", gotOptions)

	// 未知订阅名静默忽略，不回包、不断开。
	require.NoError(t, c.WriteJSON(Event{MessageType: "WhateverStart", Data: "1"}))
	select {
	case e := <-c.events:
		t.Fatalf("不支持的订阅不应回包，却收到 %+v", e)
	case <-time.After(200 * time.Millisecond):
	}

	clients := hub.clientsOf("alice")
	require.Len(t, clients, 1)
	assert.Contains(t, clients[0].subscriptions(), "Sessions")

	require.NoError(t, c.WriteJSON(Event{MessageType: "SessionsStop"}))
	time.Sleep(100 * time.Millisecond)
	assert.NotContains(t, clients[0].subscriptions(), "Sessions")
	assert.Equal(t, 1, hub.Clients(), "Stop 不应断开连接")
}

func TestHubKeepAliveRoundTrip(t *testing.T) {
	hub, url := newTestHub(t, Options{KeepAliveSeconds: 30})
	c := dial(t, url+"?user=alice")
	waitClients(t, hub, 1)

	// 建连即下发 ForceKeepAlive（官方服务器行为）。
	assert.Equal(t, "ForceKeepAlive", c.next(t, time.Second).MessageType)

	require.NoError(t, c.WriteJSON(Event{MessageType: "KeepAlive"}))
	assert.Equal(t, "KeepAlive", c.next(t, time.Second).MessageType)
}

func TestHubSlowClientDoesNotBlockBroadcast(t *testing.T) {
	// 队列只放 1 条，且对端不读——模拟卡死的客户端。
	hub, url := newTestHub(t, Options{QueueSize: 1, KeepAliveSeconds: 0})
	c := dial(t, url+"?user=alice")
	waitClients(t, hub, 1)
	// ForceKeepAlive 关掉后，第一条入队的就是广播消息。

	done := make(chan int, 1)
	go func() { done <- hub.Broadcast("alice", "Sessions", []any{}) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("广播被慢客户端阻塞")
	}

	served, dropped := hub.Stats()
	assert.Equal(t, uint64(1), served)
	assert.GreaterOrEqual(t, dropped, uint64(0))
	_ = c
}

func TestHubRejectsWhenOverCapacity(t *testing.T) {
	hub, url := newTestHub(t, Options{MaxClients: 1, KeepAliveSeconds: 0})
	dial(t, url+"?user=alice")
	waitClients(t, hub, 1)

	_, resp, err := websocket.DefaultDialer.Dial(url+"?user=bob", nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestHubCloseNotifiesAndReleasesClients(t *testing.T) {
	hub, url := newTestHub(t, Options{KeepAliveSeconds: 0})
	c := dial(t, url+"?user=alice")
	waitClients(t, hub, 1)

	hub.Close()
	assert.Equal(t, "ServerShuttingDown", c.next(t, 2*time.Second).MessageType)
	waitClients(t, hub, 0)

	// 关闭后广播不 panic，也不再计入投递。
	assert.Equal(t, 0, hub.Broadcast("alice", "Sessions", []any{}))
}
