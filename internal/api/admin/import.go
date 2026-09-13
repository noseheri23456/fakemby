package admin

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxImportBytes = 8 << 20
	maxImportItems = 1000
	maxImportNodes = 5000
	importAttempts = 3
)

type ImportRequest struct {
	Library string       `json:"library"`
	Items   []ImportItem `json:"items"`
	DryRun  bool         `json:"dry_run"`
}

type ImportItem struct {
	ID              string              `json:"Id"`
	ProviderIds     map[string]string   `json:"ProviderIds"`
	ParentID        *string             `json:"ParentId"`
	Name            string              `json:"name"`
	OriginalTitle   string              `json:"original_title"`
	Year            *int                `json:"year"`
	Type            string              `json:"type"`
	Overview        string              `json:"overview"`
	Genres          []string            `json:"genres"`
	Studios         []string            `json:"studios"`
	Countries       []string            `json:"countries"`
	Languages       []string            `json:"languages"`
	Tags            []string            `json:"tags"`
	Taglines        []string            `json:"taglines"`
	ExternalUrls    []ImportExternalUrl `json:"external_urls"`
	People          []ImportPerson      `json:"people"`
	CommunityRating *float64            `json:"community_rating"`
	OfficialRating  string              `json:"official_rating"`
	TMDBID          string              `json:"tmdb_id"`
	IMDBID          string              `json:"imdb_id"`
	TVDBID          string              `json:"tvdb_id"`
	RuntimeMinutes  *int64              `json:"runtime_minutes"`
	IsHidden        bool                `json:"is_hidden"`
	SeasonNumber    *int                `json:"season_number"`
	EpisodeNumber   *int                `json:"episode_number"`
	PremiereDate    *string             `json:"premiere_date"`
	Images          map[string]string   `json:"images"`
	Sources         []ImportSource      `json:"sources"`
	Subtitles       []ImportSubtitle    `json:"subtitles"`
	Seasons         []ImportSeason      `json:"seasons"`
}

type ImportPerson struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Role     string `json:"role"`
	ImageUrl string `json:"image_url"`
}

type ImportExternalUrl struct {
	Name string `json:"name"`
	Url  string `json:"url"`
}

type ImportSeason struct {
	ID           string            `json:"Id"`
	ProviderIds  map[string]string `json:"ProviderIds"`
	Name         string            `json:"name"`
	SeasonNumber *int              `json:"season_number"`
	Images       map[string]string `json:"images"`
	Episodes     []ImportEpisode   `json:"episodes"`
}

type ImportEpisode struct {
	ID             string            `json:"Id"`
	ProviderIds    map[string]string `json:"ProviderIds"`
	Name           string            `json:"name"`
	EpisodeNumber  *int              `json:"episode_number"`
	Overview       string            `json:"overview"`
	RuntimeMinutes *int64            `json:"runtime_minutes"`
	PremiereDate   *string           `json:"premiere_date"`
	Images         map[string]string `json:"images"`
	Sources        []ImportSource    `json:"sources"`
	Subtitles      []ImportSubtitle  `json:"subtitles"`
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
	Imported    int      `json:"imported"`
	WouldImport int      `json:"would_import"`
	DryRun      bool     `json:"dry_run"`
	Errors      []string `json:"errors"`
}

func RegisterImportRoutes(router *gin.Engine) {
	router.POST("/api/admin/import", adminAuth(), importBatch())
}

// decodeAdminJSON bounds input and rejects trailing JSON, including a second object.
func decodeAdminJSON(c *gin.Context, dst any, maxBytes int64) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("expected a single JSON value")
	}
	return nil
}

