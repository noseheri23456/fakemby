package emby_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/emby"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func adminRequest(a api, method, path string, payload any) resp {
	a.t.Helper()
	var data []byte
	if payload != nil {
		var err error
		data, err = json.Marshal(payload)
		require.NoError(a.t, err)
	}
	return a.do(method, path, map[string]string{"X-Api-Key": testutil.TestAdminAPIKey}, data)
}

func importPayload(t *testing.T, a api, payload any) emby.ImportResponse {
	t.Helper()
	r := adminRequest(a, http.MethodPost, "/api/admin/import", payload)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var out emby.ImportResponse
	require.NoError(t, json.Unmarshal(r.Body, &out))
	return out
}

func TestImportStableIdentityAndOwnedArrays(t *testing.T) {
	a, env := newAPI(t)
	payload := map[string]any{"library": "Import movies", "items": []any{map[string]any{
		"name": "First title", "type": "Movie", "ProviderIds": map[string]string{"Tmdb": "101", "custom": "own-id"},
		"sources":   []any{map[string]any{"url": "https://media.example/one.mkv"}},
		"images":    map[string]string{"Primary": "https://media.example/one.jpg"},
		"subtitles": []any{map[string]any{"url": "https://media.example/one.srt", "codec": "srt"}},
	}}}
	first := importPayload(t, a, payload)
	require.Equal(t, 1, first.Imported)
	require.Empty(t, first.Errors)
	var row database.MediaItem
	require.NoError(t, env.DB.Where("name = ?", "First title").First(&row).Error)
	id, created := row.ID, row.DateCreated
	require.NoError(t, env.DB.Create(&database.PlayProgress{UserID: testutil.NormalUserID, ItemID: id, PositionTicks: 1234, IsFavorite: true, PlayCount: 3}).Error)
	var source database.MediaSource
	require.NoError(t, env.DB.Where("item_id = ?", id).First(&source).Error)
	sourceID := source.ID

	item := payload["items"].([]any)[0].(map[string]any)
	item["name"] = "Renamed title"
	item["ProviderIds"] = map[string]string{"CUSTOM": "own-id"}
	item["sources"] = []any{map[string]any{"url": "https://media.example/new.mkv"}}
	item["images"] = map[string]string{"Backdrop": "https://media.example/new.jpg"}
	item["subtitles"] = []any{map[string]any{"url": "https://media.example/new.vtt", "codec": "vtt"}}
	for range 2 {
		out := importPayload(t, a, payload)
		require.Equal(t, 1, out.Imported)
		require.Empty(t, out.Errors)
	}
	require.NoError(t, env.DB.Where("id = ?", id).First(&row).Error)
	assert.Equal(t, "Renamed title", row.Name)
	assert.Equal(t, created, row.DateCreated)
	assert.Equal(t, "101", row.TMDBID)
	var progress database.PlayProgress
	require.NoError(t, env.DB.Where("item_id = ?", id).First(&progress).Error)
	assert.EqualValues(t, 1234, progress.PositionTicks)
	assert.True(t, progress.IsFavorite)
	assert.Equal(t, 3, progress.PlayCount)
	for _, model := range []any{&database.MediaItem{}, &database.MediaSource{}, &database.Image{}, &database.Subtitle{}} {
		var count int64
		column := "item_id"
		if _, ok := model.(*database.MediaItem); ok {
			column = "id"
		}
		require.NoError(t, env.DB.Model(model).Where(column+" = ?", id).Count(&count).Error)
		assert.EqualValues(t, 1, count)
	}
	require.NoError(t, env.DB.Where("item_id = ?", id).First(&source).Error)
	assert.Equal(t, sourceID, source.ID)
	assert.Equal(t, "https://media.example/new.mkv", source.URL)
	delete(item, "sources")
	item["images"], item["subtitles"] = map[string]string{}, []any{}
	require.Empty(t, importPayload(t, a, payload).Errors)
	for _, model := range []any{&database.MediaSource{}, &database.Image{}, &database.Subtitle{}} {
		var count int64
		require.NoError(t, env.DB.Model(model).Where("item_id = ?", id).Count(&count).Error)
		assert.Zero(t, count)
	}
}

