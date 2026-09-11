package testutil

import (
	"testing"
	"time"

	"github.com/fakemby/fakemby/internal/database"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// 固定的 ID 与口令，让测试断言可以写死、失败信息可读。
const (
	AdminUserID  = "user-admin"
	NormalUserID = "user-normal"
	OtherUserID  = "user-other"

	AdminUserName  = "admin"
	NormalUserName = "alice"
	OtherUserName  = "bob"

	// Password 三个用户共用，便于测试"改密码后旧口令失效"这类场景。
	Password = "test-password"

	AdminToken  = "token-admin"
	NormalToken = "token-alice"
	OtherToken  = "token-bob"
	// ExpiredToken 对应一条 CreatedAt 已被推到过期时间点之前的 token 记录。
	ExpiredToken = "token-expired"

	MovieLibID   = "lib-movies"
	ShowLibID    = "lib-shows"
	MovieID      = "item-movie"
	SeriesID     = "item-series"
	SeasonID     = "item-season"
	EpisodeID    = "item-episode"
	MovieSrcID   = "src-movie"
	EpisodeSrcID = "src-episode"

	// MovieSrcURL 故意落在 sign_prefixes 之外：默认配置下不应被追加签名。
	MovieSrcURL = "https://cdn.example.com/movies/dune.mkv"
	// EpisodeSrcURL 落在 TestConfig 的 sign_prefixes 内：应被追加 exp/sig。
	EpisodeSrcURL = "https://openlist.example.com/d/ep1.mkv"
)

// Fixtures 一次种子数据产生的可引用对象。
type Fixtures struct {
	Admin  database.User
	Normal database.User
	Other  database.User
}

// SeedFixtures 写入确定性的种子数据：3 个用户（管理员 / 普通 / 他人）、2 个媒体库、
// 一部电影、一部剧（Series → Season → Episode）、播放源、图片、字幕与播放进度。
//
// 覆盖意图：
//   - Movie 被 Normal 用户标记为已看 + 收藏；
//   - Episode 有播放进度但未看完（用于"继续观看"）；
//   - Episode 自身没有海报，靠父级 Series 继承（用于图片继承测试）。
func SeedFixtures(t *testing.T, db *gorm.DB) Fixtures {
	t.Helper()

	hash := hashForTest(t, Password)

	admin := database.User{ID: AdminUserID, Name: AdminUserName, PasswordHash: hash, IsAdmin: true, Policy: "{}", AllowRemoteAccess: true}
	normal := database.User{ID: NormalUserID, Name: NormalUserName, PasswordHash: hash, IsAdmin: false, Policy: "{}", AllowRemoteAccess: true}
	other := database.User{ID: OtherUserID, Name: OtherUserName, PasswordHash: hash, IsAdmin: false, Policy: "{}", AllowRemoteAccess: true}

	year := 2021
	seasonNo := 1
	episodeNo := 1
	size := int64(4_294_967_296)
	bitrate := 8_000_000
	runtime := int64(93 * 60 * 10_000_000) // 93 分钟，单位 tick
	width, height := 1920, 1080
	seriesID := SeriesID
	seasonID := SeasonID

	movie := database.MediaItem{
		ID: MovieID, LibraryID: MovieLibID, Type: "Movie",
		Name: "沙丘", OriginalTitle: "Dune", SortName: "沙丘",
		Overview: "沙漠星球上的权力斗争", Year: &year, PremiereDate: strPtr("2021-10-22"),
		CommunityRating: floatPtr(7.8), OfficialRating: "PG-13",
		Genres: `["科幻","冒险"]`, Studios: `["Legendary Pictures"]`,
		Tags: `["科幻"]`, Container: "mkv", VideoCodec: "h264", AudioCodec: "ac3",
		RuntimeTicks: &runtime, Width: &width, Height: &height, TMDBID: "438631",
	}
	series := database.MediaItem{
		ID: SeriesID, LibraryID: ShowLibID, Type: "Series",
		Name: "西部世界", SortName: "西部世界", Overview: "AI 主题公园",
		Year: &year, Genres: `["科幻","剧情"]`,
	}
	season := database.MediaItem{
		ID: SeasonID, LibraryID: ShowLibID, ParentID: &seriesID, Type: "Season",
		Name: "第 1 季", SeasonNumber: &seasonNo,
	}
	episode := database.MediaItem{
		ID: EpisodeID, LibraryID: ShowLibID, ParentID: &seasonID, Type: "Episode",
		Name: "第 1 集", SeasonNumber: &seasonNo, EpisodeNumber: &episodeNo,
		Container: "mkv", VideoCodec: "h264", AudioCodec: "aac",
		RuntimeTicks: &runtime,
	}

	movieSrc := database.MediaSource{
		ID: MovieSrcID, ItemID: MovieID, Name: "沙丘-1080p",
		URL: MovieSrcURL, Protocol: "Http", Container: "mkv",
		Size: &size, Bitrate: &bitrate, SortOrder: 0,
	}
	episodeSrc := database.MediaSource{
		ID: EpisodeSrcID, ItemID: EpisodeID, Name: "西部世界-S01E01",
		URL: EpisodeSrcURL, Protocol: "Http", Container: "mkv",
		Size: &size, Bitrate: &bitrate, SortOrder: 0,
	}

	now := time.Now()
	playedAt := now.Add(-time.Hour)
	progress := []database.PlayProgress{
		// 电影：已看完 + 收藏
		{UserID: NormalUserID, ItemID: MovieID, PositionTicks: runtime, PlayCount: 1, IsPlayed: true, IsFavorite: true, LastPlayed: &playedAt},
		// 剧集：看了一半，未看完 → 应出现在"继续观看"
		{UserID: NormalUserID, ItemID: EpisodeID, PositionTicks: runtime / 2, PlayCount: 0, IsPlayed: false, LastPlayed: &playedAt},
		// 他人数据：归属校验测试用（alice 不该看到 bob 的进度）
		// 刻意与 alice 不同：看过但没收藏，便于区分"用户维度"是否被正确带入
		{UserID: OtherUserID, ItemID: MovieID, PositionTicks: 100, PlayCount: 3, IsPlayed: true, IsFavorite: false, LastPlayed: &playedAt},
	}

	images := []database.Image{
		{ItemID: MovieID, Type: "Primary", Idx: 0, URL: "https://image.example.com/dune.jpg", Tag: "abcd1234"},
		{ItemID: MovieID, Type: "Backdrop", Idx: 0, URL: "https://image.example.com/dune-bg.jpg", Tag: "bg000001"},
		{ItemID: SeriesID, Type: "Primary", Idx: 0, URL: "https://image.example.com/ww.jpg", Tag: "ww123456"},
		{ItemID: SeriesID, Type: "Backdrop", Idx: 0, URL: "https://image.example.com/ww-bg.jpg", Tag: "bgww0001"},
	}

	subtitles := []database.Subtitle{
		{ItemID: EpisodeID, Language: "chi", Title: "中文字幕", URL: "https://sub.example.com/ep1.srt", Codec: "srt"},
	}

	tokens := []database.Token{
		{Token: AdminToken, UserID: AdminUserID, DeviceID: "dev-admin", DeviceName: "Test Admin", Client: "test", Version: "1.0"},
		{Token: NormalToken, UserID: NormalUserID, DeviceID: "dev-alice", DeviceName: "Test Alice", Client: "test", Version: "1.0"},
		{Token: OtherToken, UserID: OtherUserID, DeviceID: "dev-bob", DeviceName: "Test Bob", Client: "test", Version: "1.0"},
		// 40 天前签发，配合 30 天有效期 → 应判定为过期
		{Token: ExpiredToken, UserID: NormalUserID, DeviceID: "dev-old", CreatedAt: now.AddDate(0, 0, -40)},
	}

	insert := func(label string, value any) {
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("写入种子数据失败(%s): %v", label, err)
		}
	}

	insert("libraries", []database.Library{
		{ID: MovieLibID, Name: "电影", Type: "movies", SortOrder: 1},
		{ID: ShowLibID, Name: "剧集", Type: "tvshows", SortOrder: 2},
	})
	insert("users", []database.User{admin, normal, other})
	insert("items", []database.MediaItem{movie, series, season, episode})
	insert("sources", []database.MediaSource{movieSrc, episodeSrc})
	insert("images", images)
	insert("subtitles", subtitles)
	insert("progress", progress)
	insert("tokens", tokens)

	return Fixtures{Admin: admin, Normal: normal, Other: other}
}

func hashForTest(t *testing.T, password string) string {
	t.Helper()
	// MinCost：测试只关心逻辑正确性，没必要为每个用户花 100ms 做 bcrypt。
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成测试用密码哈希失败: %v", err)
	}
	return string(hash)
}

func strPtr(s string) *string     { return &s }
func floatPtr(f float64) *float64 { return &f }
