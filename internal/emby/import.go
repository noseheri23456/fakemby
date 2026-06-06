package emby

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ImportRequest struct {
	Library string        `json:"library"`
	Items   []ImportItem  `json:"items"`
}

type ImportItem struct {
	Name            string                 `json:"name"`
	OriginalTitle   string                 `json:"original_title"`
	Year            *int                   `json:"year"`
	Type            string                 `json:"type"` // Movie, Series, Season, Episode
	Overview        string                 `json:"overview"`
	Genres          []string               `json:"genres"`
	Studios         []string               `json:"studios"`
	Tags            []string               `json:"tags"`
	People          []ImportPerson         `json:"people"` // 演员、导演、编剧等
	CommunityRating *float64               `json:"community_rating"`
	OfficialRating  string                 `json:"official_rating"`
	TMDBID          string                 `json:"tmdb_id"`
	IMDBID          string                 `json:"imdb_id"`
	TVDBID          string                 `json:"tvdb_id"`
	RuntimeMinutes  *int64                 `json:"runtime_minutes"`
	SeasonNumber    *int                   `json:"season_number"`
	EpisodeNumber   *int                   `json:"episode_number"`
	Images          map[string]string      `json:"images"` // type -> URL
	Sources         []ImportSource         `json:"sources"`
	Subtitles       []ImportSubtitle       `json:"subtitles"`
	Seasons         []ImportSeason         `json:"seasons"` // 嵌套的 Seasons（Series 内部）
}

type ImportPerson struct {
	Name string `json:"name"`
	Type string `json:"type"` // Actor, Director, Writer, Producer, etc.
	Role string `json:"role"` // 角色（仅用于演员）
}

type ImportSeason struct {
	SeasonNumber *int           `json:"season_number"`
	Episodes     []ImportEpisode `json:"episodes"`
}

type ImportEpisode struct {
	Name          string         `json:"name"`
	EpisodeNumber *int           `json:"episode_number"`
	Overview      string         `json:"overview"`
	Sources       []ImportSource `json:"sources"`
}

type ImportSource struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Container string `json:"container"`
	Width     *int   `json:"width"`
	Height    *int   `json:"height"`
	Bitrate   *int   `json:"bitrate"`
}

