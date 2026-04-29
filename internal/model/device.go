package model

import (
	"time"
)

// Device 设备模型
type Device struct {
	ID            uint        `gorm:"primaryKey;autoIncrement" json:"id"`
	Serial        string      `gorm:"type:varchar(128);uniqueIndex;not null" json:"serial"`
	Codename      string      `gorm:"type:varchar(128)" json:"codename"`
	UserID        uint        `gorm:"index;not null;default:0" json:"user_id"`
	TagID         uint        `gorm:"index;not null;default:0" json:"tag_id"`
	Username      string      `gorm:"type:varchar(64)" json:"username"`
	ExpireAt      *time.Time  `gorm:"index" json:"expire_at"`
	Note          string      `gorm:"type:text" json:"note"`
	ProfileSerial string      `gorm:"type:varchar(64)" json:"profile_serial"`
	GroupID       uint        `gorm:"index;not null;default:0" json:"group_id"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
	SortNumber    int         `gorm:"default:0" json:"sort_number"`
	User          User        `gorm:"foreignKey:UserID;constraint:-" json:"user,omitempty"`
	Tag           Tag         `gorm:"foreignKey:TagID;constraint:-" json:"tag,omitempty"`
	Group         DeviceGroup `gorm:"foreignKey:GroupID;constraint:-" json:"group,omitempty"`
}

// TableName 指定表名
func (Device) TableName() string {
	return "devices"
}
