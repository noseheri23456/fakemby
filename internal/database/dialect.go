package database

import (
	"fmt"
	"github.com/fakemby/fakemby/internal/config"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"strings"
)

// InitConfigured selects a single-instance backend. Network backends must be
// provisioned separately; DSNs are never emitted in application logs.
func InitConfigured(c config.DatabaseConfig) (*gorm.DB, error) {
	kind := strings.ToLower(c.Dialect)
	if kind == "" || kind == "sqlite" {
		return Init(c.Path, c.WALMode, c.MaxOpenConns, c.MaxIdleConns)
	}
	var driver gorm.Dialector
	switch kind {
	case "postgres", "postgresql":
		driver = postgres.Open(c.DSN)
	case "mysql":
		driver = mysql.New(mysql.Config{DSN: c.DSN, DefaultStringSize: 256})
	default:
		return nil, fmt.Errorf("unsupported database dialect %q", kind)
	}
	if c.DSN == "" {
		return nil, fmt.Errorf("database.dsn is required for %s", kind)
	}
	d, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, fmt.Errorf("database connection failed for %s", kind)
	}
	sqlDB, err := d.DB()
	if err != nil {
		return nil, err
	}
	n := c.MaxOpenConns
	if n <= 0 {
		n = 10
	}
	sqlDB.SetMaxOpenConns(n)
	sqlDB.SetMaxIdleConns(n)
	if err = autoMigrate(d); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err = createDefaultAdmin(d); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	db, writeDB = d, d
	return d, nil
}
