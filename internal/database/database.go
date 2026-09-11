package database

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	db      *gorm.DB // 读连接池：多连接，支持并发读
	writeDB *gorm.DB // 写连接池：单连接，避免 database is locked（A2）
)

func Init(dbPath string, walMode bool, maxOpenConns, maxIdleConns int) (*gorm.DB, error) {
	// 写句柄：单连接串行化写，配合 WAL 模式仍能并发读（A2）。
	// 迁移 / 索引 / 默认管理员只跑一次，落在同一个库文件上即可。
	wdb, err := openHandle(dbPath, walMode, 1, 1)
	if err != nil {
		return nil, fmt.Errorf("打开写数据库连接失败: %w", err)
	}
	if err := autoMigrate(wdb); err != nil {
		return nil, fmt.Errorf("自动迁移失败: %w", err)
	}
	if err := createIndexes(wdb); err != nil {
		return nil, fmt.Errorf("创建索引失败: %w", err)
	}
	if err := createDefaultAdmin(wdb); err != nil {
		return nil, fmt.Errorf("创建默认管理员失败: %w", err)
	}

	// 读句柄：连接池大小可配，去掉「读也被 MaxOpenConns(1) 串行化」的问题（A2）。
	rdb, err := openHandle(dbPath, walMode, maxOpenConns, maxIdleConns)
	if err != nil {
		return nil, fmt.Errorf("打开读数据库连接失败: %w", err)
	}

	writeDB = wdb
	db = rdb
	slog.Info("✓ 数据库初始化成功", "path", dbPath, "wal_mode", walMode,
		"read_pool", maxOpenConns, "write_pool", 1)
	return rdb, nil
}

// openHandle 打开一个独立的连接池句柄。每个 *gorm.DB 维护自己的连接池，
// 读/写分开后，读并发不再被写连接的 MaxOpenConns(1) 卡住（A2）。
func openHandle(dbPath string, walMode bool, maxOpen, maxIdle int) (*gorm.DB, error) {
	// 使用 glebarez/sqlite 驱动（纯 Go，不需要 CGO）
	database, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接失败: %w", err)
	}
	if maxOpen <= 0 {
		maxOpen = 1
	}
	if maxIdle <= 0 || maxIdle > maxOpen {
		maxIdle = maxOpen
	}
	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(maxIdle)

	// 设置 WAL 模式提高并发性能，设置 busy_timeout 处理写冲突
	if walMode {
		if err := database.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
			slog.Warn("设置 WAL 模式失败", "error", err)
		}
		// busy_timeout 是「每个连接独立」的，两个句柄都要设；写冲突时等待而非立刻报 locked
		if err := database.Exec("PRAGMA busy_timeout=5000").Error; err != nil {
			slog.Warn("设置 busy_timeout 失败", "error", err)
		}
	}

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
		&PlaybackActivity{},
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
		ID:                 uuid.New().String(),
		Name:               "admin",
		PasswordHash:       string(passwordHash),
		IsAdmin:            true,
		MustChangePassword: true, // 首次启动用默认口令创建，强制改密（A4）
		Policy:             "{}",
	}

	if err := d.Create(adminUser).Error; err != nil {
		return fmt.Errorf("创建默认管理员失败: %w", err)
	}

	slog.Info("✓ 默认管理员创建成功", "username", "admin", "password", "admin")
	slog.Warn("⚠️ 警告: 生产环境中请立即更改默认密码!")
	return nil
}

// Migrate 执行自动迁移与索引创建。
// 抽出来是为了让测试与运维脚本能在不触发默认管理员创建的前提下建表。
func Migrate(d *gorm.DB) error {
	if err := autoMigrate(d); err != nil {
		return fmt.Errorf("自动迁移失败: %w", err)
	}
	if err := createIndexes(d); err != nil {
		return fmt.Errorf("创建索引失败: %w", err)
	}
	return nil
}

func Get() *gorm.DB {
	return db
}

// GetWrite 返回写连接池句柄。写连接为单连接，避免 SQLite 并发写报 locked（A2）。
// 未显式初始化（仅测试用 database.Set 注入）时回退到读句柄，保持调用方语义不变。
func GetWrite() *gorm.DB {
	if writeDB != nil {
		return writeDB
	}
	return db
}

// Set 注入全局数据库句柄。
// 仅供测试使用——正式启动必须走 Init（它还会建索引、建默认管理员）。
func Set(d *gorm.DB) {
	db = d
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
