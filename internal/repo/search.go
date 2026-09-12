package repo

import (
	"encoding/json"
	"github.com/fakemby/fakemby/internal/database"
	"gorm.io/gorm"
	"strings"
)

type Search interface {
	Candidates([]string) ([]database.MediaItem, error)
	FindSimilarItems(string, int) ([]database.MediaItem, error)
}
type GormSearch struct{ db *gorm.DB }

func NewSearch(db *gorm.DB) Search { return &GormSearch{db} }
func (r *GormSearch) Candidates(kinds []string) ([]database.MediaItem, error) {
	rows := []database.MediaItem{}
	q := r.db
	if len(kinds) > 0 {
		q = q.Where("type IN ?", kinds)
	}
	err := q.Find(&rows).Error
	return rows, err
}
func (s *GormSearch) FindSimilarItems(itemID string, limit int) ([]database.MediaItem, error) {
	// 获取原项目
	var sourceItem database.MediaItem
	if err := s.db.Where("id = ?", itemID).First(&sourceItem).Error; err != nil {
		return nil, err
	}

	var items []database.MediaItem

	// 查询同类型的项目
	query := s.db.Where("type = ?", sourceItem.Type).Where("id != ?", itemID)

	// 如果原项目有流派，优先匹配相同流派的项目
	if sourceItem.Genres != "" {
		var genres []string
		if err := json.Unmarshal([]byte(sourceItem.Genres), &genres); err == nil && len(genres) > 0 {
			// 构建 OR 条件，查询包含任何相同流派的项目
			likePatterns := make([]string, 0, len(genres))
			values := make([]interface{}, 0, len(genres))
			for _, genre := range genres {
				likePatterns = append(likePatterns, "genres LIKE ?")
				values = append(values, "%"+genre+"%")
			}

			if len(likePatterns) > 0 {
				query = query.Where(strings.Join(likePatterns, " OR "), values...)
			}
		}
	}

	// 如果有年份信息，也考虑相近年份
	if sourceItem.Year != nil && *sourceItem.Year > 0 {
		yearMin := *sourceItem.Year - 3
		yearMax := *sourceItem.Year + 3
		query = query.Where("year BETWEEN ? AND ?", yearMin, yearMax)
	}

	query = query.Order("name asc")
	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&items).Error; err != nil {
		return nil, err
	}

	return items, nil
}
