package emby_test

import (
	"encoding/json"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRoadmapClientSequence(t *testing.T) {
	a, _ := newAPI(t)
	for _, path := range []string{"/emby/Shows/NextUp?UserId=" + testutil.NormalUserID, "/emby/Items/Filters", "/emby/Channels", "/emby/Genres", "/emby/Studios", "/emby/Playlists", "/emby/Collections", "/emby/Items/" + testutil.MovieID + "/Intros", "/emby/Videos/" + testutil.MovieID + "/AdditionalParts"} {
		t.Run(path, func(t *testing.T) {
			require.Equal(t, 401, a.get(path, "").Status)
			r := a.get(path, testutil.NormalToken)
			require.Equal(t, 200, r.Status, string(r.Body))
			if !strings.Contains(path, "Filters") {
				assert.NotNil(t, r.Array(t, "Items"))
			}
		})
	}
	assert.Equal(t, "false", string(a.get("/emby/QuickConnect/Enabled", "").Body))
	r := a.get("/emby/Items/"+testutil.MovieID+"/SpecialFeatures", testutil.NormalToken)
	assert.JSONEq(t, "[]", string(r.Body))
	r = a.get("/emby/Users/"+testutil.NormalUserID+"/Items/"+testutil.MovieID, testutil.NormalToken)
	for _, field := range []string{"Genres", "Studios", "People", "Tags", "Taglines", "GenreItems", "MediaSources", "ImageTags", "ProviderIds", "BackdropImageTags", "LockedFields", "ExternalUrls"} {
		assert.Contains(t, r.JSON(t), field)
		assert.NotNil(t, r.JSON(t)[field], field)
	}
}
func TestRoadmapPolicyCoversListsAndDirectPaths(t *testing.T) {
	a, env := newAPI(t)
	require.NoError(t, env.DB.Model(&database.User{}).Where("id = ?", testutil.NormalUserID).Update("policy", `{"EnableAllFolders":false,"EnabledFolders":["lib-shows"]}`).Error)
	views := a.get("/emby/Users/"+testutil.NormalUserID+"/Views", testutil.NormalToken)
	require.Len(t, views.Array(t, "Items"), 1)
	list := a.get("/emby/Items?Recursive=true", testutil.NormalToken)
	require.Equal(t, 200, list.Status)
	for _, v := range list.Array(t, "Items") {
		assert.NotEqual(t, testutil.MovieID, v.(map[string]any)["Id"])
	}
	for _, p := range []string{"/emby/Users/" + testutil.NormalUserID + "/Items/" + testutil.MovieID, "/emby/Items/" + testutil.MovieID + "/PlaybackInfo", "/emby/Items/" + testutil.MovieID + "/Images/Primary", "/emby/Videos/" + testutil.MovieID + "/stream"} {
		assert.Equal(t, 403, a.get(p, testutil.NormalToken).Status, p)
	}
	assert.Equal(t, 403, a.get("/emby/Shows/NextUp?UserId="+testutil.OtherUserID, testutil.NormalToken).Status)
	require.NoError(t, env.DB.Model(&database.User{}).Where("id = ?", testutil.NormalUserID).Update("policy", `{"EnableAllFolders":"oops"}`).Error)
	assert.Equal(t, 403, a.get("/emby/Items?Recursive=true", testutil.NormalToken).Status)
}
func TestRoadmapDurableProgressPreservesFlags(t *testing.T) {
	a, env := newAPI(t)
	require.NoError(t, env.DB.Model(&database.PlayProgress{}).Where("user_id = ? AND item_id = ?", testutil.NormalUserID, testutil.MovieID).Updates(map[string]any{"is_favorite": true, "is_played": true, "play_count": 5}).Error)
	payload := []byte(`{"ItemId":"` + testutil.MovieID + `","PlaySessionId":"durable-session","PositionTicks":999}`)
	require.Equal(t, 204, a.post("/emby/Sessions/Playing/Progress", testutil.NormalToken, payload).Status)
	var p database.PlayProgress
	require.NoError(t, env.DB.Where("user_id = ? AND item_id = ?", testutil.NormalUserID, testutil.MovieID).First(&p).Error)
	assert.Equal(t, int64(999), p.PositionTicks)
	assert.True(t, p.IsFavorite)
	assert.True(t, p.IsPlayed)
	assert.Equal(t, 5, p.PlayCount)
	require.NoError(t, env.DB.Model(&database.User{}).Where("id = ?", testutil.NormalUserID).Update("policy", `{"SimultaneousStreamLimit":1}`).Error)
	second := []byte(`{"ItemId":"` + testutil.MovieID + `","PlaySessionId":"second-session","PositionTicks":111}`)
	assert.Equal(t, 429, a.post("/emby/Sessions/Playing", testutil.NormalToken, second).Status)
	require.Equal(t, 204, a.post("/emby/Sessions/Playing/Stopped", testutil.NormalToken, payload).Status)
}
func TestRoadmapOperationsAndAdminShell(t *testing.T) {
	a, _ := newAPI(t)
	for _, p := range []string{"/healthz", "/readyz", "/admin/", "/admin/app.js", "/admin/styles.css"} {
		r := a.get(p, "")
		assert.Equal(t, 200, r.Status, p)
		assert.NotEmpty(t, r.Header.Get("X-Request-ID"))
	}
	assert.Equal(t, 401, a.get("/metrics", "").Status)
	r := a.do("GET", "/metrics", map[string]string{"X-Api-Key": testutil.TestAdminAPIKey}, nil)
	assert.Equal(t, 200, r.Status)
	assert.Contains(t, string(r.Body), "fakemby_http_requests_total")
	assert.NotContains(t, string(r.Body), testutil.NormalToken)
}
func TestRoadmapWebSocketReceivesOwnEvents(t *testing.T) {
	a, _ := newAPI(t)
	raw := "ws" + strings.TrimPrefix(a.server.URL, "http") + "/embysocket"
	conn, resp, err := websocket.DefaultDialer.Dial(raw, http.Header{"X-Emby-Token": []string{testutil.NormalToken}})
	require.NoError(t, err)
	defer conn.Close()
	require.Equal(t, 101, resp.StatusCode)
	payload := []byte(`{"ItemId":"` + testutil.MovieID + `","PlaySessionId":"ws-test","PositionTicks":333}`)
	require.Equal(t, 204, a.post("/emby/Sessions/Playing/Progress", testutil.NormalToken, payload).Status)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, data, err := conn.ReadMessage()
	require.NoError(t, err)
	var event map[string]any
	require.NoError(t, json.Unmarshal(data, &event))
	assert.Equal(t, "UserDataChanged", event["MessageType"])
	require.Equal(t, 204, a.post("/emby/Sessions/Playing/Stopped", testutil.NormalToken, payload).Status)
}
