package service_test

import (
	"bytes"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/repo"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

type authStub struct {
	repo.Auth
	record *database.Token
}

func (s authStub) Token(string) (*database.Token, error) { return s.record, nil }
func TestAuthRepositoryWithoutDatabase(t *testing.T) {
	svc := service.NewAuthServiceWithRepository(authStub{record: &database.Token{Token: "test", UserID: "u", CreatedAt: time.Now()}})
	token, err := svc.VerifyToken("test", 30)
	require.NoError(t, err)
	assert.Equal(t, "u", token.UserID)
}
func TestRoadmapPinyinAliasesAndHighlight(t *testing.T) {
	env := testutil.Setup(t)
	require.NoError(t, env.DB.Create(&database.MediaItem{ID: "pinyin", LibraryID: testutil.MovieLibID, Type: "Movie", Name: "流浪地球", OriginalTitle: "The Wandering Earth", Tags: `["科幻"]`}).Error)
	svc := service.NewSearchService(env.DB)
	for _, q := range []string{"liulangdiqiu", "lldq", "wandering", "流浪"} {
		rows, total, err := svc.SearchItems(q, nil, 0, 10)
		require.NoError(t, err)
		require.Greater(t, total, int64(0), q)
		assert.Equal(t, "pinyin", rows[0].ID)
	}
	assert.Equal(t, "&lt;script&gt;<mark>Earth</mark>", service.HighlightName("<script>Earth", "Earth"))
}
func TestRoadmapImageResizesAndRejectsOriginErrors(t *testing.T) {
	env := testutil.Setup(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.WriteHeader(503)
			return
		}
		_, _ = w.Write(testutil.ImageBytes())
	}))
	defer origin.Close()
	require.NoError(t, env.DB.Create(&database.Image{ItemID: "resize", Type: "Primary", URL: origin.URL + "/ok"}).Error)
	svc := service.NewImageService(env.DB, "proxy_cache", t.TempDir(), 1)
	path, err := svc.GetImage("resize", "Primary", 0, 64, 32)
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	assert.LessOrEqual(t, cfg.Width, 64)
	assert.LessOrEqual(t, cfg.Height, 32)
	require.NoError(t, env.DB.Model(&database.Image{}).Where("item_id = ?", "resize").Update("url", origin.URL+"/bad").Error)
	_, err = svc.GetImage("resize", "Primary", 0, 64, 32)
	assert.Error(t, err)
}
