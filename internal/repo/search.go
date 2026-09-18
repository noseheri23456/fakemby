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

// cond 是一段待拼进 WHERE 的条件（SQL 片段 + 绑定参数）。
type searchCond struct {
	sql  string
	args []interface{}
}

func (s *GormSearch) FindSimilarItems(itemID string, limit int) ([]database.MediaItem, error) {
	// 获取原项目
	var sourceItem database.MediaItem
	if err := s.db.Where("id = ?", itemID).First(&sourceItem).Error; err != nil {
		return nil, err
	}

	var genres []string
	if sourceItem.Genres != "" {
		_ = json.Unmarshal([]byte(sourceItem.Genres), &genres)
	}

	var genreCond, yearCond *searchCond
	if len(genres) > 0 {
		likePatterns := make([]string, 0, len(genres))
		values := make([]interface{}, 0, len(genres))
		for _, genre := range genres {
			likePatterns = append(likePatterns, "genres LIKE ?")
			values = append(values, "%"+genre+"%")
		}
		genreCond = &searchCond{sql: "(" + strings.Join(likePatterns, " OR ") + ")", args: values}
	}
	if sourceItem.Year != nil && *sourceItem.Year > 0 {
		yearCond = &searchCond{
			sql:  "year BETWEEN ? AND ?",
			args: []interface{}{*sourceItem.Year - 3, *sourceItem.Year + 3},
		}
	}

	// 逐级放宽回退：
	//   1) 同类型 + 同流派 + 相近年份
	//   2) 同类型 + 同流派
	//   3) 同类型 + 相近年份
	//   4) 同类型
	// 三个条件一起收紧时命中率极低（实测小样本库里电影"类似影片"恒为 0 条），
	// 详情页那一栏就整栏消失。宁可给弱相关的推荐，也不要给空集。
	tiers := make([][]*searchCond, 0, 4)
	if genreCond != nil && yearCond != nil {
		tiers = append(tiers, []*searchCond{genreCond, yearCond})
	}
	if genreCond != nil {
		tiers = append(tiers, []*searchCond{genreCond})
	}
	if yearCond != nil {
		tiers = append(tiers, []*searchCond{yearCond})
	}
	tiers = append(tiers, nil) // 兜底：只要求同类型

	// 严格条件命中数不够时继续往下一级补，直到凑够 limit 或用完全部级别。
	// 这样相关度高的排在前面，但栏位不会因为样本太少只剩一两条。
	items := make([]database.MediaItem, 0, limit)
	seen := map[string]bool{itemID: true}
	for _, conds := range tiers {
		query := s.db.Where("type = ?", sourceItem.Type).Where("id != ?", itemID)
		for _, cd := range conds {
			query = query.Where(cd.sql, cd.args...)
		}
		query = query.Order("name asc")
		if limit > 0 {
			query = query.Limit(limit)
		}
		rows := []database.MediaItem{}
		if err := query.Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			if seen[row.ID] {
				continue
			}
			seen[row.ID] = true
			items = append(items, row)
		}
		if limit > 0 && len(items) >= limit {
			items = items[:limit]
			break
		}
	}

	return items, nil
}
