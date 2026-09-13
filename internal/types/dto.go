package types

// BaseItemDto Emby 标准媒体项目 DTO
// 字段顺序匹配 Emby 4.9.3.0 服务器响应顺序
type BaseItemDto struct {
	Name      string `json:"Name"`
	ServerID  string `json:"ServerId,omitempty"`
	ID        string `json:"Id"`
	Type      string `json:"Type"` // Movie, Series, Season, Episode, Folder, etc.
	IsFolder  bool   `json:"IsFolder"`
	MediaType string `json:"MediaType"` // Video, Audio

	Etag string `json:"Etag,omitempty"`
	Guid string `json:"Guid,omitempty"`

	Overview        string   `json:"Overview,omitempty"`
	OriginalTitle   string   `json:"OriginalTitle,omitempty"`
	SortName        string   `json:"SortName,omitempty"`
	ForcedSortName  string   `json:"ForcedSortName,omitempty"`
	Container       string   `json:"Container,omitempty"`
	RunTimeTicks    *int64   `json:"RunTimeTicks,omitempty"`
	PremiereDate    *string  `json:"PremiereDate,omitempty"`
	ProductionYear  *int     `json:"ProductionYear,omitempty"`
	CommunityRating *float64 `json:"CommunityRating,omitempty"`
	OfficialRating  string   `json:"OfficialRating,omitempty"`

	CanDelete    bool     `json:"CanDelete"`
	CanDownload  bool     `json:"CanDownload"`
	SupportsSync bool     `json:"SupportsSync"`
	LockData     bool     `json:"LockData"`
	LockedFields []string `json:"LockedFields"`

	ParentID          string `json:"ParentId,omitempty"`
	ParentIndexNumber *int   `json:"ParentIndexNumber,omitempty"`
	IndexNumber       *int   `json:"IndexNumber,omitempty"`
	SeriesID          string `json:"SeriesId,omitempty"`
	SeriesName        string `json:"SeriesName,omitempty"`

	ChildCount  *int `json:"ChildCount,omitempty"`
	SeasonCount *int `json:"SeasonCount,omitempty"`

	CollectionType string       `json:"CollectionType,omitempty"`
	GenreItems     []NameIdPair `json:"GenreItems"`
	Genres         []string     `json:"Genres"`
	Studios        []NameIdPair `json:"Studios"`
	Tags           []string     `json:"Tags"`
	Taglines       []string     `json:"Taglines"`
	Countries      []string     `json:"Countries"`
	Languages      []string     `json:"Languages"`
	People         []PersonInfo `json:"People"`

	ImageTags               map[string]string `json:"ImageTags"`
	BackdropImageTags       []string          `json:"BackdropImageTags"`
	PrimaryImageAspectRatio *float64          `json:"PrimaryImageAspectRatio,omitempty"`

	ParentLogoItemID        string   `json:"ParentLogoItemId,omitempty"`
	ParentLogoImageTag      string   `json:"ParentLogoImageTag,omitempty"`
	ParentBackdropItemID    string   `json:"ParentBackdropItemId,omitempty"`
	ParentBackdropImageTags []string `json:"ParentBackdropImageTags"`
	ParentThumbItemID       string   `json:"ParentThumbItemId,omitempty"`
	ParentThumbImageTag     string   `json:"ParentThumbImageTag,omitempty"`

	UserData       *UserItemDataDto  `json:"UserData"`
	MediaSources   []MediaSourceDto  `json:"MediaSources"`
	ProviderIds    map[string]string `json:"ProviderIds"`
	RemoteTrailers []ExternalUrl     `json:"RemoteTrailers"`
	Subviews       []string          `json:"Subviews"`

	DateCreated           string        `json:"DateCreated,omitempty"`
	DateModified          string        `json:"DateModified,omitempty"`
	PresentationUniqueKey string        `json:"PresentationUniqueKey,omitempty"`
	DisplayPreferencesId  string        `json:"DisplayPreferencesId,omitempty"`
	IsHidden              bool          `json:"IsHidden"`
	ExternalUrls          []ExternalUrl `json:"ExternalUrls"`
}

// LibraryOptions 媒体库选项
type LibraryOptions struct {
	EnablePhotos                 bool `json:"EnablePhotos"`
	EnableRealtimeMonitor        bool `json:"EnableRealtimeMonitor"`
	EnableChapterImageExtraction bool `json:"EnableChapterImageExtraction"`
	EnableInternetProviders      bool `json:"EnableInternetProviders"`
}

