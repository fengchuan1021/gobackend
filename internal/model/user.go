package model

import (
	"time"
)

// User 用户模型
type User struct {
	ID              uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Username        string    `gorm:"type:varchar(64);uniqueIndex;not null" json:"username"`
	Password        string    `gorm:"type:varchar(255);not null" json:"-"`
	ParentID        *uint     `gorm:"index" json:"parent_id"` // 添加入，为 nil 表示超级管理员添加
	IsBanned        bool      `gorm:"default:false" json:"is_banned"`
	IsActive        bool      `gorm:"default:false" json:"is_active"`
	RegisterTime    time.Time `gorm:"not null" json:"register_time"`
	RoleID          uint      `gorm:"index;not null" json:"role_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	MaxDevicesPerIp int       `gorm:"default:0" json:"max_devices_per_ip"`
}

// TableName 指定表名
func (User) TableName() string {
	return "users"
}
