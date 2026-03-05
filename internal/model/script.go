package model

import (
	"time"
)

// Script 脚本模型
type Script struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string    `gorm:"type:varchar(255);not null" json:"name"`
	IconURL     string    `gorm:"type:varchar(512)" json:"icon_url"`
	CategoryID  uint      `gorm:"index;not null" json:"category_id"`
	FilePath    string    `gorm:"type:varchar(255);" json:"file_path"`
	Description string    `gorm:"type:text" json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	PackageName string         `gorm:"type:varchar(255);not null" json:"package_name"`
	Category    ScriptCategory `gorm:"foreignKey:CategoryID" json:"category,omitempty"`
}

// TableName 指定表名
func (Script) TableName() string {
	return "scripts"
}
