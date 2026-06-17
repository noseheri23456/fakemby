package emby

import (
	"strings"

	"github.com/google/uuid"
)

// newShortID 生成 32 位十六进制字符串（官方 Emby 客户端严格要求该格式，否则反序列化报错）
func newShortID() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}
