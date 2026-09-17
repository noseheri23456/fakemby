package config

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// 已知的不安全默认值（来自历史配置 / 开源仓库），命中即视为"未配置"
const (
	DefaultAdminAPIKey = "change-me"
	DefaultSignKey     = "change-me-in-production"

	DefaultTokenExpiryDays = 30
	DefaultPort            = 8096
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Auth     AuthConfig     `mapstructure:"auth"`
	Image    ImageConfig    `mapstructure:"image"`
	Playback PlaybackConfig `mapstructure:"playback"`
	Admin    AdminConfig    `mapstructure:"admin"`
	Log      LogConfig      `mapstructure:"log"`
}

type ServerConfig struct {
	Host    string `mapstructure:"host"`
	Port    int    `mapstructure:"port"`
	Name    string `mapstructure:"name"`
	Version string `mapstructure:"version"`
	ID      string `mapstructure:"id"`
	// CORSOrigins 允许跨域访问的 Origin 白名单。
	// 空 = 同源（不输出 Access-Control-Allow-* 头）；"*" 表示允许任意源（此时强制关闭 credentials）。
	CORSOrigins []string `mapstructure:"cors_origins"`
}

type DatabaseConfig struct {
	Dialect      string `mapstructure:"dialect"`
	DSN          string `mapstructure:"dsn"`
	Path         string `mapstructure:"path"`
	WALMode      bool   `mapstructure:"wal_mode"`
	MaxOpenConns int    `mapstructure:"max_open_conns"` // 读连接池大小（A2）；<=0 回落单连接
	MaxIdleConns int    `mapstructure:"max_idle_conns"` // 读连接池空闲连接数；>MaxOpenConns 时按后者裁剪
}

type AuthConfig struct {
	TokenExpiryDays  int `mapstructure:"token_expiry_days"`
	LoginMaxAttempts int `mapstructure:"login_max_attempts"` // 登录失败锁定阈值（A5）；<=0 禁用
	LoginLockMinutes int `mapstructure:"login_lock_minutes"` // 锁定窗口（分钟）；A5
}

type ImageConfig struct {
	Mode       string `mapstructure:"mode"`
	CacheDir   string `mapstructure:"cache_dir"`
	CacheMaxMB int    `mapstructure:"cache_max_mb"` // 代理缓存磁盘配额（MB）；0=不限（A6）
	CDNPrefix  string `mapstructure:"cdn_prefix"`
	// RequireAuth 为 true 时图片端点强制校验 token/签名，匿名请求一律 401。
	// 默认 false：Emby 官方的图片端点允许匿名读取，因为客户端是用 <img src> 拉图的，
	// 既带不了请求头，Theater 的 apiClient.getImageUrl 也不会拼 api_key
	// （只有 WebSocket 等少数接口才显式加）。要求鉴权会让海报/背景整片 401。
	RequireAuth bool `mapstructure:"require_auth"`
}

type PlaybackConfig struct {
	RedirectMode  string   `mapstructure:"redirect_mode"`
	BindIP        bool     `mapstructure:"bind_ip"`
	PlainPrefixes []string `mapstructure:"plain_prefixes"`
	STRMRoot      string   `mapstructure:"strm_root"`
	Redirect      bool     `mapstructure:"redirect"`
	SignKey       string   `mapstructure:"sign_key"`
	SignTTL       int      `mapstructure:"sign_ttl"`
	FlushInterval int      `mapstructure:"flush_interval"` // 进度缓冲 flush 间隔（秒）；A7
	// SignPrefixes 只对 URL 命中这些前缀的播放源追加签名参数。
	// 对不配合校验的第三方 CDN 追加我方签名没有意义（M0-7 的设计边界）。
	SignPrefixes []string `mapstructure:"sign_prefixes"`
}

type AdminConfig struct {
	APIKey string `mapstructure:"api_key"`
}

type LogConfig struct {
	Level string `mapstructure:"level"`
	File  string `mapstructure:"file"`
}

var globalConfig *Config

// ConfigPath 返回配置文件路径：优先 CONFIG_FILE，其次是命令行参数，最后回落 config.yaml
func ConfigPath() string {
	if p := os.Getenv("CONFIG_FILE"); p != "" {
		return p
	}
	for i, arg := range os.Args {
		if arg == "-config" || arg == "--config" {
			if i+1 < len(os.Args) {
				return os.Args[i+1]
			}
		}
	}
	return "config.yaml"
}

