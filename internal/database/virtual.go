package database

import (
	"crypto/md5"
	"encoding/hex"
	"errors"

	"gorm.io/gorm"
)

// 分类（Genre / Studio）与人物（Person）在 Emby 语义里也是"条目"：
// 客户端会请求 /emby/Genres、/emby/Persons，点进去还能拿到该分类下的媒体列表。
// 本项目不单独建分类表，而是为每个分类名在 media_items 里建一条虚拟条目
// （type = Genre / Studio / Person，无 library_id、无播放源）。
//
// 代价是需要保证 ID 的生成规则在三处一致：
//   1. 导入侧建虚拟条目（internal/api/admin/import.go）
//   2. 单条 CRUD 建虚拟条目（internal/api/admin/items.go）
//   3. DTO 与列表接口回填 ID（internal/service/media.go、internal/api/emby）
// 不一致的典型后果：?PersonIds= 筛选永远筛不到结果，且不报错。
// 所以统一收敛到本文件的两个函数，禁止各处自己拼 md5。

// VirtualItemID 返回虚拟条目的确定性 ID：md5(prefix + ":" + name)。
func VirtualItemID(prefix, name string) string {
	hash := md5.Sum([]byte(prefix + ":" + name))
	return hex.EncodeToString(hash[:])
}

// EnsureVirtualItem 幂等地保证虚拟条目存在，返回其 ID。
//
// 已存在时若 type 或 name 对不上，说明哈希碰撞或数据被外部改写，返回错误而不是
// 静默覆盖——这种静默正是本项目反复踩过的 fail-open 模式。
func EnsureVirtualItem(tx *gorm.DB, prefix, name, kind string) (string, error) {
	id := VirtualItemID(prefix, name)
	var existing MediaItem
	err := tx.Where("id = ?", id).First(&existing).Error
	switch {
	case err == nil:
		if existing.Type != kind || existing.Name != name {
			return "", errors.New("virtual item identity conflict")
		}
		return id, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return id, tx.Create(&MediaItem{ID: id, Name: name, Type: kind}).Error
	default:
		return "", err
	}
}