func importBatch() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ImportRequest
		if err := decodeAdminJSON(c, &req, maxImportBytes); err != nil {
			c.JSON(http.StatusBadRequest, ErrBadRequest)
			return
		}
		req.Library = strings.TrimSpace(req.Library)
		if req.Library == "" || len(req.Library) > 256 || len(req.Items) == 0 || len(req.Items) > maxImportItems {
			c.JSON(http.StatusBadRequest, gin.H{"Message": "library and 1-1000 items are required"})
			return
		}
		if raw, ok := c.GetQuery("dry_run"); ok {
			dry, err := strconv.ParseBool(raw)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"Message": "dry_run must be a boolean"})
				return
			}
			// A safety request in either location cannot be overridden by the other.
			req.DryRun = req.DryRun || dry
		}
		validation := make([]error, len(req.Items))
		nodes := 0
		for i := range req.Items {
			validation[i] = validateImportTree(&req.Items[i], &nodes)
		}
		if nodes > maxImportNodes {
			c.JSON(http.StatusBadRequest, gin.H{"Message": "import exceeds 5000 total items, seasons and episodes"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		var response ImportResponse
		err := retryImportLock(ctx, func() error {
			response = ImportResponse{DryRun: req.DryRun, Errors: []string{}}
			return database.GetWrite().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				lib, exists, err := lookupImportLibrary(tx, req.Library)
				if err != nil {
					return err
				}
				// Dry runs use the same resolver, but never execute a write statement.
				preview := map[string]database.MediaItem{}
				for i, item := range req.Items {
					if validation[i] != nil {
						response.Errors = append(response.Errors, fmt.Sprintf("items[%d]: %v", i, validation[i]))
						continue
					}
					planned := map[string]database.MediaItem{}
					plan, err := planImportTree(tx, lib.ID, item, item.ParentID, preview, planned)
					if err == nil && !req.DryRun {
						err = retryImportLock(ctx, func() error {
							return importSavepoint(tx, func() error {
								if !exists {
									if err := tx.Create(&lib).Error; err != nil {
										return err
									}
								}
								return writeImportTree(tx, plan)
							})
						})
					}
					if err != nil {
						// A stale SQLite snapshot requires a fresh transaction, not a partial commit.
						if isImportLock(err) || ctx.Err() != nil {
							return err
						}
						var fatal *importRollbackError
						if errors.As(err, &fatal) {
							return err
						}
						response.Errors = append(response.Errors, fmt.Sprintf("items[%d]: %v", i, err))
						continue
					}
					response.WouldImport++
					if req.DryRun {
						for id, row := range planned {
							preview[id] = row
						}
					} else {
						exists = true
						response.Imported++
					}
				}
				return nil
			})
		})
		if err != nil {
			slog.Error("Import transaction failed", "error", err)
			c.JSON(http.StatusInternalServerError, ErrInternal)
			return
		}
		c.JSON(http.StatusOK, response)
	}
}

func lookupImportLibrary(tx *gorm.DB, name string) (database.Library, bool, error) {
	var libraries []database.Library
	if err := tx.Where("name = ?", name).Limit(2).Find(&libraries).Error; err != nil {
		return database.Library{}, false, err
	}
	if len(libraries) > 1 {
		return database.Library{}, false, errors.New("ambiguous library name")
	}
	if len(libraries) == 1 {
		return libraries[0], true, nil
	}
	kind := "movies"
	if name == "TV Shows" {
		kind = "tvshows"
	}
	return database.Library{ID: importStableID("library", name), Name: name, Type: kind}, false, nil
}

func importStableID(parts ...string) string {
	data, _ := json.Marshal(parts)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:16])
}

func importProviders(item ImportItem) (map[string]string, error) {
	providers := map[string]string{}
	add := func(key, value string) error {
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		if key == "" || value == "" || len(key) > 64 || len(value) > 256 {
			return errors.New("ProviderIds keys and values must be nonempty bounded strings")
		}
		if old, ok := providers[key]; ok && old != value {
			return fmt.Errorf("conflicting provider %q", key)
		}
		providers[key] = value
		return nil
	}
	if len(item.ProviderIds) > 32 {
		return nil, errors.New("too many ProviderIds")
	}
	for key, value := range item.ProviderIds {
		if err := add(key, value); err != nil {
			return nil, err
		}
	}
	for key, value := range map[string]string{"tmdb": item.TMDBID, "imdb": item.IMDBID, "tvdb": item.TVDBID} {
		if value != "" {
			if err := add(key, value); err != nil {
				return nil, err
			}
		}
	}
	return providers, nil
}

