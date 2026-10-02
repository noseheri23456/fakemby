package types

import "encoding/json"

// 本文件是用户 / 会话相关的 Emby 兼容 DTO。
//
// 它们原先定义在 internal/api/emby/auth.go 里，和 handler、token 提取挤在同一个
// 795 行的文件中（2026-10-03 审查 P3-8）。迁到 types 之后：
//   - types 包保持零内部依赖（不 import database/service），DTO 可被任何层复用；
//   - DB 模型 → DTO 的映射（GetUserPolicy）留在 handler 层，因为那是唯一需要
//     同时认识 database.User 和 DTO 的地方。

type AuthenticateRequest struct {
	Username string `json:"Username" form:"Username"`
	Pw       string `json:"Pw" form:"Pw"`
	Password string `json:"Password" form:"Password"`
}

type AuthenticateResponse struct {
	User        UserDTO     `json:"User"`
	SessionInfo SessionInfo `json:"SessionInfo"`
	AccessToken string      `json:"AccessToken"`
	ServerID    string      `json:"ServerId"`
	// ForcePasswordChange 账户处于「必须改密」状态（首次启动的默认口令账户，A4）。
	// 客户端应引导用户改密，否则该口令长期暴露。
	ForcePasswordChange bool `json:"ForcePasswordChange"`
}

type UserDTO struct {
	ID                        string     `json:"Id"`
	Name                      string     `json:"Name"`
	ServerID                  string     `json:"ServerId,omitempty"`
	HasPassword               bool       `json:"HasPassword"`
	HasConfiguredPassword     bool       `json:"HasConfiguredPassword"`
	HasConfiguredEasyPassword bool       `json:"HasConfiguredEasyPassword"`
	PrimaryImageTag           string     `json:"PrimaryImageTag,omitempty"`
	IsAdmin                   bool       `json:"IsAdmin"`
	Policy                    UserPolicy `json:"Policy"`
	Configuration             UserConfig `json:"Configuration,omitempty"`
}

type UserPolicy struct {
	IsAdministrator                 bool `json:"IsAdministrator"`
	IsHidden                        bool `json:"IsHidden"`
	IsHiddenRemotely                bool `json:"IsHiddenRemotely"`
	IsDisabled                      bool `json:"IsDisabled"`
	EnableRemoteControlOfOtherUsers bool `json:"EnableRemoteControlOfOtherUsers"`
	EnableSharedDeviceControl       bool `json:"EnableSharedDeviceControl"`
	EnableRemoteAccess              bool `json:"EnableRemoteAccess"`
	EnableLiveTvManagement          bool `json:"EnableLiveTvManagement"`
	EnableLiveTvAccess              bool `json:"EnableLiveTvAccess"`
	EnableMediaPlayback             bool `json:"EnableMediaPlayback"`
	EnableAudioPlaybackTranscoding  bool `json:"EnableAudioPlaybackTranscoding"`
	EnableVideoPlaybackTranscoding  bool `json:"EnableVideoPlaybackTranscoding"`
	EnablePlaybackRemuxing          bool `json:"EnablePlaybackRemuxing"`
	EnableContentDeletion           bool `json:"EnableContentDeletion"`
	EnableContentDownloading        bool `json:"EnableContentDownloading"`
	EnableSubtitleDownloading       bool `json:"EnableSubtitleDownloading"`
	EnableSubtitleManagement        bool `json:"EnableSubtitleManagement"`
	EnableSyncTranscoding           bool `json:"EnableSyncTranscoding"`
	EnableMediaConversion           bool `json:"EnableMediaConversion"`
	EnableAllDevices                bool `json:"EnableAllDevices"`
	EnableAllFolders                bool `json:"EnableAllFolders"`
	// 注意：这两个数组字段**不能**带 omitempty。官方客户端会裸调
	// `Policy.EnabledFolders.includes(...)`，空切片被 omitempty 省略后
	// 客户端拿到 undefined，直接 TypeError 炸断渲染链（M3-1 审计发现）。
	EnabledFolders      []string `json:"EnabledFolders"`
	BlockedMediaFolders []string `json:"BlockedMediaFolders"`
	// 家长分级。M3-6：这两个字段 enforcement（internal/access）一直在用，
	// 但 DTO 没暴露——客户端「GET 用户 → 改开关 → POST 回 Policy」的往返会把它们
	// 悄悄清空，而清空家长分级是**放开**限制而不是收紧。必须进 DTO 才能闭环。
	// MaxParentalRating 用指针：null 表示不限制，与官方一致（官方也返回 null）。
	MaxParentalRating *int `json:"MaxParentalRating"`
	// BlockUnratedItems 同样不能带 omitempty（数组字段，客户端裸调 .includes）。
	BlockUnratedItems       []string `json:"BlockUnratedItems"`
	SimultaneousStreamLimit int      `json:"SimultaneousStreamLimit"`
	AllowCameraUpload       bool     `json:"AllowCameraUpload"`
}

