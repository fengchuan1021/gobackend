package model

import (
	"time"
)

// Task 任务模型
type Task struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID       uint      `gorm:"index;not null" json:"user_id"`
	DeviceID     uint      `gorm:"index;not null" json:"device_id"`
	DeviceSerial string    `gorm:"index;type:varchar(128)" json:"device_serial"`
	ScriptID     uint      `gorm:"index;not null" json:"script_id"`
	Args         string    `gorm:"type:text" json:"args"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	TotalMinutes int       `gorm:"default:0" json:"total_minutes"`
	TotalRound   int       `gorm:"default:0" json:"total_round"`
	LeftRound    int       `gorm:"default:0" json:"left_round"`
	LeftMinute   int       `gorm:"default:0" json:"left_minute"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	User   User   `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Device Device `gorm:"foreignKey:DeviceID" json:"device,omitempty"`
	Script Script `gorm:"foreignKey:ScriptID" json:"script,omitempty"`
}

// TableName 指定表名
func (Task) TableName() string {
	return "tasks"
}