func validateImportTree(item *ImportItem, nodes *int) error {
	*nodes++
	// Count the entire tree even if a parent is invalid, so invalid input cannot bypass limits.
	for _, season := range item.Seasons {
		*nodes += 1 + len(season.Episodes)
	}
	if err := validateImportItem(item); err != nil {
		return err
	}
	if len(item.Seasons) > 0 && item.Type != "Series" {
		return errors.New("only Series can contain seasons")
	}
	seenSeasons := map[int]bool{}
	for _, season := range item.Seasons {
		si := season.importItem()
		if err := validateImportItem(&si); err != nil {
			return fmt.Errorf("season: %w", err)
		}
		if si.SeasonNumber == nil || seenSeasons[*si.SeasonNumber] {
			return errors.New("seasons require distinct season_number values")
		}
		seenSeasons[*si.SeasonNumber] = true
		seenEpisodes := map[int]bool{}
		for _, episode := range season.Episodes {
			ei := episode.importItem(si.SeasonNumber)
			if err := validateImportItem(&ei); err != nil {
				return fmt.Errorf("episode: %w", err)
			}
			if ei.EpisodeNumber == nil || seenEpisodes[*ei.EpisodeNumber] {
				return errors.New("episodes require distinct episode_number values")
			}
			seenEpisodes[*ei.EpisodeNumber] = true
		}
	}
	return nil
}

func validateImportItem(item *ImportItem) error {
	item.Name = strings.TrimSpace(item.Name)
	if item.Name == "" || len(item.Name) > 1024 {
		return errors.New("name is required and must not exceed 1024 bytes")
	}
	switch item.Type {
	case "Movie", "Series", "Season", "Episode":
	default:
		return errors.New("type must be Movie, Series, Season or Episode")
	}
	if len(item.ID) > 128 || (item.ID != "" && strings.TrimSpace(item.ID) != item.ID) {
		return errors.New("invalid Id")
	}
	if item.ParentID != nil && (strings.TrimSpace(*item.ParentID) == "" || len(*item.ParentID) > 128) {
		return errors.New("invalid ParentId")
	}
	if _, err := importProviders(*item); err != nil {
		return err
	}
	if item.Year != nil && (*item.Year < 1 || *item.Year > 9999) {
		return errors.New("year must be between 1 and 9999")
	}
	if item.SeasonNumber != nil && *item.SeasonNumber < 0 || item.EpisodeNumber != nil && *item.EpisodeNumber < 0 {
		return errors.New("season_number and episode_number must not be negative")
	}
	if item.Type == "Season" && item.SeasonNumber == nil || item.Type == "Episode" && item.EpisodeNumber == nil {
		return errors.New("Season/Episode requires its number")
	}
	if item.RuntimeMinutes != nil && (*item.RuntimeMinutes < 0 || *item.RuntimeMinutes > math.MaxInt64/600_000_000) {
		return errors.New("runtime_minutes is out of range")
	}
	if item.CommunityRating != nil && (*item.CommunityRating < 0 || *item.CommunityRating > 10 || math.IsNaN(*item.CommunityRating) || math.IsInf(*item.CommunityRating, 0)) {
		return errors.New("community_rating must be between 0 and 10")
	}
	if item.PremiereDate != nil && *item.PremiereDate != "" {
		if _, err := time.Parse(time.RFC3339, *normalizePremiereDateFromStr(item.PremiereDate)); err != nil {
			return errors.New("premiere_date must be a valid ISO 8601 date")
		}
	}
	if len(item.Sources) > 200 || len(item.Subtitles) > 200 || len(item.Images) > 20 || len(item.People) > 500 {
		return errors.New("too many sources, subtitles, images or people")
	}
	for _, src := range item.Sources {
		if err := validateImportURL(src.URL); err != nil {
			return fmt.Errorf("source: %w", err)
		}
		for _, n := range []*int{src.Width, src.Height, src.Bitrate} {
			if n != nil && *n < 0 {
				return errors.New("source dimensions and bitrate must not be negative")
			}
		}
	}
	for kind, raw := range item.Images {
		switch kind {
		case "Primary", "Backdrop", "Logo", "Thumb", "Banner", "Art", "Disc", "Box", "BoxRear", "Menu", "Screenshot":
		default:
			return fmt.Errorf("unsupported image type %q", kind)
		}
		if err := validateImportURL(raw); err != nil {
			return fmt.Errorf("image: %w", err)
		}
	}
	for _, sub := range item.Subtitles {
		if err := validateImportURL(sub.URL); err != nil {
			return fmt.Errorf("subtitle: %w", err)
		}
	}
	for _, link := range item.ExternalUrls {
		if err := validateImportURL(link.Url); err != nil {
			return fmt.Errorf("external URL: %w", err)
		}
	}
	for _, person := range item.People {
		if strings.TrimSpace(person.Name) == "" {
			return errors.New("person name is required")
		}
		if person.ImageUrl != "" {
			if err := validateImportURL(person.ImageUrl); err != nil {
				return fmt.Errorf("person image: %w", err)
			}
		}
	}
	return nil
}

func validateImportURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 8192 || u == nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return errors.New("URL must be absolute HTTP(S), without embedded credentials")
	}
	return nil
}

func (s ImportSeason) importItem() ImportItem {
	name := s.Name
	if name == "" && s.SeasonNumber != nil {
		name = fmt.Sprintf("Season %d", *s.SeasonNumber)
	}
	return ImportItem{ID: s.ID, ProviderIds: s.ProviderIds, Name: name, Type: "Season", SeasonNumber: s.SeasonNumber, Images: s.Images}
}

func (e ImportEpisode) importItem(season *int) ImportItem {
	return ImportItem{ID: e.ID, ProviderIds: e.ProviderIds, Name: e.Name, Type: "Episode", SeasonNumber: season,
		EpisodeNumber: e.EpisodeNumber, Overview: e.Overview, Sources: e.Sources, Images: e.Images,
		Subtitles: e.Subtitles, RuntimeMinutes: e.RuntimeMinutes, PremiereDate: e.PremiereDate}
}

type importPlan struct {
	input    ImportItem
	row      database.MediaItem
	exists   bool
	children []*importPlan
}

func importScope(tx *gorm.DB, library, kind string, parent *string) *gorm.DB {
	q := tx.Where("library_id = ? AND type = ?", library, kind)
	if parent == nil {
		return q.Where("parent_id IS NULL OR parent_id = ''")
	}
	return q.Where("parent_id = ?", *parent)
}

func sameImportScope(row database.MediaItem, library, kind string, parent *string) bool {
	p, rp := "", ""
	if parent != nil {
		p = *parent
	}
	if row.ParentID != nil {
		rp = *row.ParentID
	}
	return row.LibraryID == library && row.Type == kind && p == rp
}

func storedImportProviders(row database.MediaItem) map[string]string {
	var raw map[string]string
	_ = json.Unmarshal([]byte(row.ProviderIds), &raw)
	out := map[string]string{}
	for key, value := range raw {
		out[strings.ToLower(key)] = value
	}
	for key, value := range map[string]string{"tmdb": row.TMDBID, "imdb": row.IMDBID, "tvdb": row.TVDBID} {
		if value != "" {
			out[key] = value
		}
	}
	return out
}