// MarshalJSON 把可空数组兜成 []，与 BaseItemDto / MediaSourceDto 同一套约定。
//
// 光靠「不带 omitempty」还不够：Policy 会经过客户端 GET → 改 → POST 的往返，
// 以及管理面 validateAdminPolicy 的部分字段更新，中间任何一环漏初始化都会让
// 客户端拿到 null。总闸放在序列化出口上，比逐个构造点自检可靠。
func (p UserPolicy) MarshalJSON() ([]byte, error) {
	type wire UserPolicy
	if p.EnabledFolders == nil {
		p.EnabledFolders = []string{}
	}
	if p.BlockedMediaFolders == nil {
		p.BlockedMediaFolders = []string{}
	}
	if p.BlockUnratedItems == nil {
		p.BlockUnratedItems = []string{}
	}
	return json.Marshal(wire(p))
}

// UserConfig 对齐官方 Emby 的 UserConfiguration。数组字段必须保证序列化为
// []（而非 null）：官方客户端（Emby Theater / Emby Web）在首页渲染时会裸调用
// Configuration.LatestItemsExcludes.includes(...) / .indexOf(...)，字段缺失
// （undefined）会抛 TypeError，炸掉板块加载的 Promise.all 链，主页永久转圈。
type UserConfig struct {
	AudioLanguagePreference    string   `json:"AudioLanguagePreference"`
	PlayDefaultAudioTrack      bool     `json:"PlayDefaultAudioTrack"`
	SubtitleLanguagePreference string   `json:"SubtitleLanguagePreference"`
	SubtitleMode               string   `json:"SubtitleMode"`
	DisplayMissingEpisodes     bool     `json:"DisplayMissingEpisodes"`
	GroupedFolders             []string `json:"GroupedFolders"`
	LatestItemsExcludes        []string `json:"LatestItemsExcludes"`
	MyMediaExcludes            []string `json:"MyMediaExcludes"`
	OrderedViews               []string `json:"OrderedViews"`
	HidePlayedInLatest         bool     `json:"HidePlayedInLatest"`
	EnableNextEpisodeAutoPlay  bool     `json:"EnableNextEpisodeAutoPlay"`
	RememberAudioSelections    bool     `json:"RememberAudioSelections"`
	RememberSubtitleSelections bool     `json:"RememberSubtitleSelections"`
	ResumeRewindSeconds        int      `json:"ResumeRewindSeconds"`
	EnableLocalPassword        bool     `json:"EnableLocalPassword"`
}

// MarshalJSON 同 UserPolicy：四个数组字段一律兜成 []。
func (c UserConfig) MarshalJSON() ([]byte, error) {
	type wire UserConfig
	if c.GroupedFolders == nil {
		c.GroupedFolders = []string{}
	}
	if c.LatestItemsExcludes == nil {
		c.LatestItemsExcludes = []string{}
	}
	if c.MyMediaExcludes == nil {
		c.MyMediaExcludes = []string{}
	}
	if c.OrderedViews == nil {
		c.OrderedViews = []string{}
	}
	return json.Marshal(wire(c))
}

// DefaultUserConfig 返回字段完整、数组已初始化的用户配置。
// 所有构造 UserDTO 的地方必须用它，禁止手写 UserConfig{...} 字面量——
// 零值的切片字段会序列化成 null，客户端裸调用 .includes() 即崩。
func DefaultUserConfig() UserConfig {
	return UserConfig{
		SubtitleMode:               "Default",
		EnableNextEpisodeAutoPlay:  true,
		RememberAudioSelections:    true,
		RememberSubtitleSelections: true,
		GroupedFolders:             []string{},
		LatestItemsExcludes:        []string{},
		MyMediaExcludes:            []string{},
		OrderedViews:               []string{},
	}
}