// ExternalUrl 外部链接
type ExternalUrl struct {
	Name string `json:"Name"`
	Url  string `json:"Url"`
}

// MediaSourceDto 播放源
type MediaSourceDto struct {
	ID                         string           `json:"Id"`
	Name                       string           `json:"Name,omitempty"`
	Path                       string           `json:"Path"`                // 302 重定向 URL
	Protocol                   string           `json:"Protocol"`            // Http
	Type                       string           `json:"Type,omitempty"`      // Default
	Container                  string           `json:"Container,omitempty"` // mkv, mp4, etc.
	Size                       *int64           `json:"Size,omitempty"`
	Bitrate                    *int             `json:"Bitrate,omitempty"`
	RunTimeTicks               *int64           `json:"RunTimeTicks,omitempty"`
	IsRemote                   bool             `json:"IsRemote"`
	HasMixedProtocols          bool             `json:"HasMixedProtocols"`
	SupportsTranscoding        bool             `json:"SupportsTranscoding"`
	SupportsDirectStream       bool             `json:"SupportsDirectStream"`
	SupportsDirectPlay         bool             `json:"SupportsDirectPlay"`
	IsInfiniteStream           bool             `json:"IsInfiniteStream"`
	RequiresOpening            bool             `json:"RequiresOpening"`
	RequiresClosing            bool             `json:"RequiresClosing"`
	RequiresLooping            bool             `json:"RequiresLooping"`
	SupportsProbing            bool             `json:"SupportsProbing"`
	MediaStreams               []MediaStreamDto `json:"MediaStreams"`
	DefaultAudioStreamIndex    *int             `json:"DefaultAudioStreamIndex,omitempty"`
	DefaultSubtitleStreamIndex *int             `json:"DefaultSubtitleStreamIndex,omitempty"`
	ReadAtNativeFramerate      bool             `json:"ReadAtNativeFramerate"`
	Formats                    []string         `json:"Formats"`
	// RequiredHttpHeaders 不带 omitempty：官方客户端 supportsDirectPlay() 裸调
	// mediaSource.RequiredHttpHeaders.length，字段缺失（undefined）会 TypeError
	// 炸断详情页 Promise 链。空 map 序列化为 {}，与官方服务器一致。
	RequiredHttpHeaders map[string]string `json:"RequiredHttpHeaders"`
	DirectStreamUrl     string            `json:"DirectStreamUrl,omitempty"`
}

// MediaStreamDto 媒体流
type MediaStreamDto struct {
	Codec                           string   `json:"Codec"` // h264, aac, etc.
	CodecTag                        string   `json:"CodecTag,omitempty"`
	Language                        string   `json:"Language,omitempty"`
	ColorSpace                      string   `json:"ColorSpace,omitempty"`
	ColorTransfer                   string   `json:"ColorTransfer,omitempty"`
	ColorPrimaries                  string   `json:"ColorPrimaries,omitempty"`
	TimeBase                        string   `json:"TimeBase,omitempty"`
	VideoRange                      string   `json:"VideoRange,omitempty"`
	DisplayTitle                    string   `json:"DisplayTitle,omitempty"`
	NalLengthSize                   string   `json:"NalLengthSize,omitempty"`
	Width                           *int     `json:"Width,omitempty"`
	Height                          *int     `json:"Height,omitempty"`
	AspectRatio                     string   `json:"AspectRatio,omitempty"`
	AverageFrameRate                *float64 `json:"AverageFrameRate,omitempty"`
	RealFrameRate                   *float64 `json:"RealFrameRate,omitempty"`
	Profile                         string   `json:"Profile,omitempty"`
	Level                           *int     `json:"Level,omitempty"`
	BitDepth                        *int     `json:"BitDepth,omitempty"`
	RefFrames                       *int     `json:"RefFrames,omitempty"`
	PixelFormat                     string   `json:"PixelFormat,omitempty"`
	ChannelLayout                   string   `json:"ChannelLayout,omitempty"`
	Index                           int      `json:"Index"`
	IsDefault                       bool     `json:"IsDefault"`
	IsForced                        bool     `json:"IsForced"`
	IsExternal                      bool     `json:"IsExternal"`
	IsInterlaced                    bool     `json:"IsInterlaced"`
	IsHearingImpaired               bool     `json:"IsHearingImpaired"`
	IsTextSubtitleStream            bool     `json:"IsTextSubtitleStream"`
	IsAnamorphic                    bool     `json:"IsAnamorphic"`
	SupportsExternalStream          bool     `json:"SupportsExternalStream"`
	Type                            string   `json:"Type"` // Video, Audio, Subtitle
	Title                           string   `json:"Title,omitempty"`
	Protocol                        string   `json:"Protocol,omitempty"`
	BitRate                         *int     `json:"BitRate,omitempty"`
	SampleRate                      *int     `json:"SampleRate,omitempty"`
	Channels                        *int     `json:"Channels,omitempty"`
	ExtendedVideoType               string   `json:"ExtendedVideoType,omitempty"`
	ExtendedVideoSubType            string   `json:"ExtendedVideoSubType,omitempty"`
	ExtendedVideoSubTypeDescription string   `json:"ExtendedVideoSubTypeDescription,omitempty"`
	AttachmentSize                  int      `json:"AttachmentSize"`
}

