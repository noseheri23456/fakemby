package service

import (
	"log/slog"
	"time"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AuthService struct {
	db *gorm.DB
}

func NewAuthService(db *gorm.DB) *AuthService {
	return &AuthService{db: db}
}

// VerifyPassword 验证用户密码
func (s *AuthService) VerifyPassword(username, password string) (*database.User, error) {
	var user database.User
	if err := s.db.Where("name = ?", username).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if !database.CheckPassword(user.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}

	return &user, nil
}

// GenerateToken 生成认证令牌
func (s *AuthService) GenerateToken(userID, deviceID, deviceName, client, version string) (string, error) {
	token := uuid.New().String()
	t := &database.Token{
		Token:      token,
		UserID:     userID,
		DeviceID:   deviceID,
		DeviceName: deviceName,
		Client:     client,
		Version:    version,
	}

	if err := s.db.Create(t).Error; err != nil {
		slog.Error("创建 Token 失败", "error", err)
		return "", err
	}

	return token, nil
}

// VerifyToken 验证令牌有效性
func (s *AuthService) VerifyToken(token string, expiryDays int) (*database.Token, error) {
	var t database.Token
	if err := s.db.Where("token = ?", token).First(&t).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			slog.Error("Token 未找到", "token_prefix", tokenPrefix(token))
			return nil, ErrInvalidToken
		}
		slog.Error("查询 Token 失败", "error", err)
		return nil, err
	}

	slog.Debug("找到 Token", "userID", t.UserID, "createdAt", t.CreatedAt)

	// 检查令牌是否过期
	cutoffTime := time.Now().AddDate(0, 0, -expiryDays)
	if t.CreatedAt.Before(cutoffTime) {
		slog.Warn("Token 已过期", "token_prefix", tokenPrefix(token), "createdAt", t.CreatedAt, "cutoffTime", cutoffTime)
		// 删除过期令牌
		s.db.Delete(&t)
		return nil, ErrTokenExpired
	}

	return &t, nil
}

// RevokeToken 撤销令牌
func (s *AuthService) RevokeToken(token string) error {
	return s.db.Where("token = ?", token).Delete(&database.Token{}).Error
}

// FindValidToken 查找该用户已有的、仍在有效期内的 token（按 client/deviceName 定位）。
// 用于 Basic Auth 等「每次请求都带凭据」的场景：复用已有 token 而不是每请求铸造新 token，
// 避免 tokens 表无限增长（A5）。
func (s *AuthService) FindValidToken(userID, client, deviceName string, expiryDays int) (*database.Token, error) {
	cutoff := time.Now().AddDate(0, 0, -expiryDays)
	var t database.Token
	if err := s.db.Where("user_id = ? AND client = ? AND device_name = ? AND created_at >= ?",
		userID, client, deviceName, cutoff).
		Order("created_at desc").First(&t).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// ChangePassword 修改用户口令，并清除「必须改密」标记（A4）。
func (s *AuthService) ChangePassword(userID, newPassword string) error {
	hash, err := database.HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.db.Model(&database.User{}).
		Where("id = ?", userID).
		Updates(map[string]interface{}{
			"password_hash":        hash,
			"must_change_password": false,
		}).Error
}

// GetUserByID 根据 ID 获取用户
func (s *AuthService) GetUserByID(userID string) (*database.User, error) {
	var user database.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

// Error definitions
var (
	ErrInvalidCredentials = &AuthError{Code: "INVALID_CREDENTIALS", Message: "Invalid username or password"}
	ErrInvalidToken       = &AuthError{Code: "INVALID_TOKEN", Message: "Invalid or missing token"}
	ErrTokenExpired       = &AuthError{Code: "TOKEN_EXPIRED", Message: "Token has expired"}
	ErrUserNotFound       = &AuthError{Code: "USER_NOT_FOUND", Message: "User not found"}
)

// tokenPrefix 取 token 前若干位用于日志定位。
// 不能写成 token[:16]：客户端可能传任意短字符串（甚至 1 个字符），会直接越界 panic。
func tokenPrefix(token string) string {
	if len(token) <= 16 {
		return "***"
	}
	return token[:16] + "..."
}

type AuthError struct {
	Code    string
	Message string
}

func (e *AuthError) Error() string {
	return e.Message
}
