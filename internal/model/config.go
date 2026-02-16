package model

import (
	"time"
)

// Config 配置模型（key-value）
type Config struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Key       string    `gorm:"column:config_key;type:varchar(128);uniqueIndex;not null" json:"key"`
	Value     string    `gorm:"type:longtext" json:"value"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (Config) TableName() string {
	return "configs"
}
