package types

// BaseItemDto Emby 标准媒体项目 DTO
type BaseItemDto struct {
	// 基础字段
	ID              string `json:"Id"`
	Name            string `json:"Name"`
	Type            string `json:"Type"` // Movie, Series, Season, Episode, Folder, etc.
	IsFolder        bool   `json:"IsFolder"`
	MediaType       string `json:"MediaType"` // Video, Audio

	// 详情字段
	Overview        string `json:"Overview,omitempty"`
	SortName        string `json:"SortName,omitempty"`
	RunTimeTicks    *int64 `json:"RunTimeTicks,omitempty"` // 100-nanosecond ticks
	PremiereDate    *string `json:"PremiereDate,omitempty"` // ISO 8601
	ProductionYear  *int    `json:"ProductionYear,omitempty"`
	CommunityRating *float64 `json:"CommunityRating,omitempty"`
	OfficialRating  string  `json:"OfficialRating,omitempty"`

	// 关系字段
	ParentID        string `json:"ParentId,omitempty"`
	ParentIndexNumber *int `json:"ParentIndexNumber,omitempty"` // Season number for Episode
	IndexNumber     *int   `json:"IndexNumber,omitempty"` // Episode number
	SeriesID        string `json:"SeriesId,omitempty"`
	SeriesName      string `json:"SeriesName,omitempty"`

	// 计数字段
	ChildCount      *int `json:"ChildCount,omitempty"`
	SeasonCount     *int `json:"SeasonCount,omitempty"`

	// 分类字段
	CollectionType  string `json:"CollectionType,omitempty"` // movies, tvshows
	GenreItems      []NameIdPair `json:"GenreItems,omitempty"`
	Genres          []string `json:"Genres,omitempty"`
	Studios         []NameIdPair `json:"Studios,omitempty"`
	Tags            []string `json:"Tags,omitempty"`
	Taglines        []string `json:"Taglines,omitempty"`
	People          []PersonInfo `json:"People,omitempty"`

	// 图片字段
	ImageTags       map[string]string `json:"ImageTags,omitempty"`
	BackdropImageTags []string `json:"BackdropImageTags,omitempty"`
	PrimaryImageAspectRatio *float64 `json:"PrimaryImageAspectRatio,omitempty"`

	// 图片继承字段（用于 Season/Episode 继承 Series 图片）
	ParentLogoItemID string `json:"ParentLogoItemId,omitempty"`
	ParentLogoImageTag string `json:"ParentLogoImageTag,omitempty"`
	ParentBackdropItemID string `json:"ParentBackdropItemId,omitempty"`
	ParentBackdropImageTags []string `json:"ParentBackdropImageTags,omitempty"`
	ParentThumbItemID string `json:"ParentThumbItemId,omitempty"`
	ParentThumbImageTag string `json:"ParentThumbImageTag,omitempty"`

	// 媒体源
	MediaSources    []MediaSourceDto `json:"MediaSources,omitempty"`

	// 用户数据
	UserData        *UserItemDataDto `json:"UserData,omitempty"`

	// Provider IDs
	ProviderIds     map[string]string `json:"ProviderIds,omitempty"`

	// 日期
	DateCreated     string `json:"DateCreated,omitempty"`
}

// MediaSourceDto 播放源
type MediaSourceDto struct {
	ID              string `json:"Id"`
	Name            string `json:"Name,omitempty"`
	Path            string `json:"Path"` // 302 重定向 URL
	Protocol        string `json:"Protocol"` // Http
	Container       string `json:"Container,omitempty"` // mkv, mp4, etc.
	Size            *int64 `json:"Size,omitempty"`
	Bitrate         *int   `json:"Bitrate,omitempty"`
	MediaStreams    []MediaStreamDto `json:"MediaStreams,omitempty"`
	DefaultAudioStreamIndex *int `json:"DefaultAudioStreamIndex,omitempty"`
	DefaultSubtitleStreamIndex *int `json:"DefaultSubtitleStreamIndex,omitempty"`
}

// MediaStreamDto 媒体流
type MediaStreamDto struct {
	Codec           string `json:"Codec"` // h264, aac, etc.
	CodecTag        string `json:"CodecTag,omitempty"`
	Language        string `json:"Language,omitempty"`
	ColorSpace      string `json:"ColorSpace,omitempty"`
	Width           *int   `json:"Width,omitempty"`
	Height          *int   `json:"Height,omitempty"`
	AspectRatio     string `json:"AspectRatio,omitempty"`
	Index           int    `json:"Index"`
	IsDefault       bool   `json:"IsDefault"`
	IsForced        bool   `json:"IsForced"`
	IsExternal      bool   `json:"IsExternal"`
	Type            string `json:"Type"` // Video, Audio, Subtitle
	Title           string `json:"Title,omitempty"`
	BitRate         *int   `json:"BitRate,omitempty"`
	SampleRate      *int   `json:"SampleRate,omitempty"`
	Channels        *int   `json:"Channels,omitempty"`
}

// UserItemDataDto 用户项目数据（已看、收藏、进度）
type UserItemDataDto struct {
	PlaybackPositionTicks int64  `json:"PlaybackPositionTicks"`
	PlayCount             int    `json:"PlayCount"`
	Played                bool   `json:"Played"`
	IsFavorite            bool   `json:"IsFavorite"`
	LastPlayedDate        *string `json:"LastPlayedDate,omitempty"` // ISO 8601
}

// PersonInfo 人物信息
type PersonInfo struct {
	Name             string `json:"Name"`
	ID               string `json:"Id,omitempty"`
	Role             string `json:"Role,omitempty"`
	Type             string `json:"Type"` // Actor, Director, Writer, etc.
	PrimaryImageTag  string `json:"PrimaryImageTag,omitempty"`
}

// NameIdPair 名称-ID 对（用于类型、工作室等）
type NameIdPair struct {
	Name string `json:"Name"`
	ID   string `json:"Id,omitempty"`
}

// ItemsResponse 媒体列表响应
type ItemsResponse struct {
	Items            []BaseItemDto `json:"Items"`
	TotalRecordCount int           `json:"TotalRecordCount"`
	StartIndex       int           `json:"StartIndex,omitempty"`
}

// ViewsResponse 媒体库视图响应
type ViewsResponse struct {
	Items            []BaseItemDto `json:"Items"`
	TotalRecordCount int           `json:"TotalRecordCount"`
}
