package model

import "time"

// CrontabTask 定时任务：在每日 time_range 窗口内触发执行指定 task_id
// time_range 单位：一天内的分钟数（0~1439），例如 [540, 720] 表示 09:00~12:00
type CrontabTask struct {
	ID             uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID         uint      `gorm:"index;not null" json:"user_id"`
	Name           string    `gorm:"type:varchar(255);default:''" json:"name"`
	TaskID         int       `gorm:"index;not null" json:"task_id"` // 对应 tasks.id，客户端 engineManager_runRemoteTask
	TimeRangeStart int       `gorm:"index;not null;default:0" json:"time_range_start"`
	TimeRangeEnd   int       `gorm:"not null;default:0" json:"time_range_end"`
	Enabled        bool      `gorm:"default:true;index" json:"enabled"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (CrontabTask) TableName() string {
	return "crontab_tasks"
}
