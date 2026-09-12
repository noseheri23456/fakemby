package emby_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var wsPaths = []string{"/embywebsocket", "/embysocket", "/emby/embysocket", "/emby/embywebsocket"}

func wsDial(t *testing.T, a api, path, token string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(a.server.URL, "http") + path
	h := http.Header{}
	if token != "" {
		h.Set("X-Emby-Token", token)
	}
	conn, resp, err := websocket.DefaultDialer.Dial(url, h)
	require.NoError(t, err, "握手失败 %s", path)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func wsRead(t *testing.T, conn *websocket.Conn, timeout time.Duration) map[string]any {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(timeout)))
	_, b, err := conn.ReadMessage()
	require.NoError(t, err, "未在超时前收到 websocket 消息")
	var out map[string]any
	require.NoError(t, json.Unmarshal(b, &out))
	return out
}

// wsDrainHello 丢弃建连后服务端主动下发的 ForceKeepAlive，让后续断言只看业务事件。
func wsDrainHello(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	require.Equal(t, "ForceKeepAlive", wsRead(t, conn, 2*time.Second)["MessageType"])
}

// wsReadUntil 跳过无关消息（如建连即发的 ForceKeepAlive），取第一条指定类型的事件。
func wsReadUntil(t *testing.T, conn *websocket.Conn, messageType string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(500*time.Millisecond)))
		_, b, err := conn.ReadMessage()
		if err != nil {
			continue // 读超时，继续等
		}
		var out map[string]any
		if json.Unmarshal(b, &out) != nil {
			continue
		}
		if out["MessageType"] == messageType {
			return out
		}
	}
	t.Fatalf("未收到 %s 事件", messageType)
	return nil
}

func wsExpectSilence(t *testing.T, conn *websocket.Conn, d time.Duration) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(d)))
	_, b, err := conn.ReadMessage()
	require.Error(t, err, "不应收到事件，却收到: %s", string(b))
}

