// Package repo owns persistence contracts and GORM adapters.
package repo

import (
	"errors"
	"github.com/fakemby/fakemby/internal/database"
	"gorm.io/gorm"
	"time"
)

var ErrNotFound = errors.New("record not found")

type Auth interface {
	UserByName(string) (*database.User, error)
	UserByID(string) (*database.User, error)
	Token(string) (*database.Token, error)
	CreateToken(*database.Token) error
	DeleteToken(string) error
	FindToken(user, client, device string, cutoff time.Time) (*database.Token, error)
	SetPassword(user, hash string) error
}
type GormAuth struct{ read, write *gorm.DB }

func NewAuth(db *gorm.DB) Auth {
	write := db
	if db == database.Get() {
		write = database.GetWrite()
	}
	return &GormAuth{db, write}
}
func recordError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
func (r *GormAuth) UserByName(name string) (*database.User, error) {
	var u database.User
	err := r.read.Where("name = ?", name).First(&u).Error
	return &u, recordError(err)
}
func (r *GormAuth) UserByID(id string) (*database.User, error) {
	var u database.User
	err := r.read.Where("id = ?", id).First(&u).Error
	return &u, recordError(err)
}
func (r *GormAuth) Token(token string) (*database.Token, error) {
	var t database.Token
	err := r.read.Where("token = ?", token).First(&t).Error
	return &t, recordError(err)
}
func (r *GormAuth) CreateToken(t *database.Token) error { return r.write.Create(t).Error }
func (r *GormAuth) DeleteToken(token string) error {
	return r.write.Where("token = ?", token).Delete(&database.Token{}).Error
}
func (r *GormAuth) FindToken(user, client, device string, cutoff time.Time) (*database.Token, error) {
	var t database.Token
	err := r.read.Where("user_id = ? AND client = ? AND device_name = ? AND created_at >= ?", user, client, device, cutoff).Order("created_at DESC").First(&t).Error
	return &t, recordError(err)
}
func (r *GormAuth) SetPassword(user, hash string) error {
	return r.write.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&database.User{}).Where("id = ?", user).Updates(map[string]any{"password_hash": hash, "must_change_password": false})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return tx.Where("user_id = ?", user).Delete(&database.Token{}).Error
	})
}
