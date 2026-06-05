package database

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

func Init(dbPath string, walMode bool) (*gorm.DB, error) {
	// 使用 glebarez/sqlite 驱动（纯 Go，不需要 CGO）
	// 连接字符串：file:path?mode=rwc
	database, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// SQLite 并发写入限制：设置 MaxOpenConns(1) 防止 'database is locked'
	sqlDB, err := database.DB()
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	// 设置 WAL 模式提高并发性能，设置 busy_timeout 处理写冲突
	if walMode {
		if err := database.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
			slog.Warn("设置 WAL 模式失败", "error", err)
		}
		if err := database.Exec("PRAGMA busy_timeout=5000").Error; err != nil {
			slog.Warn("设置 busy_timeout 失败", "error", err)
		}
	}

	// 自动迁移
	if err := autoMigrate(database); err != nil {
		return nil, fmt.Errorf("自动迁移失败: %w", err)
	}

	// 创建索引
	if err := createIndexes(database); err != nil {
		return nil, fmt.Errorf("创建索引失败: %w", err)
	}

	// 创建默认管理员（如果不存在）
	if err := createDefaultAdmin(database); err != nil {
		return nil, fmt.Errorf("创建默认管理员失败: %w", err)
	}

	db = database
	slog.Info("✓ 数据库初始化成功", "path", dbPath, "wal_mode", walMode)
	return database, nil
}

func autoMigrate(d *gorm.DB) error {
	return d.AutoMigrate(
		&Library{},
		&MediaItem{},
		&MediaSource{},
		&Image{},
		&Subtitle{},
		&User{},
		&PlayProgress{},
		&Token{},
	)
}

func createIndexes(d *gorm.DB) error {
	// media_items 索引
	d.Exec("CREATE INDEX IF NOT EXISTS idx_items_library ON media_items(library_id)")
	d.Exec("CREATE INDEX IF NOT EXISTS idx_items_parent ON media_items(parent_id)")
	d.Exec("CREATE INDEX IF NOT EXISTS idx_items_type ON media_items(type)")
	d.Exec("CREATE INDEX IF NOT EXISTS idx_items_year ON media_items(year)")
	d.Exec("CREATE INDEX IF NOT EXISTS idx_items_name ON media_items(name)")

	// media_sources 索引
	d.Exec("CREATE INDEX IF NOT EXISTS idx_sources_item ON media_sources(item_id)")

	// images 索引
	d.Exec("CREATE INDEX IF NOT EXISTS idx_images_item ON images(item_id)")

	// play_progress 索引
	d.Exec("CREATE INDEX IF NOT EXISTS idx_progress_user ON play_progress(user_id)")

	// tokens 索引（用于过期清理）
	d.Exec("CREATE INDEX IF NOT EXISTS idx_tokens_created ON tokens(created_at)")

	slog.Info("✓ 数据库索引创建成功")
	return nil
}

func createDefaultAdmin(d *gorm.DB) error {
	var count int64
	d.Model(&User{}).Count(&count)

	if count > 0 {
		slog.Info("users 表已有数据，跳过默认管理员创建")
		return nil
	}

	// 生成 bcrypt 密码哈希
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("密码哈希失败: %w", err)
	}

	adminUser := &User{
		ID:           uuid.New().String(),
		Name:         "admin",
		PasswordHash: string(passwordHash),
		IsAdmin:      true,
		Policy:       "{}",
	}

	if err := d.Create(adminUser).Error; err != nil {
		return fmt.Errorf("创建默认管理员失败: %w", err)
	}

	slog.Info("✓ 默认管理员创建成功", "username", "admin", "password", "admin")
	slog.Warn("⚠️ 警告: 生产环境中请立即更改默认密码!")
	return nil
}

func Get() *gorm.DB {
	return db
}

func Close() error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// StartTokenCleanupRoutine 启动令牌过期清理 goroutine
func StartTokenCleanupRoutine(expiryDays int) {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			cleanExpiredTokens(expiryDays)
		}
	}()

	slog.Info("✓ Token 过期清理 goroutine 启动", "interval", "1h", "expiry_days", expiryDays)
}

func cleanExpiredTokens(expiryDays int) {
	cutoffTime := time.Now().AddDate(0, 0, -expiryDays)

	if err := db.Where("created_at < ?", cutoffTime).Delete(&Token{}).Error; err != nil {
		slog.Error("清理过期 Token 失败", "error", err)
		return
	}

	slog.Debug("✓ 过期 Token 清理完成", "cutoff_time", cutoffTime)
}

// CheckPassword 验证密码
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// HashPassword 生成密码哈希
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
