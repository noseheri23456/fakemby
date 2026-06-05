package service

import (
	"github.com/fakemby/fakemby/internal/database"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PlaybackService struct {
	db *gorm.DB
}

func NewPlaybackService(db *gorm.DB) *PlaybackService {
	return &PlaybackService{db: db}
}

// GetMediaSources 获取媒体项的所有播放源
func (s *PlaybackService) GetMediaSources(itemID string) ([]database.MediaSource, error) {
	var sources []database.MediaSource
	if err := s.db.Where("item_id = ?", itemID).Order("sort_order").Find(&sources).Error; err != nil {
		return nil, err
	}
	return sources, nil
}

// GeneratePlaySession 生成播放会话 ID
func (s *PlaybackService) GeneratePlaySession(userID, itemID string) string {
	// 简单实现：使用 UUID
	// 实际应用中可能需要存储会话信息用于进度跟踪
	return uuid.New().String()
}

// UpdatePlayProgress 更新播放进度（去抖缓冲版本）
func (s *PlaybackService) UpdatePlayProgress(userID, itemID string, positionTicks int64) error {
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

// MarkAsPlayed 标记为已看
func (s *PlaybackService) MarkAsPlayed(userID, itemID string) error {
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

// UnmarkAsPlayed 取消已看标记
func (s *PlaybackService) UnmarkAsPlayed(userID, itemID string) error {
	return s.db.Where("user_id = ? AND item_id = ?", userID, itemID).
		Update("is_played", false).Error
}

// MarkAsFavorite 标记为收藏
func (s *PlaybackService) MarkAsFavorite(userID, itemID string) error {
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

// UnmarkAsFavorite 取消收藏
func (s *PlaybackService) UnmarkAsFavorite(userID, itemID string) error {
	return s.db.Where("user_id = ? AND item_id = ?", userID, itemID).
		Update("is_favorite", false).Error
}

// GetPlayProgress 获取播放进度
func (s *PlaybackService) GetPlayProgress(userID, itemID string) (*database.PlayProgress, error) {
	var progress database.PlayProgress
	if err := s.db.Where("user_id = ? AND item_id = ?", userID, itemID).First(&progress).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &progress, nil
}

// GetResumeItems 获取继续观看列表
func (s *PlaybackService) GetResumeItems(userID string, limit int) ([]database.MediaItem, error) {
	var items []database.MediaItem

	// 查询播放进度大于 0 且未标记已看的项目
	err := s.db.
		Joins("JOIN play_progress ON play_progress.item_id = media_items.id").
		Where("play_progress.user_id = ? AND play_progress.position_ticks > 0 AND play_progress.is_played = 0", userID).
		Order("play_progress.last_played DESC").
		Limit(limit).
		Find(&items).Error

	if err != nil {
		return nil, err
	}

	return items, nil
}
