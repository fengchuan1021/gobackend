package model

import "time"

// Log 设备/系统日志记录
type Log struct {
	ID           uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID       uint       `gorm:"index;not null;default:0" json:"user_id"`
	Username     string     `gorm:"type:varchar(64)" json:"username"`
	LogType      string     `gorm:"type:varchar(64);index" json:"log_type"`
	Remark       string     `gorm:"type:string" json:"remark"`
	DeviceSerial string     `gorm:"type:varchar(128)" json:"device_serial"`
	DeviceID     uint       `gorm:"index;not null" json:"device_id"`
	AddDuration  int        `gorm:"default:0" json:"add_duration"`
	NewExpireAt  *time.Time `gorm:"index" json:"new_expire_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

// TableName 指定表名
func (Log) TableName() string {
	return "logs"
}