// Load 加载配置。
// 配置文件缺失时不再直接失败，而是回落内置默认值（便于容器/一次性场景零配置启动），
// 但会在日志中明确告警。配置文件存在但内容非法时仍然报错。
func Load(configPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	setDefaults(v)

	// 环境变量覆盖：FAKEMBY_SERVER_PORT 这类下划线形式
	v.SetEnvPrefix("FAKEMBY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()
	bindEnvKeys(v)

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		var pathErr *os.PathError
		if errors.As(err, &notFound) || errors.As(err, &pathErr) || os.IsNotExist(err) {
			slog.Warn("配置文件不存在，使用内置默认值启动",
				"path", configPath,
				"hint", "可通过 CONFIG_FILE 指定路径，或用 FAKEMBY_ 前缀环境变量覆盖")
		} else {
			return nil, fmt.Errorf("读取配置文件失败: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	// 设置默认值
	if cfg.Server.ID == "" || cfg.Server.ID == "fakemby-xxxxx" {
		cfg.Server.ID = "fakemby-default"
	}

	globalConfig = &cfg
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", DefaultPort)
	v.SetDefault("server.name", "FakEmby Server")
	v.SetDefault("server.version", "4.8.0.0")
	v.SetDefault("server.id", "fakemby-xxxxx")
	v.SetDefault("server.cors_origins", []string{})

	v.SetDefault("database.dialect", "sqlite")
	v.SetDefault("database.dsn", "")
	v.SetDefault("database.path", "./fakemby.db")
	v.SetDefault("database.wal_mode", true)
	v.SetDefault("database.max_open_conns", 10)
	v.SetDefault("database.max_idle_conns", 5)

	v.SetDefault("auth.token_expiry_days", DefaultTokenExpiryDays)
	v.SetDefault("auth.login_max_attempts", 5)
	v.SetDefault("auth.login_lock_minutes", 15)

	v.SetDefault("image.mode", "redirect")
	v.SetDefault("image.cache_dir", "./cache/images")
	v.SetDefault("image.cache_max_mb", 0)
	v.SetDefault("image.cdn_prefix", "")
	v.SetDefault("image.require_auth", false)

	v.SetDefault("playback.redirect_mode", "signed")
	v.SetDefault("playback.bind_ip", false)
	v.SetDefault("playback.plain_prefixes", []string{})
	v.SetDefault("playback.strm_root", "")
	v.SetDefault("playback.redirect", true)
	v.SetDefault("playback.sign_key", "")
	v.SetDefault("playback.sign_ttl", 3600)
	v.SetDefault("playback.flush_interval", 30)
	v.SetDefault("playback.sign_prefixes", []string{})

	v.SetDefault("admin.api_key", "")

	v.SetDefault("log.level", "info")
	v.SetDefault("log.file", "")
}

// bindEnvKeys 显式绑定所有叶子键。
// viper 的 AutomaticEnv 对"配置文件里不存在、只有默认值"的嵌套键不会生效，
// 必须显式 BindEnv 才能让 FAKEMBY_SERVER_PORT 这类变量真正覆盖。
func bindEnvKeys(v *viper.Viper) {
	keys := []string{
		"server.host", "server.port", "server.name", "server.version", "server.id", "server.cors_origins",
		"database.dialect", "database.dsn", "database.path", "database.wal_mode", "database.max_open_conns", "database.max_idle_conns",
		"auth.token_expiry_days", "auth.login_max_attempts", "auth.login_lock_minutes",
		"image.mode", "image.cache_dir", "image.cache_max_mb", "image.cdn_prefix", "image.require_auth",
		"playback.redirect_mode", "playback.bind_ip", "playback.plain_prefixes", "playback.strm_root", "playback.redirect", "playback.sign_key", "playback.sign_ttl", "playback.flush_interval", "playback.sign_prefixes",
		"admin.api_key",
		"log.level", "log.file",
	}
	for _, k := range keys {
		if err := v.BindEnv(k); err != nil {
			slog.Warn("绑定环境变量失败", "key", k, "error", err)
		}
	}
}

func Get() *Config {
	return globalConfig
}

// SetGlobal 直接设置全局配置。
// 仅供测试与进程内嵌场景使用；正式启动请走 Load（它负责环境变量覆盖与默认值）。
func SetGlobal(c *Config) {
	globalConfig = c
}

func (c *Config) PrintConfig() {
	logger := slog.Default()
	logger.Info("=== FakEmby 配置 ===")
	logger.Info("服务器", "host", c.Server.Host, "port", c.Server.Port, "cors_origins", c.Server.CORSOrigins)
	logger.Info("数据库", "path", c.Database.Path, "wal_mode", c.Database.WALMode)
	logger.Info("认证", "token_expiry_days", c.Auth.TokenExpiryDays)
	logger.Info("图片", "mode", c.Image.Mode, "cache_dir", c.Image.CacheDir)
	logger.Info("播放", "redirect", c.Playback.Redirect, "sign_ttl", c.Playback.SignTTL,
		"sign_enabled", c.SigningEnabled(), "sign_prefixes", c.Playback.SignPrefixes)
	logger.Info("管理", "api_key_configured", c.AdminAPIKeyUsable())
	logger.Info("日志", "level", c.Log.Level, "file", c.Log.File)
}

// GetListenAddr 返回监听地址
func (s ServerConfig) GetListenAddr() string {
	return s.Host + ":" + fmt.Sprint(s.Port)
}

// TokenExpiryDays 返回 token 过期天数，非法值回落默认值
func (c *Config) TokenExpiryDays() int {
	if c == nil || c.Auth.TokenExpiryDays <= 0 {
		return DefaultTokenExpiryDays
	}
	return c.Auth.TokenExpiryDays
}

// AdminAPIKeyUsable 管理密钥是否可用于鉴权（已配置且非公开默认值）
func (c *Config) AdminAPIKeyUsable() bool {
	return c != nil && c.Admin.APIKey != "" && c.Admin.APIKey != DefaultAdminAPIKey
}

// SigningEnabled 是否启用播放直链签名
func (c *Config) SigningEnabled() bool {
	return c != nil && c.Playback.SignKey != "" && c.Playback.SignKey != DefaultSignKey
}

// ValidateSignKey 检查签名密钥 / 管理密钥是否安全。
// 返回值仅为提示性警告列表，调用方负责打印。
func (c *Config) ValidateSignKey() []string {
	var warns []string
	if !c.SigningEnabled() {
		warns = append(warns, "playback.sign_key 未配置或仍为默认值，播放直链签名已禁用")
	}
	if !c.AdminAPIKeyUsable() {
		warns = append(warns, "admin.api_key 未配置或仍为默认值 change-me，管理接口将拒绝所有请求")
	}
	return warns
}

// EnsureSignKey 保证签名密钥可用：未配置或仍是仓库里的公开默认值时，生成临时随机密钥。
//
// 临时密钥不跨进程保留，重启后此前签发的直链会失效。这是刻意取舍——
// 宁可让旧链接失效，也不要用一个公开在源码里的常量去做"看起来有防护"的签名。
func (c *Config) EnsureSignKey() {
	if c.SigningEnabled() {
		return
	}
	if c.Playback.SignKey == DefaultSignKey {
		slog.Warn("⚠️ playback.sign_key 仍是仓库里的公开默认值，已忽略并改用临时随机密钥",
			"hint", "设置 FAKEMBY_PLAYBACK_SIGN_KEY 或配置文件 playback.sign_key")
	} else {
		slog.Warn("playback.sign_key 未配置，已生成临时随机签名密钥（重启后旧直链将失效）",
			"hint", "生产环境建议在配置中固定一个强随机值")
	}
	b := make([]byte, 32)
	if _, err := cryptorand.Read(b); err != nil {
		slog.Error("生成随机签名密钥失败，签名功能保持禁用", "error", err)
		return
	}
	c.Playback.SignKey = hex.EncodeToString(b)
}

// ShouldSign 判断某个源 URL 是否需要追加签名参数。
//
// 判定顺序（前者压过后者）：
//  0. 签名未启用（sign_key 空或仍是默认值）→ 不签名
//  1. redirect_mode == "plain"  → 全不签名（灰度总开关）
//  2. plain_prefixes 命中       → 不签名（逐前缀免签，灰度切换主手段）
//  3. sign_prefixes 为空        → 签名（空 = 不限定范围 = 全部签名）
//  4. sign_prefixes 命中则签名，未命中则不签名
//
// 第 3 条是出厂默认（两份 config.yaml 的 sign_prefixes 都是 []）：默认必须签名，
// 否则防盗链形同虚设。曾因写成 `return false` 导致默认全部直链裸奔（fail-open）。
func (c *Config) ShouldSign(rawURL string) bool {
	if c == nil || rawURL == "" || c.Playback.RedirectMode == "plain" || !c.SigningEnabled() {
		return false
	}
	for _, p := range c.Playback.PlainPrefixes {
		if URLPrefixMatches(rawURL, p) {
			return false
		}
	}
	if len(c.Playback.SignPrefixes) == 0 {
		return true
	}
	for _, p := range c.Playback.SignPrefixes {
		if URLPrefixMatches(rawURL, p) {
			return true
		}
	}
	return false
}

// URLPrefixMatches compares URL origins and path boundaries, not host prefixes.
func URLPrefixMatches(raw, prefix string) bool {
	u, e := url.Parse(raw)
	p, pe := url.Parse(prefix)
	if e != nil || pe != nil || p.Host == "" || u.User != nil || p.User != nil || !strings.EqualFold(u.Scheme, p.Scheme) || !strings.EqualFold(u.Host, p.Host) {
		return false
	}
	path := strings.TrimSuffix(p.Path, "/")
	return path == "" || u.Path == path || strings.HasPrefix(u.Path, path+"/")
}

// PrepareRuntime 启动时一次性准备：生成/校验密钥、创建日志与缓存目录并输出安全告警
func (c *Config) PrepareRuntime() error {
	c.EnsureSignKey()

	for _, w := range c.ValidateSignKey() {
		slog.Warn("⚠️ "+w, "hint", "可用 FAKEMBY_ADMIN_API_KEY / FAKEMBY_PLAYBACK_SIGN_KEY 覆盖")
	}
	if err := c.EnsureLogDir(); err != nil {
		return err
	}
	return c.EnsureCacheDir()
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
