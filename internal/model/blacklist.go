package model

import (
	"time"

	"gorm.io/gorm"
)

type Blacklist struct {
	ID          uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	PackageName string `gorm:"column:package_name;type:varchar(64);uniqueIndex;not null" json:"package_name"`

	CreatedAt time.Time `json:"created_at"`
}

// TableName 指定表名
func (Blacklist) TableName() string {
	return "blacklist"
}

// GetAllBlacklistPackageNames 从 blacklist 表取出所有 package_name。
// 返回值确保为非 nil slice，避免 JSON 序列化为 null 导致客户端解析失败。
func GetAllBlacklistPackageNames(db *gorm.DB) ([]string, error) {
	pkgs := make([]string, 0)
	if db == nil {
		return pkgs, nil
	}
	err := db.Model(&Blacklist{}).
		Order("id ASC").
		Pluck("package_name", &pkgs).Error
	return pkgs, err
}
