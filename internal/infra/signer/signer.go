// Package signer 提供播放直链的 HMAC-SHA256 签名与校验（ROADMAP M0-7 / S2）。
//
// 设计边界（诚实记录）：
// 签名只有当「直链的服务端愿意校验我方签名」时才有防盗链意义——例如自建反代、
// OpenList/Alist（配合回调校验）。对不配合的第三方 CDN，追加我方签名参数不会
// 被校验，也不会阻止盗链；那种场景的防线是 PlaybackInfo 本身的鉴权 + 源站 URL
// 自带的时效性。因此实现上只对 playback.sign_prefixes 命中的源追加签名。
package signer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrExpired / ErrBadSignature 为校验失败的两种原因
var (
	ErrExpired      = fmt.Errorf("signature expired")
	ErrBadSignature = fmt.Errorf("signature mismatch")
)

// Signer 基于共享密钥的 URL 签名器
type Signer struct {
	key []byte
	ttl time.Duration
}

// New 构造签名器；key 为空时返回 nil（表示禁用签名）
func New(key string, ttlSeconds int) *Signer {
	if key == "" {
		return nil
	}
	if ttlSeconds <= 0 {
		ttlSeconds = 3600
	}
	return &Signer{key: []byte(key), ttl: time.Duration(ttlSeconds) * time.Second}
}

// Payload 是被签名的业务数据。字段顺序固定，改动即视为破坏性变更。
type Payload struct {
	ItemID     string
	SourceID   string
	UserID     string
	MediaType  string // video | subtitle
	ExtraIndex string // 字幕下标等可选维度，视频场景留空
	IP         string // Optional client address binding, signed as a separate dimension.
}

// String 序列化为待签名字符串
func (p Payload) String() string {
	parts := []string{p.MediaType, p.ItemID, p.SourceID, p.UserID, p.ExtraIndex}
	for i, v := range parts {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(v, "%", "%25"), "|", "%7C")
	}
	raw := strings.Join(parts, "|")
	if p.IP != "" {
		raw += "|ip=" + p.IP
	}
	return raw
}

// Sign 返回过期时间戳与签名（hex）
func (s *Signer) Sign(p Payload) (exp int64, sig string, err error) {
	if s == nil {
		return 0, "", fmt.Errorf("signer disabled")
	}
	exp = time.Now().Add(s.ttl).Unix()
	sig = s.sign(p.String(), exp)
	return exp, sig, nil
}

func (s *Signer) sign(payload string, exp int64) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	mac.Write([]byte(":"))
	mac.Write([]byte(strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify 校验签名与时效。payload 必须与签发时一致。
func (s *Signer) Verify(p Payload, exp int64, sig string) error {
	return s.VerifyRaw(p.String(), exp, sig)
}

// VerifyRaw 校验任意 payload 字符串（供 /api/auth/verify 回调使用）
func (s *Signer) VerifyRaw(payload string, exp int64, sig string) error {
	if s == nil {
		return fmt.Errorf("signer disabled")
	}
	if exp <= 0 || sig == "" {
		return ErrBadSignature
	}
	if time.Now().Unix() > exp {
		return ErrExpired
	}
	if !hmac.Equal([]byte(s.sign(payload, exp)), []byte(sig)) {
		return ErrBadSignature
	}
	return nil
}
