package model

import (
	"time"

	"gorm.io/gorm"
)

// Script 脚本模型
type Script struct {
	ID          uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string         `gorm:"type:varchar(255);not null" json:"name"`
	IconURL     string         `gorm:"type:varchar(512)" json:"icon_url"`
	CategoryID  uint           `gorm:"index;not null" json:"category_id"`
	Content     string         `gorm:"type:longtext" json:"content"`
	Description string         `gorm:"type:text" json:"description"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`

	Category ScriptCategory `gorm:"foreignKey:CategoryID" json:"category,omitempty"`
}

// TableName 指定表名
func (Script) TableName() string {
	return "scripts"
}