// M3-3：websocket 必须先鉴权，否则任何人都能订阅到别人的播放动态。
func TestWebSocketRequiresAuthentication(t *testing.T) {
	a, _ := newAPI(t)
	for _, path := range wsPaths {
		t.Run(path, func(t *testing.T) {
			url := "ws" + strings.TrimPrefix(a.server.URL, "http") + path
			_, resp, err := websocket.DefaultDialer.Dial(url, nil)
			require.Error(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		})
	}
}

// 官方客户端把 token 放在 ?api_key= 查询参数里（apiclient.js openWebSocket）。
func TestWebSocketHandshakeAcceptsHeaderAndAPIKey(t *testing.T) {
	a, _ := newAPI(t)
	for _, path := range wsPaths {
		wsDial(t, a, path, testutil.NormalToken)
		wsDial(t, a, path+"?api_key="+testutil.NormalToken, "")
	}
}

// M3-3 订阅协议：客户端发 `<Name>Start`，服务端必须回同名首帧，
// 否则客户端会退化成定时轮询。Data 恒为数组，不能是 null。
func TestWebSocketSubscriptionRepliesWithSnapshot(t *testing.T) {
	a, _ := newAPI(t)
	conn := wsDial(t, a, "/embywebsocket", testutil.NormalToken)
	wsDrainHello(t, conn)

	for _, name := range []string{"Sessions", "ScheduledTasksInfo", "ActivityLogEntry"} {
		require.NoError(t, conn.WriteJSON(map[string]any{"MessageType": name + "Start", "Data": "0,1500"}))
		event := wsRead(t, conn, 2*time.Second)
		require.Equal(t, name, event["MessageType"])
		data, ok := event["Data"].([]any)
		require.True(t, ok, "%s 的 Data 必须是数组，实际 %#v", name, event["Data"])
		assert.NotNil(t, data)
	}

	// 停止订阅不应断开连接：再发一次 Start 仍然有回应。
	require.NoError(t, conn.WriteJSON(map[string]any{"MessageType": "SessionsStop"}))
	require.NoError(t, conn.WriteJSON(map[string]any{"MessageType": "SessionsStart", "Data": ""}))
	assert.Equal(t, "Sessions", wsRead(t, conn, 2*time.Second)["MessageType"])
}

func TestWebSocketKeepAliveRoundTrip(t *testing.T) {
	a, _ := newAPI(t)
	conn := wsDial(t, a, "/embywebsocket", testutil.NormalToken)

	// 建连即下发 ForceKeepAlive（官方服务器行为），客户端据此决定心跳间隔。
	assert.Equal(t, "ForceKeepAlive", wsRead(t, conn, 2*time.Second)["MessageType"])

	require.NoError(t, conn.WriteJSON(map[string]any{"MessageType": "KeepAlive"}))
	event := wsRead(t, conn, 2*time.Second)
	assert.Equal(t, "KeepAlive", event["MessageType"])
	assert.NotContains(t, event, "Data", "KeepAlive 无负载，不应带 Data")
}

// 收藏/已看这类非播放变更也要推 UserDataChanged，否则多端不同步。
func TestWebSocketReceivesUserDataChangedOnFavorite(t *testing.T) {
	a, _ := newAPI(t)
	conn := wsDial(t, a, "/embywebsocket", testutil.NormalToken)

	path := "/emby/Users/" + testutil.NormalUserID + "/FavoriteItems/" + testutil.MovieID
	require.Equal(t, 200, a.post(path, testutil.NormalToken, nil).Status)

	event := wsReadUntil(t, conn, "UserDataChanged", 3*time.Second)
	// 官方契约把负载放在 Data 里：{"MessageType":"UserDataChanged","Data":{"UserId":..,"UserDataList":[..]}}
	data, ok := event["Data"].(map[string]any)
	require.True(t, ok, "UserDataChanged 的 Data 必须是对象，实际 %#v", event["Data"])
	assert.Equal(t, testutil.NormalUserID, data["UserId"])
	list, ok := data["UserDataList"].([]any)
	require.True(t, ok, "UserDataList 必须是数组，实际 %#v", data["UserDataList"])
	require.NotEmpty(t, list)
	first := list[0].(map[string]any)
	assert.Equal(t, testutil.MovieID, first["ItemId"])
	assert.Equal(t, true, first["IsFavorite"])
}

// 播放进度应同时推 UserDataChanged 与 Sessions 快照。
func TestWebSocketReceivesPlaybackEvents(t *testing.T) {
	a, _ := newAPI(t)
	conn := wsDial(t, a, "/embywebsocket", testutil.NormalToken)

	payload := []byte(`{"ItemId":"` + testutil.MovieID + `","PlaySessionId":"ws-m3-3","PositionTicks":123}`)
	require.Equal(t, 204, a.post("/emby/Sessions/Playing/Progress", testutil.NormalToken, payload).Status)

	assert.Equal(t, "UserDataChanged", wsReadUntil(t, conn, "UserDataChanged", 3*time.Second)["MessageType"])
	sessions := wsReadUntil(t, conn, "Sessions", 3*time.Second)
	list, ok := sessions["Data"].([]any)
	require.True(t, ok, "Sessions 的 Data 必须是数组")
	require.Len(t, list, 1)
	session := list[0].(map[string]any)
	assert.Equal(t, "ws-m3-3", session["Id"])
	// 数组字段必须初始化为空切片，客户端会裸调 .includes/.length。
	for _, field := range []string{"PlayableMediaTypes", "SupportedCommands", "AdditionalUsers"} {
		assert.NotNil(t, session[field], field)
	}
	caps, ok := session["Capabilities"].(map[string]any)
	require.True(t, ok)
	assert.NotNil(t, caps["PlayableMediaTypes"])
	assert.NotNil(t, caps["SupportedCommands"])

	require.Equal(t, 204, a.post("/emby/Sessions/Playing/Stopped", testutil.NormalToken, payload).Status)
}

// 反向用例：不能把 A 用户的播放动态推给 B 用户的连接。
func TestWebSocketIsolatesUsers(t *testing.T) {
	a, _ := newAPI(t)
	alice := wsDial(t, a, "/embywebsocket", testutil.NormalToken)
	bob := wsDial(t, a, "/embywebsocket", testutil.OtherToken)
	wsDrainHello(t, alice)
	wsDrainHello(t, bob)

	payload := []byte(`{"ItemId":"` + testutil.MovieID + `","PlaySessionId":"ws-isolate","PositionTicks":7}`)
	require.Equal(t, 204, a.post("/emby/Sessions/Playing/Progress", testutil.NormalToken, payload).Status)

	event := wsReadUntil(t, alice, "UserDataChanged", 3*time.Second)
	data, ok := event["Data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, testutil.NormalUserID, data["UserId"])
	wsExpectSilence(t, bob, 700*time.Millisecond)
}