type SessionInfo struct {
	PlayState           PlayState       `json:"PlayState"`
	AdditionalUsers     []UserDTO       `json:"AdditionalUsers"`
	Capabilities        Capabilities    `json:"Capabilities"`
	RemoteEndPoint      string          `json:"RemoteEndPoint"`
	PlayableMediaTypes  []string        `json:"PlayableMediaTypes"`
	Id                  string          `json:"Id"`
	UserId              string          `json:"UserId"`
	UserName            string          `json:"UserName"`
	Client              string          `json:"Client"`
	LastActivityDate    string          `json:"LastActivityDate"`
	LastPlaybackCheckIn string          `json:"LastPlaybackCheckIn"`
	DeviceName          string          `json:"DeviceName"`
	DeviceId            string          `json:"DeviceId"`
	ApplicationVersion  string          `json:"ApplicationVersion"`
	AppIconUrl          string          `json:"AppIconUrl"`
	SupportedCommands   []string        `json:"SupportedCommands"`
	NowPlayingItem      *NowPlayingItem `json:"NowPlayingItem,omitempty"`
}

// MarshalJSON 同 UserPolicy：客户端裸调的数组字段一律兜成 []。
func (s SessionInfo) MarshalJSON() ([]byte, error) {
	type wire SessionInfo
	if s.AdditionalUsers == nil {
		s.AdditionalUsers = []UserDTO{}
	}
	if s.PlayableMediaTypes == nil {
		s.PlayableMediaTypes = []string{}
	}
	if s.SupportedCommands == nil {
		s.SupportedCommands = []string{}
	}
	return json.Marshal(wire(s))
}

// NowPlayingItem 是 SessionInfo 里正在播放条目的最小视图。
type NowPlayingItem struct {
	Id   string `json:"Id"`
	Name string `json:"Name"`
	Type string `json:"Type"`
}

type PlayState struct {
	PositionTicks       *int64 `json:"PositionTicks,omitempty"`
	CanSeek             bool   `json:"CanSeek"`
	IsPaused            bool   `json:"IsPaused"`
	IsMuted             bool   `json:"IsMuted"`
	VolumeLevel         *int   `json:"VolumeLevel,omitempty"`
	AudioStreamIndex    *int   `json:"AudioStreamIndex,omitempty"`
	SubtitleStreamIndex *int   `json:"SubtitleStreamIndex,omitempty"`
	MediaSourceId       string `json:"MediaSourceId,omitempty"`
	PlayMethod          string `json:"PlayMethod,omitempty"`
	RepeatMode          string `json:"RepeatMode,omitempty"`
}

type Capabilities struct {
	PlayableMediaTypes           []string `json:"PlayableMediaTypes"`
	SupportedCommands            []string `json:"SupportedCommands"`
	SupportsMediaControl         bool     `json:"SupportsMediaControl"`
	SupportsContentUploading     bool     `json:"SupportsContentUploading"`
	SupportsPersistentIdentifier bool     `json:"SupportsPersistentIdentifier"`
	SupportsSync                 bool     `json:"SupportsSync"`
}

// MarshalJSON 同 UserPolicy：两个数组字段一律兜成 []。
func (c Capabilities) MarshalJSON() ([]byte, error) {
	type wire Capabilities
	if c.PlayableMediaTypes == nil {
		c.PlayableMediaTypes = []string{}
	}
	if c.SupportedCommands == nil {
		c.SupportedCommands = []string{}
	}
	return json.Marshal(wire(c))
}

// PublicUserDTO 是 /emby/Users/Public 的返回结构（M0-6 / S4）。
//
// 该端点无需鉴权，且历史上配合 CORS 全开可被任意网页跨域读取。
// 因此只保留登录页必需的最小字段，去掉 IsAdmin / Policy——
// 否则任何人都能枚举出全部管理员账号。
type PublicUserDTO struct {
	ID                        string     `json:"Id"`
	Name                      string     `json:"Name"`
	ServerID                  string     `json:"ServerId,omitempty"`
	HasPassword               bool       `json:"HasPassword"`
	HasConfiguredPassword     bool       `json:"HasConfiguredPassword"`
	HasConfiguredEasyPassword bool       `json:"HasConfiguredEasyPassword"`
	Configuration             UserConfig `json:"Configuration,omitempty"`
}
