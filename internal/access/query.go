package access

import (
	"github.com/fakemby/fakemby/internal/database"
	"gorm.io/gorm"
)

// VisibleIDs evaluates ancestor restrictions once per request. A missing parent,
// cyclic hierarchy or malformed policy denies access rather than broadening it.
func VisibleIDs(db *gorm.DB, user *database.User) ([]string, error) {
	p, err := Normalize(user)
	if err != nil {
		return nil, err
	}
	var items []database.MediaItem
	if err = db.Find(&items).Error; err != nil {
		return nil, err
	}
	var libs []database.Library
	if err = db.Find(&libs).Error; err != nil {
		return nil, err
	}
	libraries := map[string]bool{}
	for _, l := range libs {
		libraries[l.ID] = true
	}
	byID := map[string]*database.MediaItem{}
	for i := range items {
		byID[items[i].ID] = &items[i]
	}
	state := map[string]int{}
	var allowed func(string) bool
	allowed = func(id string) bool {
		if state[id] != 0 {
			return state[id] == 2
		}
		state[id] = 1
		item := byID[id]
		if item == nil || !libraries[item.LibraryID] || !p.AllowsItem(item) {
			state[id] = 3
			return false
		}
		if item.ParentID != nil && *item.ParentID != "" && !allowed(*item.ParentID) {
			state[id] = 3
			return false
		}
		state[id] = 2
		return true
	}
	ids := []string{}
	for _, item := range items {
		if allowed(item.ID) {
			ids = append(ids, item.ID)
		}
	}
	return ids, nil
}
