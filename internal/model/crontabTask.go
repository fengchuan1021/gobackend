package model

import (
	"encoding/json"
	"time"
)

// CrontabTask 定时任务：在每日 time_range 窗口内触发执行指定 task_id
// DB 用 TIME 存时:分；对外 JSON 仍用一天内分钟数（0~1439），例如 [540, 720] = 09:00~12:00
type CrontabTask struct {
	ID             uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID         uint      `gorm:"index;not null" json:"user_id"`
	Name           string    `gorm:"type:varchar(255);default:''" json:"name"`
	TaskID         int       `gorm:"index;not null" json:"task_id"` // 对应 tasks.id
	TimeRangeStart time.Time `gorm:"type:time;index;not null" json:"-"`
	TimeRangeEnd   time.Time `gorm:"type:time;not null" json:"-"`
	Enabled        bool      `gorm:"default:true;index" json:"enabled"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (CrontabTask) TableName() string {
	return "crontab_tasks"
}

// MinutesOfDay 把 TIME 转成一天内分钟数
func MinutesOfDay(t time.Time) int {
	return t.Hour()*60 + t.Minute()
}

// TimeFromMinutes 把一天内分钟数转成仅含时:分的 TIME（日期占位为 0000-01-01 UTC）
func TimeFromMinutes(m int) time.Time {
	if m < 0 {
		m = 0
	}
	if m > 1439 {
		m = 1439
	}
	return time.Date(0, 1, 1, m/60, m%60, 0, 0, time.UTC)
}

func (c CrontabTask) TimeRangeMinutes() []int {
	return []int{MinutesOfDay(c.TimeRangeStart), MinutesOfDay(c.TimeRangeEnd)}
}

// MarshalJSON 对外仍返回分钟数
func (c CrontabTask) MarshalJSON() ([]byte, error) {
	type Alias CrontabTask
	return json.Marshal(&struct {
		TimeRangeStart int   `json:"time_range_start"`
		TimeRangeEnd   int   `json:"time_range_end"`
		TimeRange      []int `json:"time_range"`
		*Alias
	}{
		TimeRangeStart: MinutesOfDay(c.TimeRangeStart),
		TimeRangeEnd:   MinutesOfDay(c.TimeRangeEnd),
		TimeRange:      c.TimeRangeMinutes(),
		Alias:          (*Alias)(&c),
	})
}
