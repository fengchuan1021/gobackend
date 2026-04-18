package model

import (
	"time"
)

const (
	BackupStatusPending   = 0
	BackupStatusRunning   = 1
	BackupStatusCompleted = 2
	BackupStatusFailed    = 3
)

type Backup struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Serial    string    `gorm:"column:serial;type:varchar(128);index;not null" json:"serial"`
	Pkgs      string    `gorm:"type:varchar(1024)" json:"pkgs"`
	Status    int       `gorm:"default:0" json:"status"`
	Progress  int       `gorm:"default:0" json:"progress"`
	UserID    uint      `gorm:"index;not null;default:0" json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName 指定表名
func (Backup) TableName() string {
	return "backups"
}
