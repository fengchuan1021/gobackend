package model

import "time"

// UserActivateLog 用户激活日志
// 记录操作者与目标用户的激活行为。
type UserActivateLog struct {
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`

	OperatorUID       uint      `gorm:"index;not null" json:"operator_uid"`
	OperatorUsername  string    `gorm:"type:varchar(64)" json:"operator_username"`
	TargetUID         uint      `gorm:"index;not null" json:"target_uid"`
	TargetUsername    string    `gorm:"type:varchar(64)" json:"target_username"`
	CreatedAt         time.Time `json:"created_at"`
}

func (UserActivateLog) TableName() string {
	return "user_activate_logs"
}