package service_test

import (
	"testing"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/service"
	"github.com/fakemby/fakemby/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyPassword(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewAuthService(env.DB)

	user, err := svc.VerifyPassword(testutil.AdminUserName, testutil.Password)
	require.NoError(t, err)
	assert.Equal(t, testutil.AdminUserID, user.ID)
	assert.True(t, user.IsAdmin)
}

func TestVerifyPasswordRejectsWrongOrUnknownUser(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewAuthService(env.DB)

	_, err := svc.VerifyPassword(testutil.AdminUserName, "definitely-wrong")
	assert.ErrorIs(t, err, service.ErrInvalidCredentials)

	_, err = svc.VerifyPassword("no-such-user", testutil.Password)
	assert.ErrorIs(t, err, service.ErrInvalidCredentials)
}

func TestGenerateAndVerifyToken(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewAuthService(env.DB)

	token, err := svc.GenerateToken(testutil.NormalUserID, "dev-1", "Phone", "SenPlayer", "1.0")
	require.NoError(t, err)
	require.NotEmpty(t, token)

	got, err := svc.VerifyToken(token, 30)
	require.NoError(t, err)
	assert.Equal(t, testutil.NormalUserID, got.UserID)
	assert.Equal(t, "dev-1", got.DeviceID)
	assert.Equal(t, "SenPlayer", got.Client)
}

func TestVerifyTokenRejectsUnknownAndShortToken(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewAuthService(env.DB)

	_, err := svc.VerifyToken(testutil.NormalToken+"-not-exist", 30)
	assert.ErrorIs(t, err, service.ErrInvalidToken)

	// 回归：历史上这里写的是 token[:16]，短于 16 字符的 token 会直接越界 panic。
	// 客户端完全可能传任意短串，必须安全返回 401 而不是崩溃。
	_, err = svc.VerifyToken("x", 30)
	assert.ErrorIs(t, err, service.ErrInvalidToken)
}

func TestVerifyTokenExpired(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewAuthService(env.DB)

	_, err := svc.VerifyToken(testutil.ExpiredToken, 30)
	assert.ErrorIs(t, err, service.ErrTokenExpired)

	// 过期 token 应被顺手清理，下次查询变成"不存在"
	var remains int64
	env.DB.Model(&database.Token{}).Where("token = ?", testutil.ExpiredToken).Count(&remains)
	assert.Zero(t, remains, "过期 token 应从库中删除")
}

func TestVerifyTokenRespectsExpiryDays(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewAuthService(env.DB)

	// 同一条 40 天前的 token：1 天有效期必过期，365 天有效期仍可用
	_, err := svc.VerifyToken(testutil.ExpiredToken, 1)
	assert.ErrorIs(t, err, service.ErrTokenExpired)

	env2 := testutil.Setup(t) // 重新灌种子（上一条已被删除）
	svc2 := service.NewAuthService(env2.DB)
	_, err = svc2.VerifyToken(testutil.ExpiredToken, 365)
	require.NoError(t, err)
}

func TestRevokeToken(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewAuthService(env.DB)

	require.NoError(t, svc.RevokeToken(testutil.NormalToken))
	_, err := svc.VerifyToken(testutil.NormalToken, 30)
	assert.ErrorIs(t, err, service.ErrInvalidToken)
}

func TestGetUserByID(t *testing.T) {
	env := testutil.Setup(t)
	svc := service.NewAuthService(env.DB)

	user, err := svc.GetUserByID(testutil.OtherUserID)
	require.NoError(t, err)
	assert.Equal(t, testutil.OtherUserName, user.Name)
	assert.False(t, user.IsAdmin)

	_, err = svc.GetUserByID("missing")
	assert.ErrorIs(t, err, service.ErrUserNotFound)
}
