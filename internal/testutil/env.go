package testutil

import (
	"testing"

	"github.com/fakemby/fakemby/internal/config"
	"gorm.io/gorm"
)

// Env 一次「建库 + 灌种子 + 装配置」的结果，让各层测试只需一行：
//
//	env := testutil.Setup(t)
type Env struct {
	DB       *gorm.DB
	Cfg      *config.Config
	Fixtures Fixtures
}

// Setup 组合 NewTestDB / SeedFixtures / TestConfig，返回可直接使用的测试环境。
func Setup(t *testing.T) Env {
	t.Helper()

	db := NewTestDB(t)
	cfg := TestConfig(t)
	fx := SeedFixtures(t, db)
	return Env{DB: db, Cfg: cfg, Fixtures: fx}
}