// resolveImportIdentity rejects ambiguous matches instead of merging unrelated items.
func resolveImportIdentity(tx *gorm.DB, library string, item ImportItem, parent *string, preview, planned map[string]database.MediaItem) (database.MediaItem, bool, error) {
	providers, err := importProviders(item)
	if err != nil {
		return database.MediaItem{}, false, err
	}
	matches := map[string]database.MediaItem{}
	if len(providers) > 0 {
		conditions := []string{}
		args := []any{}
		keys := make([]string, 0, len(providers))
		for key := range providers {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			conditions = append(conditions, "EXISTS (SELECT 1 FROM json_each(CASE WHEN json_valid(provider_ids) THEN provider_ids ELSE '{}' END) WHERE lower(key) = ? AND value = ?)")
			args = append(args, key, providers[key])
			if key == "tmdb" || key == "imdb" || key == "tvdb" {
				conditions = append(conditions, tx.NamingStrategy.ColumnName("media_items", map[string]string{"tmdb": "TMDBID", "imdb": "IMDBID", "tvdb": "TVDBID"}[key])+" = ?")
				args = append(args, providers[key])
			}
		}
		var rows []database.MediaItem
		if err := importScope(tx, library, item.Type, parent).Where(strings.Join(conditions, " OR "), args...).Limit(2).Find(&rows).Error; err != nil {
			return database.MediaItem{}, false, err
		}
		for _, row := range rows {
			matches[row.ID] = row
		}
		for _, overlay := range []map[string]database.MediaItem{preview, planned} {
			for _, row := range overlay {
				if sameImportScope(row, library, item.Type, parent) {
					stored := storedImportProviders(row)
					for key, value := range providers {
						if stored[key] == value {
							matches[row.ID] = row
						}
					}
				}
			}
		}
	}
	id := item.ID
	if id == "" {
		p := ""
		if parent != nil {
			p = *parent
		}
		identity := ""
		if len(providers) != 0 {
			data, _ := json.Marshal(providers)
			identity = string(data)
		} else if item.Type == "Season" && item.SeasonNumber != nil {
			identity = fmt.Sprintf("season:%d", *item.SeasonNumber)
		} else if item.Type == "Episode" && item.EpisodeNumber != nil {
			identity = fmt.Sprintf("episode:%d", *item.EpisodeNumber)
		} else {
			identity = item.Name
			if item.Year != nil {
				identity += fmt.Sprintf(":%d", *item.Year)
			}
		}
		id = importStableID("item", library, item.Type, p, identity)
	}
	var existing database.MediaItem
	err = tx.Where("id = ?", id).First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return database.MediaItem{}, false, err
	}
	found := err == nil
	for _, overlay := range []map[string]database.MediaItem{preview, planned} {
		if row, ok := overlay[id]; ok {
			existing, found = row, true
		}
	}
	if found {
		if !sameImportScope(existing, library, item.Type, parent) {
			return database.MediaItem{}, false, errors.New("id belongs to a different library, type or parent")
		}
		matches[id] = existing
	}
	if len(matches) > 1 {
		return database.MediaItem{}, false, errors.New("Id/ProviderIds match multiple items")
	}
	for _, row := range matches {
		if item.ID != "" && row.ID != item.ID {
			return database.MediaItem{}, false, errors.New("id conflicts with the existing provider identity")
		}
		if item.ID == "" {
			stored := storedImportProviders(row)
			for key, value := range providers {
				if old := stored[key]; old != "" && old != value {
					return database.MediaItem{}, false, errors.New("conflicting ProviderIds; use the existing Id to change providers")
				}
			}
		}
		return row, true, nil
	}
	return database.MediaItem{ID: id, LibraryID: library, Type: item.Type, ParentID: parent}, false, nil
}

