package model

import (
	"time"
)

// ScriptCategory 脚本分类模型
type ScriptCategory struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string    `gorm:"type:varchar(255);not null" json:"name"`
	IsNew       bool      `gorm:"default:false" json:"is_new"`
	IsHot       bool      `gorm:"default:false" json:"is_hot"`
	Description string    `gorm:"type:text" json:"description"`
	SortOrder   int       `gorm:"index;default:0" json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName 指定表名
func (ScriptCategory) TableName() string {
	return "script_categories"
}
