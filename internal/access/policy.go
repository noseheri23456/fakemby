// Package access implements fail-closed user media authorization.
package access

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/fakemby/fakemby/internal/database"
)

var ErrInvalidPolicy = errors.New("invalid user policy")

// Policy is the normalized authorization subset of the stored Emby policy.
// An empty legacy policy uses the same defaults as a newly created user.
type Policy struct {
	IsDisabled              bool
	IsHidden                bool
	IsHiddenRemotely        bool
	EnableAllFolders        bool
	EnabledFolders          []string
	BlockedMediaFolders     []string
	MaxParentalRating       *int
	BlockUnratedItems       []string
	EnableMediaPlayback     bool
	SimultaneousStreamLimit int
}

// Normalize is pure. Malformed values never fall back to permissive defaults.
// IsAdministrator is intentionally ignored: only User.IsAdmin is authoritative.
func Normalize(user *database.User) (Policy, error) {
	if user == nil {
		return Policy{}, ErrInvalidPolicy
	}
	p := Policy{EnableAllFolders: true, EnableMediaPlayback: true}
	raw := strings.TrimSpace(user.Policy)
	if raw == "" {
		return p, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return Policy{}, ErrInvalidPolicy
	}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return Policy{}, ErrInvalidPolicy
		}
		name, ok := key.(string)
		if !ok {
			return Policy{}, ErrInvalidPolicy
		}
		name = strings.ToLower(name)
		if seen[name] {
			return Policy{}, ErrInvalidPolicy
		}
		seen[name] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return Policy{}, ErrInvalidPolicy
		}
		var target any
		allowNull := false
		switch name {
		case "isdisabled":
			target = &p.IsDisabled
		case "ishidden":
			target = &p.IsHidden
		case "ishiddenremotely":
			target = &p.IsHiddenRemotely
		case "enableallfolders":
			target = &p.EnableAllFolders
		case "enablemediaplayback":
			target = &p.EnableMediaPlayback
		case "enabledfolders":
			target, allowNull = &p.EnabledFolders, true
		case "blockedmediafolders":
			target, allowNull = &p.BlockedMediaFolders, true
		case "blockunrateditems":
			target, allowNull = &p.BlockUnratedItems, true
		case "maxparentalrating":
			target, allowNull = &p.MaxParentalRating, true
		case "simultaneousstreamlimit":
			target = &p.SimultaneousStreamLimit
		}
		if target != nil && ((!allowNull && string(value) == "null") || json.Unmarshal(value, target) != nil) {
			return Policy{}, ErrInvalidPolicy
		}
	}
	if _, err := decoder.Token(); err != nil {
		return Policy{}, ErrInvalidPolicy
	}
	if _, err := decoder.Token(); err != io.EOF {
		return Policy{}, ErrInvalidPolicy
	}
	if p.SimultaneousStreamLimit < 0 || (p.MaxParentalRating != nil && *p.MaxParentalRating < 0) {
		return Policy{}, ErrInvalidPolicy
	}
	for _, list := range [][]string{p.EnabledFolders, p.BlockedMediaFolders, p.BlockUnratedItems} {
		for i, value := range list {
			list[i] = strings.TrimSpace(value)
			if list[i] == "" {
				return Policy{}, ErrInvalidPolicy
			}
		}
	}
	for i, kind := range p.BlockUnratedItems {
		kind = strings.ToLower(kind)
		if _, ok := unratedTypes[kind]; !ok {
			return Policy{}, ErrInvalidPolicy
		}
		p.BlockUnratedItems[i] = kind
	}
	return p, nil
}

func (p Policy) AllowsLibrary(libraryID string) bool {
	if p.IsDisabled || libraryID == "" || contains(p.BlockedMediaFolders, libraryID) {
		return false
	}
	return p.EnableAllFolders || contains(p.EnabledFolders, libraryID)
}

// AllowsItem checks an item's own fields. CanAccessItem and Scope additionally
// deny descendants of inaccessible ancestors and require an existing library.
func (p Policy) AllowsItem(item *database.MediaItem) bool {
	if item == nil || item.IsHidden || !p.AllowsLibrary(item.LibraryID) {
		return false
	}
	rating := strings.ToUpper(strings.TrimSpace(item.OfficialRating))
	level, rated := ratingLevels[rating]
	if !rated {
		if rating != "" && rating != "NR" && rating != "UNRATED" && rating != "NOT RATED" && p.MaxParentalRating != nil {
			return false
		}
		return !contains(p.blockedTypes(), strings.ToLower(item.Type))
	}
	return p.MaxParentalRating == nil || level <= *p.MaxParentalRating
}

// VirtualTypes 是不属于任何媒体库的元数据索引条目（type = Genre / Studio / Person）。
// 它们的 library_id 恒为空，所以任何"必须属于某个库"的判定都会把客户端对演员页、
// 分类页的访问一并拒掉。这里集中列出，避免各处各写一份字符串字面量。
var VirtualTypes = []string{"genre", "studio", "person"}

func IsVirtualType(kind string) bool {
	return contains(VirtualTypes, strings.ToLower(strings.TrimSpace(kind)))
}

func IsPlaybackAllowed(user *database.User) bool {
	p, err := Normalize(user)
	return err == nil && !p.IsDisabled && p.EnableMediaPlayback
}

// SessionLimit returns 0 for unlimited, and -1 for invalid/disabled policies.
// Callers must check IsPlaybackAllowed before enforcing a positive limit.
func SessionLimit(user *database.User) int {
	p, err := Normalize(user)
	if err != nil || p.IsDisabled {
		return -1
	}
	return p.SimultaneousStreamLimit
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// Emby parental levels, not ages: PG-13=7, TV-14=8, R/TV-MA=9.
var ratingLevels = map[string]int{
	"G": 1, "TV-Y": 1, "TV-G": 1, "TV-Y7": 3, "TV-Y7-FV": 3,
	"PG": 5, "TV-PG": 5, "PG-13": 7, "TV-14": 8, "R": 9, "TV-MA": 9, "NC-17": 10,
}

var unratedTypes = map[string][]string{
	"all":   {"movie", "series", "season", "episode", "trailer", "music", "audio", "musicalbum", "musicartist", "musicvideo", "video", "book", "game", "channel", "livetv", "program", "folder", "boxset", "other"},
	"movie": {"movie"}, "series": {"series", "season", "episode"},
	"season": {"season"}, "episode": {"episode"}, "trailer": {"trailer"},
	"music": {"music", "audio", "musicalbum", "musicartist", "musicvideo"},
	"audio": {"audio"}, "musicvideo": {"musicvideo"}, "video": {"video"},
	"book": {"book"}, "game": {"game"}, "channel": {"channel"},
	"livetv": {"livetv", "program"}, "other": {"folder", "boxset", "other"},
}

func (p Policy) blockedTypes() []string {
	var kinds []string
	for _, kind := range p.BlockUnratedItems {
		kinds = append(kinds, unratedTypes[kind]...)
	}
	return kinds
}
