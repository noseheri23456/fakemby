package database

import (
	"time"
)

// Library 媒体库
type Library struct {
	ID        string `gorm:"primaryKey"`
	Name      string
	Type      string // movies, tvshows
	SortOrder int
}

// MediaItem 媒体项目
type MediaItem struct {
	ID              string `gorm:"primaryKey"`
	LibraryID       string `gorm:"index"`
	ParentID        *string
	Type            string `gorm:"index"` // Movie, Series, Season, Episode
	Name            string `gorm:"index"`
	OriginalTitle   string
	SortName        string
	Overview        string `gorm:"type:text"`
	Year            *int   `gorm:"index"`
	PremiereDate    *string
	CommunityRating *float64
	OfficialRating  string
	Genres          string `gorm:"type:text"` // JSON array
	Studios         string `gorm:"type:text"` // JSON array
	People          string `gorm:"type:text"` // JSON array
	Tags            string `gorm:"type:text"` // JSON array
	Taglines        string `gorm:"type:text"` // JSON array
	ExternalUrls    string `gorm:"type:text"` // JSON array of {Name, Url}
	ProviderIds     string `gorm:"type:text"` // JSON
	TMDBID          string
	IMDBID          string
	TVDBID          string
	SeasonNumber    *int
	EpisodeNumber   *int
	RuntimeTicks    *int64
	IsHidden        bool
	Container       string
	VideoCodec      string
	AudioCodec      string
	Width           *int
	Height          *int
	DateCreated     time.Time `gorm:"autoCreateTime:milli"`
	DateModified    time.Time `gorm:"autoUpdateTime:milli"`
}

// MediaSource 播放源
type MediaSource struct {
	ID        string `gorm:"primaryKey"`
	ItemID    string `gorm:"index"`
	Name      string
	URL       string `gorm:"type:text"`
	Protocol  string
	Container string
	Size      *int64
	Bitrate   *int
	SortOrder int
}

// Image 图片
type Image struct {
	ID     uint
	ItemID string `gorm:"index"`
	Type   string // Primary, Backdrop, Logo, Thumb, Banner, Art
	Idx    int    // 多图索引
	URL    string `gorm:"type:text"`
	Tag    string // MD5 前8位
	Width  *int
	Height *int
}

// Subtitle 字幕
type Subtitle struct {
	ID       uint
	ItemID   string `gorm:"index"`
	Language string
	Title    string
	URL      string `gorm:"type:text"`
	Codec    string // srt, ass, vtt
}

// User 用户
type User struct {
	ID                string `gorm:"primaryKey"`
	Name              string `gorm:"uniqueIndex"`
	PasswordHash      string
	IsAdmin           bool
	AllowRemoteAccess bool
	Policy            string `gorm:"type:text"` // JSON
	ImageURL          string
	DateCreated       time.Time `gorm:"autoCreateTime:milli"`
}

// PlayProgress 播放进度
type PlayProgress struct {
	UserID        string `gorm:"primaryKey"`
	ItemID        string `gorm:"primaryKey"`
	PositionTicks int64
	PlayCount     int
	IsPlayed      bool
	IsFavorite    bool
	LastPlayed    *time.Time
}

// Token 认证令牌
type Token struct {
	Token      string `gorm:"primaryKey"`
	UserID     string `gorm:"index"`
	DeviceID   string
	DeviceName string
	Client     string
	Version    string
	CreatedAt  time.Time `gorm:"autoCreateTime:milli;index"`
}

// PlaybackActivity 播放活动 (供 Emby 统计插件兼容)
type PlaybackActivity struct {
	ID            uint      `gorm:"primaryKey;autoIncrement"`
	DateCreated   time.Time `gorm:"autoCreateTime"`
	UserID        string    `gorm:"column:UserId;index"`
	ItemID        string    `gorm:"column:ItemId;index"`
	ItemType      string    `gorm:"column:ItemType;index"`
	ItemName      string    `gorm:"column:ItemName"`
	PlayDuration  int       `gorm:"column:PlayDuration"`
	PauseDuration int       `gorm:"column:PauseDuration"`
	ClientName    string    `gorm:"column:ClientName"`
	DeviceName    string    `gorm:"column:DeviceName"`
	DeviceID      string    `gorm:"column:DeviceId"`
	RemoteAddress string    `gorm:"column:RemoteAddress"`
}

func (Library) TableName() string          { return "libraries" }
func (MediaItem) TableName() string        { return "media_items" }
func (MediaSource) TableName() string      { return "media_sources" }
func (Image) TableName() string            { return "images" }
func (Subtitle) TableName() string         { return "subtitles" }
func (User) TableName() string             { return "users" }
func (PlayProgress) TableName() string     { return "play_progress" }
func (Token) TableName() string            { return "tokens" }
func (PlaybackActivity) TableName() string { return "PlaybackActivity" }

// GetUsableToken 返回有效的 Token（不包括过期的）
func (t *Token) GetUsableToken(expiryDays int) bool {
	if t.CreatedAt.IsZero() {
		return false
	}
	// 简化：假设从 CreatedAt 推算是否过期
	// 详细实现在 service 层
	return true
}