func planImportTree(tx *gorm.DB, library string, item ImportItem, parent *string, preview, planned map[string]database.MediaItem) (*importPlan, error) {
	if parent != nil {
		var row database.MediaItem
		var ok bool
		if row, ok = planned[*parent]; !ok {
			if row, ok = preview[*parent]; !ok {
				if err := tx.Where("id = ?", *parent).First(&row).Error; err != nil {
					return nil, fmt.Errorf("parent not found: %w", err)
				}
			}
		}
		if row.LibraryID != library || (item.Type == "Season" && row.Type != "Series") || (item.Type == "Episode" && row.Type != "Season") || item.Type == "Movie" || item.Type == "Series" {
			return nil, errors.New("invalid parent type or library")
		}
	} else if item.Type == "Season" || item.Type == "Episode" {
		return nil, errors.New("Season/Episode requires ParentId or nesting")
	}
	row, exists, err := resolveImportIdentity(tx, library, item, parent, preview, planned)
	if err != nil {
		return nil, err
	}
	if _, duplicate := planned[row.ID]; duplicate {
		return nil, errors.New("duplicate identity within item tree")
	}
	providers := storedImportProviders(row)
	incoming, _ := importProviders(item)
	for key, value := range incoming {
		providers[key] = value
	}
	providerJSON, _ := json.Marshal(providers)
	row.ProviderIds = string(providerJSON)
	row.TMDBID, row.IMDBID, row.TVDBID = providers["tmdb"], providers["imdb"], providers["tvdb"]
	row.Name, row.OriginalTitle, row.Overview = item.Name, item.OriginalTitle, item.Overview
	row.Year, row.PremiereDate = item.Year, normalizePremiereDateFromStr(item.PremiereDate)
	row.CommunityRating, row.OfficialRating, row.IsHidden = item.CommunityRating, item.OfficialRating, item.IsHidden
	row.SeasonNumber, row.EpisodeNumber = item.SeasonNumber, item.EpisodeNumber
	row.RuntimeTicks = nil
	if item.RuntimeMinutes != nil {
		ticks := *item.RuntimeMinutes * 600_000_000
		row.RuntimeTicks = &ticks
	}
	for _, field := range []struct {
		dst   *string
		value any
	}{
		{&row.Genres, item.Genres}, {&row.Studios, item.Studios}, {&row.Tags, item.Tags},
		{&row.Taglines, item.Taglines}, {&row.ExternalUrls, item.ExternalUrls},
		{&row.Countries, item.Countries}, {&row.Languages, item.Languages},
	} {
		data, _ := json.Marshal(field.value)
		*field.dst = string(data)
	}
	planned[row.ID] = row
	plan := &importPlan{input: item, row: row, exists: exists}
	for _, season := range item.Seasons {
		si := season.importItem()
		sub, err := planImportTree(tx, library, si, &row.ID, preview, planned)
		if err != nil {
			return nil, err
		}
		for _, episode := range season.Episodes {
			ep, err := planImportTree(tx, library, episode.importItem(si.SeasonNumber), &sub.row.ID, preview, planned)
			if err != nil {
				return nil, err
			}
			sub.children = append(sub.children, ep)
		}
		plan.children = append(plan.children, sub)
	}
	return plan, nil
}

