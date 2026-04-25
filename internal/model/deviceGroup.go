package model

import (
	"time"
)

// Device 设备模型
type DeviceGroup struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	GroupName string    `gorm:"type:varchar(128)" json:"group_name"`
	UserID    uint      `gorm:"index;not null;default:0" json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      User      `gorm:"foreignKey:UserID;constraint:-" json:"user,omitempty"`
}

// TableName 指定表名
func (DeviceGroup) TableName() string {
	return "device_groups"
}
