package model

import (
	"time"
)

// Device 设备模型
type Device struct {
	ID        uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Serial    string     `gorm:"type:varchar(128);uniqueIndex;not null" json:"serial"`
	Codename  string     `gorm:"type:varchar(128)" json:"codename"`
	UserID    uint       `gorm:"index;not null;default:0" json:"user_id"`
	Username  string     `gorm:"type:varchar(64)" json:"username"`
	ExpireAt  *time.Time `gorm:"index" json:"expire_at"`
	Note      string     `gorm:"type:text" json:"note"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// TableName 指定表名
func (Device) TableName() string {
	return "devices"
}
