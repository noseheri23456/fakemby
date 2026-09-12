package service

import (
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/repo"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PlaybackService struct {
	repository repo.Playback
}

func NewPlaybackService(db *gorm.DB) *PlaybackService {
	return NewPlaybackServiceWithRepository(repo.NewPlayback(db))
}

// GetMediaSources 获取媒体项的所有播放源
func (s *PlaybackService) GetMediaSources(itemID string) ([]database.MediaSource, error) {
	return s.repository.GetMediaSources(itemID)
}

// MediaStreamBaseIndex 返回字幕流在 MediaStreams 数组中的起始全局下标。
//
// 视频流（若有，Index 0）与音频流（若有）占据前若干位，字幕流必须接在它们之后。
// PlaybackInfo 渲染字幕流下标 与 字幕流式端点按 Index 反查，必须共用同一套计算，
// 否则客户端拿到的字幕 Index 与 /Subtitles/:index/Stream 对不上（A10：多字幕/多音轨
// 场景下取到错误语言甚至 404）。
func MediaStreamBaseIndex(item *database.MediaItem) int {
	base := 0
	if item.VideoCodec != "" {
		base++
	}
	if item.AudioCodec != "" {
		base++
	}
	return base
}

// GeneratePlaySession 生成播放会话 ID
func (s *PlaybackService) GeneratePlaySession(userID, itemID string) string {
	// 简单实现：使用 UUID
	// 实际应用中可能需要存储会话信息用于进度跟踪
	return uuid.New().String()
}

// UpdatePlayProgress 更新播放进度（去抖缓冲版本）
func (s *PlaybackService) UpdatePlayProgress(userID, itemID string, positionTicks int64) error {
	return s.repository.UpdatePlayProgress(userID, itemID, positionTicks)
}

// MarkAsPlayed 标记为已看
func (s *PlaybackService) MarkAsPlayed(userID, itemID string) error {
	return s.repository.MarkAsPlayed(userID, itemID)
}

// UnmarkAsPlayed 取消已看标记。
// 没有可更新的记录（用户本来就没标记过）不算错误。
func (s *PlaybackService) UnmarkAsPlayed(userID, itemID string) error {
	return s.repository.UnmarkAsPlayed(userID, itemID)
}

// MarkAsFavorite 标记为收藏
func (s *PlaybackService) MarkAsFavorite(userID, itemID string) error {
	return s.repository.MarkAsFavorite(userID, itemID)
}

// UnmarkAsFavorite 取消收藏。
// 没有可更新的记录（用户本来就没收藏）不算错误。
func (s *PlaybackService) UnmarkAsFavorite(userID, itemID string) error {
	return s.repository.UnmarkAsFavorite(userID, itemID)
}

// toggleFlag 把 user_id+item_id 定位到的进度记录上的布尔字段置为指定值。
//
// 踩过的坑：直接 s.db.Where(...).Update(...) 而不指定 Model，gorm 拼出的 UPDATE
// 语句没有表名，执行必然报错；旧代码又把"RowsAffected == 0"当成成功吞掉，
// 导致"取消已看 / 取消收藏"静默失效——用户点了没反应，服务端还返回 200。

// GetPlayProgress 获取播放进度
func (s *PlaybackService) GetPlayProgress(userID, itemID string) (*database.PlayProgress, error) {
	return s.repository.GetPlayProgress(userID, itemID)
}

// GetResumeItems 获取继续观看列表
func (s *PlaybackService) GetResumeItems(userID string, limit int) ([]database.MediaItem, error) {
	return s.repository.GetResumeItems(userID, limit)
}

func NewPlaybackServiceWithRepository(r repo.Playback) *PlaybackService {
	return &PlaybackService{repository: r}
}
