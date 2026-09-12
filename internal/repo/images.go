package repo

import (
	"github.com/fakemby/fakemby/internal/database"
	"gorm.io/gorm"
)

type Images interface {
	Image(string, string, int) (database.Image, error)
	Images(string, string) ([]database.Image, error)
}
type GormImages struct{ db *gorm.DB }

func NewImages(db *gorm.DB) Images { return &GormImages{db} }
func (r *GormImages) Image(item, kind string, index int) (database.Image, error) {
	var row database.Image
	q := r.db.Where("item_id = ? AND type = ?", item, kind)
	if index >= 0 {
		q = q.Where("idx = ?", index)
	}
	err := q.Order("idx, id").First(&row).Error
	return row, err
}
func (r *GormImages) Images(item, kind string) ([]database.Image, error) {
	rows := []database.Image{}
	err := r.db.Where("item_id = ? AND type = ?", item, kind).Order("idx, id").Find(&rows).Error
	return rows, err
}
