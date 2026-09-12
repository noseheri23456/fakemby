package repo

import (
	"github.com/fakemby/fakemby/internal/database"
	"gorm.io/gorm"
)

type Playback interface {
	GetMediaSources(itemID string) ([]database.MediaSource, error)
	UpdatePlayProgress(userID, itemID string, positionTicks int64) error
	MarkAsPlayed(userID, itemID string) error
	UnmarkAsPlayed(userID, itemID string) error
	MarkAsFavorite(userID, itemID string) error
	UnmarkAsFavorite(userID, itemID string) error
	GetPlayProgress(userID, itemID string) (*database.PlayProgress, error)
	GetResumeItems(userID string, limit int) ([]database.MediaItem, error)
}
type GormPlayback struct{ db *gorm.DB }

func NewPlayback(db *gorm.DB) Playback {
	if db == database.Get() {
		db = database.GetWrite()
	}
	return &GormPlayback{db}
}
func (s *GormPlayback) GetMediaSources(itemID string) ([]database.MediaSource, error) {
	var sources []database.MediaSource
	if err := s.db.Where("item_id = ?", itemID).Order("sort_order").Find(&sources).Error; err != nil {
		return nil, err
	}
	return sources, nil
}
func (s *GormPlayback) UpdatePlayProgress(userID, itemID string, positionTicks int64) error {
	// 这个方法会由进度缓冲系统调用
	// 直接更新数据库
	var progress database.PlayProgress
	if err := s.db.Where("user_id = ? AND item_id = ?", userID, itemID).First(&progress).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// 创建新的进度记录
			progress = database.PlayProgress{
				UserID:        userID,
				ItemID:        itemID,
				PositionTicks: positionTicks,
			}
			return s.db.Create(&progress).Error
		}
		return err
	}

	// 更新现有进度
	return s.db.Model(&progress).Update("position_ticks", positionTicks).Error
}
func (s *GormPlayback) MarkAsPlayed(userID, itemID string) error {
	var progress database.PlayProgress
	if err := s.db.Where("user_id = ? AND item_id = ?", userID, itemID).First(&progress).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// 创建新的进度记录
			progress = database.PlayProgress{
				UserID:    userID,
				ItemID:    itemID,
				IsPlayed:  true,
				PlayCount: 1,
			}
			return s.db.Create(&progress).Error
		}
		return err
	}

	// 更新现有记录
	return s.db.Model(&progress).
		Update("is_played", true).
		Update("play_count", gorm.Expr("play_count + ?", 1)).
		Error
}
func (s *GormPlayback) UnmarkAsPlayed(userID, itemID string) error {
	return s.toggleFlag(userID, itemID, "is_played", false)
}
func (s *GormPlayback) MarkAsFavorite(userID, itemID string) error {
	var progress database.PlayProgress
	if err := s.db.Where("user_id = ? AND item_id = ?", userID, itemID).First(&progress).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// 创建新的进度记录
			progress = database.PlayProgress{
				UserID:     userID,
				ItemID:     itemID,
				IsFavorite: true,
			}
			return s.db.Create(&progress).Error
		}
		return err
	}

	// 更新现有记录
	return s.db.Model(&progress).Update("is_favorite", true).Error
}
func (s *GormPlayback) UnmarkAsFavorite(userID, itemID string) error {
	return s.toggleFlag(userID, itemID, "is_favorite", false)
}
func (s *GormPlayback) toggleFlag(userID, itemID, column string, value bool) error {
	result := s.db.Model(&database.PlayProgress{}).
		Where("user_id = ? AND item_id = ?", userID, itemID).
		Update(column, value)
	if result.Error != nil {
		return result.Error
	}
	return nil
}
func (s *GormPlayback) GetPlayProgress(userID, itemID string) (*database.PlayProgress, error) {
	var progress database.PlayProgress
	if err := s.db.Where("user_id = ? AND item_id = ?", userID, itemID).First(&progress).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &progress, nil
}
func (s *GormPlayback) GetResumeItems(userID string, limit int) ([]database.MediaItem, error) {
	var items []database.MediaItem

	// 查询播放进度大于 0 且未标记已看的项目
	err := s.db.
		Joins("JOIN play_progress ON play_progress.item_id = media_items.id").
		Where("play_progress.user_id = ? AND play_progress.position_ticks > 0 AND play_progress.is_played = false", userID).
		Order("play_progress.last_played DESC").
		Limit(limit).
		Find(&items).Error

	if err != nil {
		return nil, err
	}

	return items, nil
}