func TestImportRecursiveStableIDsAndScope(t *testing.T) {
	a, env := newAPI(t)
	item := map[string]any{"type": "Series", "name": "Show", "ProviderIds": map[string]string{"tvdb": "900"},
		"seasons": []any{map[string]any{"season_number": 1, "episodes": []any{
			map[string]any{"name": "Pilot", "episode_number": 1, "ProviderIds": map[string]string{"custom": "ep"}, "sources": []any{map[string]any{"url": "https://media.example/ep.mkv"}}},
		}}}}
	payload := map[string]any{"library": "Import TV", "items": []any{item}}
	require.Empty(t, importPayload(t, a, payload).Errors)
	var lib database.Library
	require.NoError(t, env.DB.Where("name = ?", "Import TV").First(&lib).Error)
	var before []database.MediaItem
	require.NoError(t, env.DB.Where("library_id = ?", lib.ID).Order("id").Find(&before).Error)
	require.Len(t, before, 3)
	for _, row := range before {
		require.NoError(t, env.DB.Create(&database.PlayProgress{UserID: testutil.NormalUserID, ItemID: row.ID, PositionTicks: 99}).Error)
	}
	item["name"] = "Show renamed"
	require.Empty(t, importPayload(t, a, payload).Errors)
	var after []database.MediaItem
	require.NoError(t, env.DB.Where("library_id = ?", lib.ID).Order("id").Find(&after).Error)
	require.Len(t, after, 3)
	for i := range before {
		assert.Equal(t, before[i].ID, after[i].ID)
		assert.Equal(t, before[i].ParentID, after[i].ParentID)
	}
	// Provider IDs must not merge across libraries or types.
	payload["library"] = "Other TV"
	require.Empty(t, importPayload(t, a, payload).Errors)
	payload["library"] = "Import TV"
	payload["items"] = []any{map[string]any{"name": "Movie", "type": "Movie", "ProviderIds": map[string]string{"tvdb": "900"}}}
	require.Empty(t, importPayload(t, a, payload).Errors)
	var count int64
	require.NoError(t, env.DB.Model(&database.MediaItem{}).Where("library_id = ?", lib.ID).Count(&count).Error)
	assert.EqualValues(t, 4, count)
}

func TestImportExplicitIdentityAndAmbiguity(t *testing.T) {
	a, env := newAPI(t)
	payload := map[string]any{"library": "Identity", "items": []any{
		map[string]any{"Id": "owned-a", "name": "A", "type": "Movie", "ProviderIds": map[string]string{"tmdb": "a"}},
		map[string]any{"Id": "owned-b", "name": "B", "type": "Movie", "ProviderIds": map[string]string{"imdb": "b"}},
	}}
	require.Equal(t, 2, importPayload(t, a, payload).Imported)
	payload["items"] = []any{map[string]any{"Id": "owned-a", "name": "Updated", "type": "Movie"}}
	require.Empty(t, importPayload(t, a, payload).Errors)
	var row database.MediaItem
	require.NoError(t, env.DB.Where("id = ?", "owned-a").First(&row).Error)
	assert.Equal(t, "Updated", row.Name)
	payload["items"] = []any{map[string]any{"name": "Conflict", "type": "Movie", "ProviderIds": map[string]string{"tmdb": "a", "imdb": "b"}}}
	out := importPayload(t, a, payload)
	assert.Zero(t, out.Imported)
	require.Len(t, out.Errors, 1)
	payload["library"] = "Foreign"
	payload["items"] = []any{map[string]any{"Id": "owned-a", "name": "Hijack", "type": "Movie"}}
	out = importPayload(t, a, payload)
	assert.Zero(t, out.Imported)
	require.Len(t, out.Errors, 1)
	var count int64
	require.NoError(t, env.DB.Model(&database.Library{}).Where("name = ?", "Foreign").Count(&count).Error)
	assert.Zero(t, count, "failed import must not leave an empty new library")
}

func TestImportDryRunAndValidationNeverWrite(t *testing.T) {
	a, env := newAPI(t)
	var writes atomic.Int64
	require.NoError(t, env.DB.Callback().Create().Before("gorm:create").Register("test:no_create", func(*gorm.DB) { writes.Add(1) }))
	require.NoError(t, env.DB.Callback().Update().Before("gorm:update").Register("test:no_update", func(*gorm.DB) { writes.Add(1) }))
	require.NoError(t, env.DB.Callback().Delete().Before("gorm:delete").Register("test:no_delete", func(*gorm.DB) { writes.Add(1) }))
	for _, path := range []string{"/api/admin/import", "/api/admin/import?dry_run=true", "/api/admin/import?dry_run=false"} {
		r := adminRequest(a, http.MethodPost, path, map[string]any{"library": "Preview", "dry_run": true, "items": []any{
			map[string]any{"name": "Valid", "type": "Movie"},
			map[string]any{"name": "Bad", "type": "Movie", "sources": []any{map[string]any{"url": "javascript:alert(1)"}}},
		}})
		require.Equal(t, http.StatusOK, r.Status)
		assert.Equal(t, float64(0), r.JSON(t)["imported"])
		assert.Equal(t, float64(1), r.JSON(t)["would_import"])
		assert.Len(t, r.Array(t, "errors"), 1)
	}
	bad := map[string]any{"library": "Invalid", "items": []any{map[string]any{"name": "Bad tree", "type": "Series", "seasons": []any{
		map[string]any{"season_number": 1, "episodes": []any{map[string]any{"name": "Bad child", "episode_number": 1, "sources": []any{map[string]any{"url": "file:///secret"}}}}},
	}}}}
	assert.Zero(t, importPayload(t, a, bad).Imported)
	assert.Zero(t, writes.Load(), "validation/dry run must never issue writes, even rolled-back writes")
	for _, body := range []string{`null`, `{}`, `{"library":"x","items":[{"name":"x","type":"Movie"}]} {}`, `{"library":"x","items":[]}`} {
		r := a.do(http.MethodPost, "/api/admin/import", map[string]string{"X-Api-Key": testutil.TestAdminAPIKey}, []byte(body))
		assert.Equal(t, http.StatusBadRequest, r.Status)
	}
	oversized := strings.Repeat(" ", 8<<20) + `{}`
	r := a.do(http.MethodPost, "/api/admin/import", map[string]string{"X-Api-Key": testutil.TestAdminAPIKey}, []byte(oversized))
	assert.Equal(t, http.StatusBadRequest, r.Status)
	items := make([]any, 1001)
	for i := range items {
		items[i] = map[string]any{"name": fmt.Sprint(i), "type": "Movie"}
	}
	r = adminRequest(a, http.MethodPost, "/api/admin/import", map[string]any{"library": "Huge", "dry_run": true, "items": items})
	assert.Equal(t, http.StatusBadRequest, r.Status)
	assert.Zero(t, writes.Load())
}

