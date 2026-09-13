package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fakemby/fakemby/internal/config"
	"github.com/fakemby/fakemby/internal/database"
	"github.com/fakemby/fakemby/internal/infra/source"
	"github.com/fakemby/fakemby/internal/transfer"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
)

var version = "1.0.0-dev"

// Offline migration never overwrites a snapshot or an occupied destination.
func command(cfg *config.Config) (bool, error) {
	if len(os.Args) < 2 {
		return false, nil
	}
	op := os.Args[1]
	if op == "version" {
		fmt.Println(version)
		return true, nil
	}
	if op != "export" && op != "import" && op != "scan-strm" {
		return false, nil
	}
	if len(os.Args) < 3 {
		return true, errors.New("usage: fakemby export|import SNAPSHOT.json | scan-strm OUTPUT.json [-config CONFIG]")
	}
	path := os.Args[2]
	if op == "scan-strm" {
		entries, err := (source.STRM{Root: cfg.Playback.STRMRoot}).Scan(context.Background())
		if err != nil {
			return true, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return true, err
		}
		defer func() { _ = f.Close() }()
		err = json.NewEncoder(f).Encode(map[string]any{"library": "STRM", "items": entries})
		if err == nil {
			err = f.Sync()
		}
		return true, err
	}
	if cfg.Database.Dialect != "" && cfg.Database.Dialect != "sqlite" {
		return true, errors.New("offline snapshot CLI currently requires SQLite; network database migration is not yet supported")
	}
	if op == "export" {
		if _, err := os.Stat(cfg.Database.Path); err != nil {
			return true, err
		}
	}
	d, err := gorm.Open(sqlite.Open(cfg.Database.Path+"?_pragma=busy_timeout(5000)"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return true, err
	}
	sqlDB, err := d.DB()
	if err != nil {
		return true, err
	}
	defer func() { _ = sqlDB.Close() }()
	sqlDB.SetMaxOpenConns(1)
	if op == "export" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return true, err
		}
		defer func() { _ = f.Close() }()
		if err = transfer.Export(d, cfg, f); err == nil {
			err = f.Sync()
		}
		return true, err
	}
	f, err := os.Open(path)
	if err != nil {
		return true, err
	}
	defer func() { _ = f.Close() }()
	if err = database.Migrate(d); err != nil {
		return true, err
	}
	restored, err := transfer.Import(d, f)
	if err != nil {
		return true, err
	}
	// Config is delivered separately, never silently replaces the active config.
	configPath := path + ".restored-config.json"
	out, err := os.OpenFile(configPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return true, fmt.Errorf("database restored but config output could not be created: %w", err)
	}
	defer func() { _ = out.Close() }()
	err = json.NewEncoder(out).Encode(restored)
	fmt.Fprintln(os.Stderr, "Snapshot restored. Configure new admin/signing keys; previous tokens were intentionally not restored.")
	return true, err
}