// UserItemDataDto 用户项目数据（已看、收藏、进度）
type UserItemDataDto struct {
	PlaybackPositionTicks int64   `json:"PlaybackPositionTicks"`
	PlayCount             int     `json:"PlayCount"`
	Played                bool    `json:"Played"`
	IsFavorite            bool    `json:"IsFavorite"`
	LastPlayedDate        *string `json:"LastPlayedDate,omitempty"` // ISO 8601
	Key                   string  `json:"Key,omitempty"`
	ItemId                string  `json:"ItemId,omitempty"`
	PlayedPercentage      float64 `json:"PlayedPercentage,omitempty"`
	UnplayedItemCount     *int    `json:"UnplayedItemCount,omitempty"`
}

// PersonInfo 人物信息
type PersonInfo struct {
	Name            string `json:"Name"`
	ID              string `json:"Id"`
	Role            string `json:"Role,omitempty"`
	Type            string `json:"Type"` // Actor, Director, Writer, etc.
	PrimaryImageTag string `json:"PrimaryImageTag,omitempty"`
}

// NameIdPair 名称-ID 对（用于类型、工作室等）
type NameIdPair struct {
	Name string `json:"Name"`
	ID   string `json:"Id"`
}

// ItemsResponse 媒体列表响应
type ItemsResponse struct {
	Items            []BaseItemDto `json:"Items"`
	TotalRecordCount int           `json:"TotalRecordCount"`
	StartIndex       int           `json:"StartIndex"`
}

// ViewsResponse 媒体库视图响应
type ViewsResponse struct {
	Items            []BaseItemDto `json:"Items"`
	TotalRecordCount int           `json:"TotalRecordCount"`
}

// SearchHintDto 搜索提示项
type SearchHintDto struct {
	ItemId            string   `json:"ItemId"`
	Id                string   `json:"Id"`
	Name              string   `json:"Name"`
	Type              string   `json:"Type"`
	MediaType         string   `json:"MediaType,omitempty"`
	ProductionYear    *int     `json:"ProductionYear,omitempty"`
	RunTimeTicks      *int64   `json:"RunTimeTicks,omitempty"`
	IndexNumber       *int     `json:"IndexNumber,omitempty"`
	ParentIndexNumber *int     `json:"ParentIndexNumber,omitempty"`
	PrimaryImageTag   string   `json:"PrimaryImageTag,omitempty"`
	ThumbImageTag     string   `json:"ThumbImageTag,omitempty"`
	BackdropImageTags []string `json:"BackdropImageTags,omitempty"`
	SeriesName        string   `json:"SeriesName,omitempty"`
}

// NewBaseItemDto 返回一个所有"面向客户端的数组/map 字段"都已初始化的 DTO。
//
// 官方客户端对这些字段裸调 .length / .includes / .filter，null 会直接 TypeError
// 炸断渲染链（首页无限转圈或详情页 "Content no longer available"）。
// 因此新增字段时必须同时改这里——集中一处比在每个构造点各写一遍更不容易漏。
func NewBaseItemDto() BaseItemDto {
	return BaseItemDto{
		Genres:            []string{},
		Studios:           []NameIdPair{},
		GenreItems:        []NameIdPair{},
		Tags:              []string{},
		Taglines:          []string{},
		Countries:         []string{},
		Languages:         []string{},
		People:            []PersonInfo{},
		ImageTags:         map[string]string{},
		BackdropImageTags: []string{},
		MediaSources:      []MediaSourceDto{},
		ProviderIds:       map[string]string{},
		RemoteTrailers:    []ExternalUrl{},
		ExternalUrls:      []ExternalUrl{},
		LockedFields:      []string{},
		Subviews:          []string{},
	}
}
