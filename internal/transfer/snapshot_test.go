package transfer_test

import (
	"bytes"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/fakemby/fakemby/internal/transfer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSnapshotRoundTripAndNonDestructiveRestore(t *testing.T) {
	env := testutil.Setup(t)
	var b bytes.Buffer
	require.NoError(t, transfer.Export(env.DB, env.Cfg, &b))
	assert.NotContains(t, b.String(), testutil.TestAdminAPIKey)
	assert.NotContains(t, b.String(), testutil.TestSignKey)
	assert.NotContains(t, b.String(), testutil.NormalToken)
	target := testutil.NewTestDB(t)
	cfg, err := transfer.Import(target, bytes.NewReader(b.Bytes()))
	require.NoError(t, err)
	assert.Empty(t, cfg.Admin.APIKey)
	var items, users int64
	require.NoError(t, target.Model(&database.MediaItem{}).Count(&items).Error)
	require.NoError(t, target.Model(&database.User{}).Count(&users).Error)
	assert.Equal(t, int64(4), items)
	assert.Equal(t, int64(3), users)
	_, err = transfer.Import(target, bytes.NewReader(b.Bytes()))
	assert.Error(t, err)
	var after int64
	require.NoError(t, target.Model(&database.MediaItem{}).Count(&after).Error)
	assert.Equal(t, items, after)
}
func TestSnapshotInvalidVersionDoesNotWrite(t *testing.T) {
	db := testutil.NewTestDB(t)
	_, err := transfer.Import(db, bytes.NewBufferString(`{"version":99}`))
	require.Error(t, err)
	var n int64
	require.NoError(t, db.Model(&database.MediaItem{}).Count(&n).Error)
	assert.Zero(t, n)
}
