package model

import (
	"time"
)

// ScriptCategory 脚本分类模型
type ScriptCategory struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string    `gorm:"type:varchar(255);not null" json:"name"`
	FilePath    string    `gorm:"type:varchar(255);" json:"file_path"`
	IsNew       bool      `gorm:"default:false" json:"is_new"`
	IsHot       bool      `gorm:"default:false" json:"is_hot"`
	Description string    `gorm:"type:text" json:"description"`
	SortOrder   int       `gorm:"index;default:0" json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// 逻辑关联：category_id 在 scripts 表，迁移时不创建数据库外键
	Scripts []Script `gorm:"foreignKey:CategoryID;constraint:-" json:"scripts,omitempty"`
}

// TableName 指定表名
func (ScriptCategory) TableName() string {
	return "script_categories"
}