func TestImportSavepointRollsBackWholeTreeAndArrays(t *testing.T) {
	a, env := newAPI(t)
	initial := map[string]any{"library": "Atomic", "items": []any{map[string]any{"Id": "atomic-root", "name": "Original", "type": "Movie", "sources": []any{map[string]any{"url": "https://media.example/original.mkv"}}}}}
	require.Empty(t, importPayload(t, a, initial).Errors)
	var failedCreates atomic.Int64
	require.NoError(t, env.DB.Callback().Create().Before("gorm:create").Register("test:fail_subtitle", func(tx *gorm.DB) {
		if tx.Statement.Table == "subtitles" {
			failedCreates.Add(1)
			tx.AddError(errors.New("injected permanent subtitle failure"))
		}
	}))
	payload := map[string]any{"library": "Atomic", "items": []any{
		map[string]any{"Id": "atomic-root", "name": "Must roll back", "type": "Movie", "genres": []string{"Rollback genre"}, "sources": []any{map[string]any{"url": "https://media.example/replaced.mkv"}}, "subtitles": []any{map[string]any{"url": "https://media.example/fail.srt"}}},
		map[string]any{"Id": "atomic-series", "name": "Must roll back tree", "type": "Series", "seasons": []any{map[string]any{"season_number": 1, "episodes": []any{map[string]any{"name": "Episode", "episode_number": 1, "subtitles": []any{map[string]any{"url": "https://media.example/fail.srt"}}}}}}},
		map[string]any{"Id": "atomic-good", "name": "Good", "type": "Movie"},
	}}
	out := importPayload(t, a, payload)
	assert.Equal(t, 1, out.Imported)
	require.Len(t, out.Errors, 2)
	assert.EqualValues(t, 2, failedCreates.Load(), "permanent failures must not retry")
	var row database.MediaItem
	require.NoError(t, env.DB.Where("id = ?", "atomic-root").First(&row).Error)
	assert.Equal(t, "Original", row.Name)
	var source database.MediaSource
	require.NoError(t, env.DB.Where("item_id = ?", row.ID).First(&source).Error)
	assert.Equal(t, "https://media.example/original.mkv", source.URL)
	for _, name := range []string{"Must roll back tree", "Season 1", "Episode", "Rollback genre"} {
		var count int64
		require.NoError(t, env.DB.Model(&database.MediaItem{}).Where("name = ?", name).Count(&count).Error)
		assert.Zero(t, count, name)
	}
}

type importTestLock struct{}

func (importTestLock) Error() string { return "injected SQLITE_BUSY" }
func (importTestLock) Code() int     { return 5 }

func TestImportRetriesOnlyTransientLocks(t *testing.T) {
	a, env := newAPI(t)
	var attempts atomic.Int64
	require.NoError(t, env.DB.Callback().Create().Before("gorm:create").Register("test:transient_source", func(tx *gorm.DB) {
		if tx.Statement.Table == "media_sources" && attempts.Add(1) == 1 {
			tx.AddError(importTestLock{})
		}
	}))
	out := importPayload(t, a, map[string]any{"library": "Retry", "items": []any{map[string]any{
		"Id": "retry-item", "name": "Retry", "type": "Movie", "sources": []any{map[string]any{"url": "https://media.example/retry.mkv"}},
	}}})
	assert.Equal(t, 1, out.Imported)
	assert.Empty(t, out.Errors)
	assert.EqualValues(t, 2, attempts.Load())
	var count int64
	require.NoError(t, env.DB.Model(&database.MediaSource{}).Where("item_id = ?", "retry-item").Count(&count).Error)
	assert.EqualValues(t, 1, count)
}
