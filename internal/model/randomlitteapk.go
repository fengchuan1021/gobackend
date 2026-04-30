package model

import "time"

// RandomLitteApk 随机小游戏 APK 下载信息
type RandomLitteApk struct {
	ID          uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	PackageName string     `gorm:"type:varchar(128);not null;default:'';uniqueIndex:uk_package_name" json:"package_name"`
	DownloadURL string     `gorm:"column:download_url;type:varchar(512);default:''" json:"download_url"`
	CreatedAt   time.Time  `gorm:"column:created_at;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   *time.Time `gorm:"column:updated_at" json:"updated_at"`
}

// TableName 指定表名
func (RandomLitteApk) TableName() string {
	return "random_little_apk"
}