type ImportSubtitle struct {
	Language string `json:"language"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Codec    string `json:"codec"`
}

type ImportResponse struct {
	Imported int      `json:"imported"`
	Errors   []string `json:"errors"`
}

func RegisterImportRoutes(router *gin.Engine) {
	router.POST("/api/admin/import", adminAuth(), importBatch())
}

func importBatch() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ImportRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		if req.Library == "" || len(req.Items) == 0 {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}

		// 获取或创建媒体库
		lib, err := getOrCreateLibrary(req.Library)
		if err != nil {
			slog.Error("获取或创建库失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		// 批量导入（单个事务）
		resp := &ImportResponse{
			Imported: 0,
			Errors:   []string{},
		}

		err = database.Get().Transaction(func(tx *gorm.DB) error {
			for _, item := range req.Items {
				if err := importItem(tx, lib.ID, item, nil); err != nil {
					resp.Errors = append(resp.Errors, err.Error())
				} else {
					resp.Imported++
				}
			}
			return nil
		})

		if err != nil {
			slog.Error("批量导入事务失败", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}

		c.JSON(http.StatusOK, resp)
	}
}

func getOrCreateLibrary(libName string) (*database.Library, error) {
	var lib database.Library
	// 先按 name 查找库
	result := database.Get().Where("name = ?", libName).First(&lib)

	if result.Error == nil {
		return &lib, nil
	}

	if result.Error == gorm.ErrRecordNotFound {
		// 创建新库，根据名称判断类型
		libType := "movies"
		if libName == "TV Shows" {
			libType = "tvshows"
		} else if libName == "Movies" {
			libType = "movies"
		}

		lib = database.Library{
			ID:   uuid.New().String(),
			Name: libName,
			Type: libType,
		}
		if err := database.Get().Create(&lib).Error; err != nil {
			return nil, err
		}
		return &lib, nil
	}

	return nil, result.Error
}

func importItem(tx *gorm.DB, libraryID string, item ImportItem, parentID *string) error {
	itemID := uuid.New().String()

	// 计算运行时间（转换为 ticks：100纳秒）
	var runtimeTicks *int64
	if item.RuntimeMinutes != nil {
		ticks := *item.RuntimeMinutes * 60 * 10_000_000
		runtimeTicks = &ticks
	}

	// 序列化 JSON 字段
	genresJSON, _ := json.Marshal(item.Genres)
	studiosJSON, _ := json.Marshal(item.Studios)
	tagsJSON, _ := json.Marshal(item.Tags)

	// 转换 ImportPerson 为 PersonInfo 格式
	peopleInfo := make([]map[string]interface{}, 0, len(item.People))
	for _, p := range item.People {
		person := map[string]interface{}{
			"Name": p.Name,
			"Type": p.Type,
		}
		if p.Role != "" {
			person["Role"] = p.Role
		}
		peopleInfo = append(peopleInfo, person)
	}
	peopleJSON, _ := json.Marshal(peopleInfo)

	mediaItem := &database.MediaItem{
		ID:              itemID,
		LibraryID:       libraryID,
		ParentID:        parentID,
		Type:            item.Type,
		Name:            item.Name,
		OriginalTitle:   item.OriginalTitle,
		Overview:        item.Overview,
		Year:            item.Year,
		CommunityRating: item.CommunityRating,
		OfficialRating:  item.OfficialRating,
		Genres:          string(genresJSON),
		Studios:         string(studiosJSON),
		Tags:            string(tagsJSON),
		People:          string(peopleJSON),
		TMDBID:          item.TMDBID,
		IMDBID:          item.IMDBID,
		TVDBID:          item.TVDBID,
		RuntimeTicks:    runtimeTicks,
		SeasonNumber:    item.SeasonNumber,
		EpisodeNumber:   item.EpisodeNumber,
	}

	if err := tx.Create(mediaItem).Error; err != nil {
		return err
	}

	// 导入媒体源
	for _, src := range item.Sources {
		source := &database.MediaSource{
			ID:        uuid.New().String(),
			ItemID:    itemID,
			Name:      src.Name,
			URL:       src.URL,
			Protocol:  "Http",
			Container: src.Container,
			Bitrate:   src.Bitrate,
		}
		if err := tx.Create(source).Error; err != nil {
			return err
		}
	}

	// 导入图片
	for imgType, imgURL := range item.Images {
		tag := generateImageTag(imgURL)
		image := &database.Image{
			ItemID: itemID,
			Type:   imgType,
			Idx:    0,
			URL:    imgURL,
			Tag:    tag,
		}
		if err := tx.Create(image).Error; err != nil {
			return err
		}
	}

	// 导入字幕
	for _, sub := range item.Subtitles {
		subtitle := &database.Subtitle{
			ItemID:   itemID,
			Language: sub.Language,
			Title:    sub.Title,
			URL:      sub.URL,
			Codec:    sub.Codec,
		}
		if err := tx.Create(subtitle).Error; err != nil {
			return err
		}
	}

	// 如果是 Series，创建嵌套的 Seasons 和 Episodes
	if item.Type == "Series" && len(item.Seasons) > 0 {
		for _, season := range item.Seasons {
			seasonNum := 1
			if season.SeasonNumber != nil {
				seasonNum = *season.SeasonNumber
			}

			// 创建 Season
			seasonItem := ImportItem{
				Name:       fmt.Sprintf("Season %d", seasonNum),
				Type:       "Season",
				SeasonNumber: &seasonNum,
			}

			seasonID := uuid.New().String()
			seasonDBItem := &database.MediaItem{
				ID:           seasonID,
				LibraryID:    libraryID,
				ParentID:     &itemID,
				Type:         "Season",
				Name:         seasonItem.Name,
				SeasonNumber: &seasonNum,
			}

			if err := tx.Create(seasonDBItem).Error; err != nil {
				return err
			}

			// 创建 Episodes
			for _, episode := range season.Episodes {
				episodeNum := 1
				if episode.EpisodeNumber != nil {
					episodeNum = *episode.EpisodeNumber
				}

				episodeItem := &database.MediaItem{
					ID:                 uuid.New().String(),
					LibraryID:          libraryID,
					ParentID:           &seasonID,
					Type:               "Episode",
					Name:               episode.Name,
					Overview:           episode.Overview,
					SeasonNumber:       &seasonNum,
					EpisodeNumber:      &episodeNum,
				}

				if err := tx.Create(episodeItem).Error; err != nil {
					return err
				}

				// 添加 Episode 的播放源
				for _, src := range episode.Sources {
					source := &database.MediaSource{
						ID:        uuid.New().String(),
						ItemID:    episodeItem.ID,
						Name:      src.Name,
						URL:       src.URL,
						Protocol:  "Http",
						Container: src.Container,
						Bitrate:   src.Bitrate,
					}
					if err := tx.Create(source).Error; err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func generateImageTag(url string) string {
	// MD5 哈希 URL 并取前 8 个十六进制字符
	hash := md5.Sum([]byte(url))
	hashStr := hex.EncodeToString(hash[:])
	if len(hashStr) >= 8 {
		return hashStr[:8]
	}
	return hashStr
}

// adminAuth 管理接口认证中间件
func adminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-Api-Key")
		if apiKey == "" {
			apiKey = c.Query("api_key")
		}

		// 从配置中获取管理 API 密钥（简化：硬编码）
		// 实际应该从 config 中读取
		if apiKey != "change-me" { // TODO: 从 config 读取
			c.JSON(http.StatusUnauthorized, ErrUnauthorized)
			c.Abort()
			return
		}

		c.Next()
	}
}
