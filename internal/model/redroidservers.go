package model

import (
	"time"
)

// RedroidServer 云手机服务器
type RedroidServer struct {
	ID         uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	MachinedId string `gorm:"type:varchar(128);uniqueIndex;not null" json:"machined_id"`

	UserID uint `gorm:"index;not null;default:0" json:"user_id"`

	ExpireAt *time.Time `gorm:"index" json:"expire_at"`
	Note     string     `gorm:"type:text" json:"note"`
	Ip       string     `gorm:"type:varchar(128)" json:"ip"`
	User     User       `gorm:"foreignKey:UserID;constraint:-" json:"user,omitempty"`
	Port     int        `gorm:"default:5555" json:"port"`
}

// TableName 指定表名
func (RedroidServer) TableName() string {
	return "redroid_servers"
}
