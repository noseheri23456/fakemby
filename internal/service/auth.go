package service

import (
	"errors"
	"fmt"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/repo"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"time"
)

type AuthService struct{ repository repo.Auth }

// NewAuthService retains source compatibility; new callers can inject a domain repository.
func NewAuthService(db *gorm.DB) *AuthService               { return NewAuthServiceWithRepository(repo.NewAuth(db)) }
func NewAuthServiceWithRepository(r repo.Auth) *AuthService { return &AuthService{r} }
func (s *AuthService) VerifyPassword(name, password string) (*database.User, error) {
	u, err := s.repository.UserByName(name)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if !database.CheckPassword(u.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}
	return u, nil
}
func (s *AuthService) GenerateToken(user, device, name, client, version string) (string, error) {
	token := uuid.NewString()
	err := s.repository.CreateToken(&database.Token{Token: token, UserID: user, DeviceID: device, DeviceName: name, Client: client, Version: version, CreatedAt: time.Now()})
	if err != nil {
		return "", err
	}
	return token, nil
}
func (s *AuthService) VerifyToken(token string, days int) (*database.Token, error) {
	t, err := s.repository.Token(token)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	if days <= 0 {
		days = 30
	}
	if t.CreatedAt.Before(time.Now().AddDate(0, 0, -days)) {
		_ = s.repository.DeleteToken(token)
		return nil, ErrTokenExpired
	}
	return t, nil
}
func (s *AuthService) RevokeToken(token string) error { return s.repository.DeleteToken(token) }
func (s *AuthService) FindValidToken(user, client, device string, days int) (*database.Token, error) {
	if days <= 0 {
		days = 30
	}
	t, err := s.repository.FindToken(user, client, device, time.Now().AddDate(0, 0, -days))
	if errors.Is(err, repo.ErrNotFound) {
		return nil, nil
	}
	return t, err
}
func (s *AuthService) ChangePassword(user, password string) error {
	if len(password) < 8 || len(password) > 72 {
		return fmt.Errorf("password must be 8-72 bytes")
	}
	hash, err := database.HashPassword(password)
	if err != nil {
		return err
	}
	return s.repository.SetPassword(user, hash)
}
func (s *AuthService) GetUserByID(id string) (*database.User, error) {
	u, err := s.repository.UserByID(id)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, ErrUserNotFound
	}
	return u, err
}

var (
	ErrInvalidCredentials = &AuthError{"INVALID_CREDENTIALS", "Invalid username or password"}
	ErrInvalidToken       = &AuthError{"INVALID_TOKEN", "Invalid or missing token"}
	ErrTokenExpired       = &AuthError{"TOKEN_EXPIRED", "Token has expired"}
	ErrUserNotFound       = &AuthError{"USER_NOT_FOUND", "User not found"}
)

type AuthError struct{ Code, Message string }

func (e *AuthError) Error() string { return e.Message }
