package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Auth     AuthConfig     `mapstructure:"auth"`
	Image    ImageConfig    `mapstructure:"image"`
	Playback PlaybackConfig `mapstructure:"playback"`
	Admin    AdminConfig    `mapstructure:"admin"`
	TMDb     TMDbConfig     `mapstructure:"tmdb"`
	Log      LogConfig      `mapstructure:"log"`
}

type ServerConfig struct {
	Host    string `mapstructure:"host"`
	Port    int    `mapstructure:"port"`
	Name    string `mapstructure:"name"`
	Version string `mapstructure:"version"`
	ID      string `mapstructure:"id"`
}

type DatabaseConfig struct {
	Path    string `mapstructure:"path"`
	WALMode bool   `mapstructure:"wal_mode"`
}

type AuthConfig struct {
	TokenExpiryDays int `mapstructure:"token_expiry_days"`
}

type ImageConfig struct {
	Mode      string `mapstructure:"mode"`
	CacheDir  string `mapstructure:"cache_dir"`
	CDNPrefix string `mapstructure:"cdn_prefix"`
}

type PlaybackConfig struct {
	Redirect bool   `mapstructure:"redirect"`
	SignKey  string `mapstructure:"sign_key"`
	SignTTL  int    `mapstructure:"sign_ttl"`
}

type AdminConfig struct {
	APIKey string `mapstructure:"api_key"`
}

type TMDbConfig struct {
	APIKey    string `mapstructure:"api_key"`
	Language  string `mapstructure:"language"`
	ImageBase string `mapstructure:"image_base"`
}

type LogConfig struct {
	Level string `mapstructure:"level"`
	File  string `mapstructure:"file"`
}

var globalConfig *Config

func Load(configPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	// 支持环境变量覆盖
	v.AutomaticEnv()
	v.SetEnvPrefix("FAKEMBY")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	// 设置默认值
	if cfg.Server.ID == "" || cfg.Server.ID == "fakemby-xxxxx" {
		// 生成默认 ID（可以在这里生成 UUID）
		cfg.Server.ID = "fakemby-default"
	}

	globalConfig = &cfg
	return &cfg, nil
}

func Get() *Config {
	return globalConfig
}

func (c *Config) PrintConfig() {
	logger := slog.Default()
	logger.Info("=== FakEmby 配置 ===")
	logger.Info("服务器", "host", c.Server.Host, "port", c.Server.Port)
	logger.Info("数据库", "path", c.Database.Path, "wal_mode", c.Database.WALMode)
	logger.Info("认证", "token_expiry_days", c.Auth.TokenExpiryDays)
	logger.Info("图片", "mode", c.Image.Mode, "cache_dir", c.Image.CacheDir)
	logger.Info("日志", "level", c.Log.Level, "file", c.Log.File)
}

// GetListenAddr 返回监听地址
func (s ServerConfig) GetListenAddr() string {
	return s.Host + ":" + fmt.Sprint(s.Port)
}

// ValidateSignKey 检查签名密钥是否安全（生产环境提示）
func (c *Config) ValidateSignKey() {
	if c.Playback.SignKey == "change-me-in-production" {
		slog.Warn("⚠️ 警告: 使用默认签名密钥，请在生产环境中更改")
	}
	if c.Admin.APIKey == "change-me" {
		slog.Warn("⚠️ 警告: 使用默认管理 API 密钥，请在生产环境中更改")
	}
}

// EnsureLogDir 创建日志目录
func (c *Config) EnsureLogDir() error {
	if c.Log.File == "" {
		return nil
	}
	dir := filepath.Dir(c.Log.File)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建日志目录失败: %w", err)
		}
	}
	return nil
}

// EnsureCacheDir 创建缓存目录
func (c *Config) EnsureCacheDir() error {
	if c.Image.CacheDir == "" {
		return nil
	}
	if err := os.MkdirAll(c.Image.CacheDir, 0755); err != nil {
		return fmt.Errorf("创建缓存目录失败: %w", err)
	}
	return nil
}
