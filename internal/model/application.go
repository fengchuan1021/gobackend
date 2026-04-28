package model

import (
	"time"
)

// Application 应用配置模型（用户勾选的清理/备份选项）
type Application struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	PackageName string    `gorm:"type:varchar(255);uniqueIndex;not null" json:"package_name"`
	Name        string    `gorm:"type:varchar(255);not null" json:"name"`
	IconPath    string    `gorm:"type:varchar(512)" json:"icon_path"` // 相对于 antares_assets 的路径，如 images/appicon/xxx.jpg
	Whitelist   bool      `gorm:"default:false" json:"whitelist"`
	BackupData  bool      `gorm:"default:false" json:"backup_data"`
	IsEssential bool      `gorm:"default:false;index" json:"is_essential"`
	DownloadUrl string    `gorm:"type:varchar(512)" json:"download_url"`
	ApkVersion  string    `gorm:"type:varchar(32);default:''" json:"apk_version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName 指定表名
func (Application) TableName() string {
	return "applications"
}
