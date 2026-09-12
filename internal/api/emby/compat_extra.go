package emby

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/types"
	"github.com/gin-gonic/gin"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

func registerExtraCompat(r *gin.Engine, cfg *config.Config) {
	auth := AuthTokenMiddleware(cfg.TokenExpiryDays())
	r.GET("/emby/Shows/NextUp", auth, nextUp())
	r.GET("/emby/Items/:itemId/SpecialFeatures", auth, emptyArrayHandler())
	r.GET("/emby/Videos/:itemId/AdditionalParts", auth, emptyItemsHandler())
	r.GET("/emby/Items/:itemId/Intros", auth, emptyItemsHandler())
	r.GET("/emby/Items/Filters", auth, itemFilters())
	r.GET("/emby/Channels", auth, emptyItemsHandler())
	r.GET("/emby/QuickConnect/Enabled", func(c *gin.Context) { c.JSON(200, false) })
	r.GET("/emby/Genres", auth, taxonomy("Genre"))
	r.GET("/emby/Studios", auth, taxonomy("Studio"))
	r.GET("/emby/Persons/:itemId", auth, func(c *gin.Context) {
		svc := scopedMediaService(c)
		item, err := svc.GetItemByID(c.Param("itemId"))
		if err != nil {
			c.JSON(404, ErrNotFound)
			return
		}
		c.JSON(200, svc.ItemToDTO(item, c.GetString("user_id"), nil))
	})
	r.GET("/emby/Playlists", auth, emptyItemsHandler())
	r.GET("/emby/Collections", auth, emptyItemsHandler())
}
func pageBounds(c *gin.Context) (int, int, bool) {
	start, e1 := strconv.Atoi(c.DefaultQuery("StartIndex", "0"))
	limit, e2 := strconv.Atoi(c.DefaultQuery("Limit", "100"))
	if e1 != nil || e2 != nil || start < 0 || limit < 1 || limit > 500 {
		c.JSON(http.StatusBadRequest, ErrBadRequest)
		return 0, 0, false
	}
	return start, limit, true
}
func nextUp() gin.HandlerFunc {
	return func(c *gin.Context) {
		start, limit, ok := pageBounds(c)
		if !ok {
			return
		}
		db := scopedMediaDB(c)
		svc := scopedMediaService(c)
		var episodes []database.MediaItem
		q := db.Where("type = ?", "Episode")
		if id := c.Query("SeriesId"); id != "" {
			q = q.Where("parent_id IN (SELECT id FROM media_items WHERE parent_id = ?)", id)
		}
		if id := c.Query("ParentId"); id != "" {
			q = q.Where("library_id = ?", id)
		}
		if q.Order("season_number, episode_number, id").Find(&episodes).Error != nil {
			c.JSON(500, ErrInternal)
			return
		}
		var progress []database.PlayProgress
		if database.Get().Where("user_id = ?", c.GetString("user_id")).Find(&progress).Error != nil {
			c.JSON(500, ErrInternal)
			return
		}
		played := map[string]bool{}
		for _, p := range progress {
			played[p.ItemID] = p.IsPlayed
		}
		seen := map[string]bool{}
		out := []types.BaseItemDto{}
		for _, ep := range episodes {
			if played[ep.ID] || ep.ParentID == nil {
				continue
			}
			var season database.MediaItem
			if database.Get().Where("id = ?", *ep.ParentID).First(&season).Error != nil || season.ParentID == nil {
				continue
			}
			sid := *season.ParentID
			if seen[sid] {
				continue
			}
			seen[sid] = true
			out = append(out, *svc.ItemToDTO(&ep, c.GetString("user_id"), nil))
		}
		total := len(out)
		if start > total {
			start = total
		}
		end := start + limit
		if end > total {
			end = total
		}
		c.JSON(200, types.ItemsResponse{Items: out[start:end], TotalRecordCount: total, StartIndex: start})
	}
}
func itemFilters() gin.HandlerFunc {
	return func(c *gin.Context) {
		var rows []database.MediaItem
		q := scopedMediaDB(c)
		if id := c.Query("ParentId"); id != "" {
			q = q.Where("library_id = ?", id)
		}
		if q.Find(&rows).Error != nil {
			c.JSON(500, ErrInternal)
			return
		}
		genres := map[string]bool{}
		years := map[int]bool{}
		for _, r := range rows {
			var gs []string
			_ = json.Unmarshal([]byte(r.Genres), &gs)
			for _, g := range gs {
				genres[g] = true
			}
			if r.Year != nil {
				years[*r.Year] = true
			}
		}
		g := []string{}
		y := []int{}
		for v := range genres {
			g = append(g, v)
		}
		for v := range years {
			y = append(y, v)
		}
		sort.Strings(g)
		sort.Ints(y)
		c.JSON(200, gin.H{"Genres": g, "Years": y, "Tags": []string{}, "OfficialRatings": []string{}})
	}
}
func taxonomy(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		start, limit, ok := pageBounds(c)
		if !ok {
			return
		}
		var rows []database.MediaItem
		if scopedMediaDB(c).Find(&rows).Error != nil {
			c.JSON(500, ErrInternal)
			return
		}
		names := map[string]bool{}
		for _, r := range rows {
			raw := r.Genres
			if kind == "Studio" {
				raw = r.Studios
			}
			var vals []string
			_ = json.Unmarshal([]byte(raw), &vals)
			for _, v := range vals {
				names[v] = true
			}
		}
		sorted := []string{}
		for n := range names {
			sorted = append(sorted, n)
		}
		sort.Strings(sorted)
		total := len(sorted)
		if start > total {
			start = total
		}
		end := start + limit
		if end > total {
			end = total
		}
		out := []types.BaseItemDto{}
		for _, n := range sorted[start:end] {
			out = append(out, types.BaseItemDto{ID: fmt.Sprintf("%x", md5.Sum([]byte(strings.ToLower(kind)+":"+n))), Name: n, Type: kind, IsFolder: true})
		}
		c.JSON(200, types.ItemsResponse{Items: out, TotalRecordCount: total, StartIndex: start})
	}
}
