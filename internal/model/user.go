package model

import (
	"time"

	"gorm.io/gorm"
)

// User 用户模型
type User struct {
	ID           uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Username     string         `gorm:"type:varchar(64);uniqueIndex;not null" json:"username"`
	Password     string         `gorm:"type:varchar(255);not null" json:"-"`
	IsBanned     bool           `gorm:"default:false" json:"is_banned"`
	RegisterTime time.Time      `gorm:"not null" json:"register_time"`
	RoleID       uint           `gorm:"index;not null" json:"role_id"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名
func (User) TableName() string {
	return "users"
}
