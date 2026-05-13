package third

import (
	"time"
)

// QuNaTaskSummary 去哪儿任务批次汇总
type QuNaTaskSummary struct {
	ID                uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	TotalTasks        int        `gorm:"not null;default:0" json:"total_tasks"`
	CompletedTasks    int        `gorm:"not null;default:0" json:"completed_tasks"`
	StartTime         *time.Time `json:"start_time"`
	EndTime           *time.Time `json:"end_time"`
	TimeoutTasks      int        `gorm:"not null;default:0" json:"timeout_tasks"`
	PublisherID       uint       `gorm:"index;not null" json:"publisher_id"`
	PublisherUsername string     `gorm:"type:varchar(255);not null;default:''" json:"publisher_username"`
}

func (QuNaTaskSummary) TableName() string {
	return "qu_na_task_summaries"
}
