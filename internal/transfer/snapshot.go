// Package transfer reads and writes versioned, transactional migration snapshots.
// Snapshots contain password hashes and source URLs: treat them as sensitive backups.
package transfer

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"gorm.io/gorm"
	"io"
	"time"
)

type Snapshot struct {
	Version   int                         `json:"version"`
	CreatedAt time.Time                   `json:"created_at"`
	Config    config.Config               `json:"config"`
	Libraries []database.Library          `json:"libraries"`
	Items     []database.MediaItem        `json:"items"`
	Sources   []database.MediaSource      `json:"sources"`
	Images    []database.Image            `json:"images"`
	Subtitles []database.Subtitle         `json:"subtitles"`
	Users     []database.User             `json:"users"`
	Progress  []database.PlayProgress     `json:"progress"`
	Activity  []database.PlaybackActivity `json:"activity"`
}

func Export(db *gorm.DB, cfg *config.Config, w io.Writer) error {
	s := Snapshot{Version: 1, CreatedAt: time.Now().UTC(), Config: *cfg}
	s.Config.Admin.APIKey = ""
	s.Config.Playback.SignKey = ""
	s.Config.Database.DSN = ""
	s.Config.Database.Path = "./fakemby.db"
	s.Config.Image.CacheDir = "./cache/images"
	s.Config.Log.File = ""
	s.Config.Playback.STRMRoot = ""
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, dest := range []any{&s.Libraries, &s.Items, &s.Sources, &s.Images, &s.Subtitles, &s.Users, &s.Progress, &s.Activity} {
			if err := tx.Find(dest).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}
func Import(db *gorm.DB, r io.Reader) (*config.Config, error) {
	data, err := io.ReadAll(io.LimitReader(r, 256*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 256*1024*1024 {
		return nil, errors.New("snapshot exceeds 256 MiB")
	}
	var s Snapshot
	if err = json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if err = s.Validate(); err != nil {
		return nil, err
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		for _, model := range []any{&database.Library{}, &database.MediaItem{}, &database.User{}, &database.MediaSource{}, &database.Image{}, &database.Subtitle{}, &database.PlayProgress{}, &database.Token{}, &database.PlaybackActivity{}} {
			var n int64
			if err := tx.Model(model).Count(&n).Error; err != nil {
				return err
			}
			if n != 0 {
				return errors.New("restore requires an empty database; existing data was not modified")
			}
		}
		for _, batch := range []struct {
			data any
			n    int
		}{{&s.Libraries, len(s.Libraries)}, {&s.Users, len(s.Users)}, {&s.Items, len(s.Items)}, {&s.Sources, len(s.Sources)}, {&s.Images, len(s.Images)}, {&s.Subtitles, len(s.Subtitles)}, {&s.Progress, len(s.Progress)}, {&s.Activity, len(s.Activity)}} {
			if batch.n > 0 {
				if err := tx.CreateInBatches(batch.data, 100).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.Config.Admin.APIKey = ""
	s.Config.Playback.SignKey = ""
	s.Config.Database.DSN = ""
	return &s.Config, nil
}
func (s *Snapshot) Validate() error {
	if s.Version != 1 {
		return fmt.Errorf("unsupported snapshot version %d", s.Version)
	}
	libs := map[string]bool{}
	items := map[string]database.MediaItem{}
	users := map[string]bool{}
	names := map[string]bool{}
	for _, l := range s.Libraries {
		if l.ID == "" || libs[l.ID] {
			return errors.New("duplicate or empty library ID")
		}
		libs[l.ID] = true
	}
	for _, u := range s.Users {
		if u.ID == "" || users[u.ID] || u.Name == "" || names[u.Name] {
			return errors.New("invalid user identity")
		}
		users[u.ID] = true
		names[u.Name] = true
		if u.PasswordHash == "" {
			return errors.New("snapshot user has no password hash")
		}
	}
	for _, i := range s.Items {
		if _, ok := items[i.ID]; ok || i.ID == "" {
			return errors.New("duplicate or empty item ID")
		}
		if !libs[i.LibraryID] && i.Type != "Genre" && i.Type != "Studio" && i.Type != "Person" {
			return errors.New("item references missing library")
		}
		items[i.ID] = i
	}
	for _, i := range s.Items {
		seen := map[string]bool{}
		for i.ParentID != nil && *i.ParentID != "" {
			if seen[i.ID] {
				return errors.New("cyclic item hierarchy")
			}
			seen[i.ID] = true
			p, ok := items[*i.ParentID]
			if !ok || p.LibraryID != i.LibraryID {
				return errors.New("invalid item parent")
			}
			i = p
		}
	}
	sourceIDs := map[string]bool{}
	for _, v := range s.Sources {
		if _, ok := items[v.ItemID]; !ok || v.ID == "" || sourceIDs[v.ID] {
			return errors.New("invalid source reference")
		}
		sourceIDs[v.ID] = true
	}
	imageIDs := map[uint]bool{}
	for _, v := range s.Images {
		_, ok := items[v.ItemID]
		if (!ok && !libs[v.ItemID]) || v.ID == 0 || imageIDs[v.ID] {
			return errors.New("invalid image reference")
		}
		imageIDs[v.ID] = true
	}
	subIDs := map[uint]bool{}
	for _, v := range s.Subtitles {
		if _, ok := items[v.ItemID]; !ok || v.ID == 0 || subIDs[v.ID] {
			return errors.New("invalid subtitle reference")
		}
		subIDs[v.ID] = true
	}
	progressIDs := map[string]bool{}
	for _, v := range s.Progress {
		k := v.UserID + "\x00" + v.ItemID
		if _, ok := items[v.ItemID]; !ok || !users[v.UserID] || v.PositionTicks < 0 || progressIDs[k] {
			return errors.New("invalid progress reference")
		}
		progressIDs[k] = true
	}
	return nil
}
