package service

import (
	"github.com/mozillazg/go-pinyin"
	"html"
	"sort"
	"strings"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/repo"
	"gorm.io/gorm"
)

type SearchService struct {
	repository repo.Search
}

func NewSearchService(db *gorm.DB) *SearchService {
	return NewSearchServiceWithRepository(repo.NewSearch(db))
}

// SearchItems 全文搜索媒体项目
func (s *SearchService) SearchItems(term string, kinds []string, start, limit int) ([]database.MediaItem, int64, error) {
	rows, err := s.repository.Candidates(kinds)
	if err != nil {
		return nil, 0, err
	}
	type hit struct {
		item  database.MediaItem
		score int
	}
	hits := []hit{}
	needle := strings.ToLower(strings.TrimSpace(term))
	compact := strings.ReplaceAll(needle, " ", "")
	for _, item := range rows {
		score := 0
		name := strings.ToLower(item.Name)
		switch {
		case name == needle:
			score = 100
		case strings.HasPrefix(name, needle):
			score = 80
		case strings.Contains(name, needle):
			score = 60
		case strings.Contains(strings.ToLower(item.OriginalTitle+" "+item.Tags), needle):
			score = 50
		case strings.Contains(strings.ToLower(item.Overview), needle):
			score = 10
		}
		syllables := pinyin.LazyPinyin(item.Name, pinyin.NewArgs())
		initials := ""
		for _, p := range syllables {
			if len(p) > 0 {
				initials += p[:1]
			}
		}
		if compact != "" && (strings.Contains(strings.Join(syllables, ""), compact) || strings.Contains(initials, compact)) {
			score = max(score, 40)
		}
		if score > 0 {
			switch item.Type {
			case "Movie", "Series":
				score += 5
			case "Episode":
				score += 2
			}
			hits = append(hits, hit{item, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		if hits[i].item.Name != hits[j].item.Name {
			return hits[i].item.Name < hits[j].item.Name
		}
		return hits[i].item.ID < hits[j].item.ID
	})
	total := len(hits)
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	end := min(start+limit, total)
	items := []database.MediaItem{}
	for _, h := range hits[start:end] {
		items = append(items, h.item)
	}
	return items, int64(total), nil
}

// HighlightName returns escaped markup; unmatched aliases return plain escaped text.
func HighlightName(name, term string) string {
	if term == "" {
		return html.EscapeString(name)
	}
	i := strings.Index(strings.ToLower(name), strings.ToLower(term))
	if i < 0 {
		return html.EscapeString(name)
	}
	end := min(i+len(term), len(name))
	return html.EscapeString(name[:i]) + "<mark>" + html.EscapeString(name[i:end]) + "</mark>" + html.EscapeString(name[end:])
}

// FindSimilarItems 查找相似项目（按流派、年份、类型）
func (s *SearchService) FindSimilarItems(id string, limit int) ([]database.MediaItem, error) {
	return s.repository.FindSimilarItems(id, limit)
}
func NewSearchServiceWithRepository(r repo.Search) *SearchService {
	return &SearchService{repository: r}
}