func writeImportTree(tx *gorm.DB, plan *importPlan) error {
	item, row := plan.input, plan.row
	virtual := func(prefix, name, kind string) (string, error) {
		// 规则统一收敛到 database.EnsureVirtualItem（见 internal/database/virtual.go）
		return database.EnsureVirtualItem(tx, prefix, name, kind)
	}
	for _, genre := range item.Genres {
		if _, err := virtual("genre", genre, "Genre"); err != nil {
			return err
		}
	}
	for _, studio := range item.Studios {
		if _, err := virtual("studio", studio, "Studio"); err != nil {
			return err
		}
	}
	people := make([]map[string]any, 0, len(item.People))
	for _, person := range item.People {
		id, err := virtual("person", person.Name, "Person")
		if err != nil {
			return err
		}
		info := map[string]any{"Name": person.Name, "Type": person.Type, "Id": id, "Role": person.Role}
		if person.ImageUrl != "" {
			if err := tx.Where("item_id = ? AND type = ?", id, "Primary").Delete(&database.Image{}).Error; err != nil {
				return err
			}
			tag := generateImageTag(person.ImageUrl)
			if err := tx.Create(&database.Image{ItemID: id, Type: "Primary", URL: person.ImageUrl, Tag: tag}).Error; err != nil {
				return err
			}
			info["PrimaryImageTag"] = tag
		}
		people = append(people, info)
	}
	data, _ := json.Marshal(people)
	row.People = string(data)
	if plan.exists {
		// Select all owned metadata, including zero values, without replacing the row or its creation time.
		if err := tx.Model(&database.MediaItem{}).Where("id = ?", row.ID).
			Select("Name", "OriginalTitle", "Overview", "Year", "PremiereDate", "CommunityRating", "OfficialRating",
				"Genres", "Studios", "Countries", "Languages", "Tags", "Taglines", "ExternalUrls", "People", "ProviderIds", "TMDBID", "IMDBID", "TVDBID",
				"RuntimeTicks", "IsHidden", "SeasonNumber", "EpisodeNumber", "DateModified").Updates(&row).Error; err != nil {
			return err
		}
	} else if err := tx.Create(&row).Error; err != nil {
		return err
	}
	// The supplied arrays are authoritative, including omitted/empty arrays. Progress is never touched.
	for _, model := range []any{&database.MediaSource{}, &database.Image{}, &database.Subtitle{}} {
		if err := tx.Where("item_id = ?", row.ID).Delete(model).Error; err != nil {
			return err
		}
	}
	for i, src := range item.Sources {
		source := database.MediaSource{ID: importStableID("source", row.ID, strconv.Itoa(i)), ItemID: row.ID,
			Name: src.Name, URL: src.URL, Protocol: "Http", Container: src.Container, Bitrate: src.Bitrate, SortOrder: i}
		if err := tx.Create(&source).Error; err != nil {
			return err
		}
	}
	for kind, raw := range item.Images {
		if err := tx.Create(&database.Image{ItemID: row.ID, Type: kind, URL: raw, Tag: generateImageTag(raw)}).Error; err != nil {
			return err
		}
	}
	for _, sub := range item.Subtitles {
		if err := tx.Create(&database.Subtitle{ItemID: row.ID, Language: sub.Language, Title: sub.Title, URL: sub.URL, Codec: sub.Codec}).Error; err != nil {
			return err
		}
	}
	for _, child := range plan.children {
		if err := writeImportTree(tx, child); err != nil {
			return err
		}
	}
	return nil
}

type importRollbackError struct{ err error }

func (e *importRollbackError) Error() string { return e.err.Error() }
func (e *importRollbackError) Unwrap() error { return e.err }

func importSavepoint(tx *gorm.DB, write func() error) error {
	if err := tx.Exec("SAVEPOINT import_item").Error; err != nil {
		return &importRollbackError{err}
	}
	if err := write(); err != nil {
		if rollback := tx.Exec("ROLLBACK TO SAVEPOINT import_item").Error; rollback != nil {
			return &importRollbackError{rollback}
		}
		if release := tx.Exec("RELEASE SAVEPOINT import_item").Error; release != nil {
			return &importRollbackError{release}
		}
		return err
	}
	if err := tx.Exec("RELEASE SAVEPOINT import_item").Error; err != nil {
		return &importRollbackError{err}
	}
	return nil
}

func isImportLock(err error) bool {
	var sqliteError interface{ Code() int }
	if errors.As(err, &sqliteError) {
		code := sqliteError.Code() & 0xff
		return code == 5 || code == 6 // SQLITE_BUSY / SQLITE_LOCKED, including extended codes.
	}
	return false
}

func retryImportLock(ctx context.Context, fn func() error) error {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := fn()
		var fatal *importRollbackError
		if err == nil || errors.As(err, &fatal) || !isImportLock(err) || attempt+1 >= importAttempts {
			return err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func generateImageTag(raw string) string {
	hash := md5.Sum([]byte(raw))
	return hex.EncodeToString(hash[:])[:8]
}

func normalizePremiereDateFromStr(dateStr *string) *string {
	if dateStr == nil || *dateStr == "" {
		return dateStr
	}
	d := *dateStr
	if len(d) == 10 && d[4] == '-' && d[7] == '-' {
		normalized := d + "T00:00:00Z"
		return &normalized
	}
	return dateStr
}
