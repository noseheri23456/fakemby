package emby_test

import (
	"testing"
	"time"

	"github.com/fakemby/fakemby/internal/emby"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProgressBufferFlushWritesToDB 验证：缓冲中的进度能被 FlushProgressNow 落库（A7）。
// 用 1 小时 flush 间隔，确保只有手动 flush 才会写库，排除后台 goroutine 干扰。
func TestProgressBufferFlushWritesToDB(t *testing.T) {
	env := testutil.Setup(t)
	emby.InitProgressBuffer(env.DB, time.Hour)
	defer emby.ShutdownProgressBuffer()

	emby.BufferProgress(testutil.NormalUserID, testutil.MovieID, 123456, true)
	emby.FlushProgressNow()

	svc := service.NewPlaybackService(env.DB)
	prog, err := svc.GetPlayProgress(testutil.NormalUserID, testutil.MovieID)
	require.NoError(t, err)
	require.NotNil(t, prog, "flush 后进度应已落库")
	assert.Equal(t, int64(123456), prog.PositionTicks)
}

// TestProgressFlushEndpoint 验证管理端点 /api/admin/progress/flush 能触发落库。
func TestProgressFlushEndpoint(t *testing.T) {
	a, env := newAPI(t)
	emby.InitProgressBuffer(env.DB, time.Hour)
	defer emby.ShutdownProgressBuffer()

	emby.BufferProgress(testutil.NormalUserID, testutil.MovieID, 654321, true)

	r := a.do("POST", "/api/admin/progress/flush",
		map[string]string{"X-Api-Key": testutil.TestAdminAPIKey}, nil)
	require.Equal(t, 204, r.Status)

	svc := service.NewPlaybackService(env.DB)
	prog, err := svc.GetPlayProgress(testutil.NormalUserID, testutil.MovieID)
	require.NoError(t, err)
	require.NotNil(t, prog)
	assert.Equal(t, int64(654321), prog.PositionTicks)
}
