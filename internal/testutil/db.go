// Package testutil 提供测试脚手架：内存 SQLite、确定性种子数据、以及挂载了全部路由的 gin 引擎。
//
// 它是非 _test.go 的普通包，这样 service / emby / integration 各层测试都能共用同一套夹具，
// 而不用各自复制一遍建表与造数据的代码。
package testutil

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/fakemby/fakemby/internal/database"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// memDBSeq 为每个测试生成独立的内存库名，避免 cache=shared 下多个测试互相看见彼此数据。
var memDBSeq atomic.Uint64

// NewTestDB 建一个隔离的内存 SQLite（已迁移建表 + 建索引），并注入为全局句柄。
//
// 注入全局句柄是必要的：emby 包的路由注册函数与部分 handler 直接走 database.Get()。
// 测试结束后会恢复全局句柄为 nil 并关闭连接。
func NewTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:testdb_%d?mode=memory&cache=shared", memDBSeq.Add(1))

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取测试数据库连接失败: %v", err)
	}
	// 内存库每个连接都是独立的库，必须限制为单连接，否则会莫名读到空表。
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	if err := database.Migrate(db); err != nil {
		t.Fatalf("迁移测试数据库失败: %v", err)
	}

	database.Set(db)
	t.Cleanup(func() {
		database.Set(nil)
		_ = sqlDB.Close()
	})

	return db
}
