package model

import "time"

// 计划任务执行顺序
const (
	PlanTaskExecutionOrderSequential = 1 // 顺序
	PlanTaskExecutionOrderRandom     = 2 // 乱序
)

// PlanTask 计划任务模型
type PlanTask struct {
	ID             uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name           string    `gorm:"type:varchar(255);not null" json:"name"`
	UserID         uint      `gorm:"index;not null" json:"user_id"`
	ExecutionOrder int       `gorm:"not null;default:1" json:"execution_order"` // 1顺序 2乱序
	IsTimedTrigger bool      `gorm:"default:false" json:"is_timed_trigger"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TableName 指定表名
func (PlanTask) TableName() string {
	return "plan_tasks"
}

// PlanTaskItem 计划任务条目模型
type PlanTaskItem struct {
	ID             uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	PlanTaskID     uint      `gorm:"index;not null" json:"plan_task_id"`
	ScriptID       uint      `gorm:"index;not null" json:"script_id"`
	StartTime      string    `gorm:"type:char(5)" json:"start_time"` // 24小时制，格式 HH:mm
	Args           string    `gorm:"type:json" json:"args"`
	TotalRound     int       `gorm:"default:0" json:"total_round"`
	DurationMinute int       `gorm:"default:0" json:"duration_minute"`
	PackageName    string    `gorm:"type:varchar(255)" json:"package_name"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TableName 指定表名
func (PlanTaskItem) TableName() string {
	return "plan_task_items"
}

// DevicePlanTask 设备_计划任务模型
type DevicePlanTask struct {
	ID         uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	DeviceID   uint      `gorm:"not null;index:idx_device_plan_task,priority:1" json:"device_id"`
	PlanTaskID uint      `gorm:"not null;index:idx_device_plan_task,priority:2" json:"plan_task_id"`
	Serial     string    `gorm:"type:varchar(128)" json:"serial"`
	UserID     uint      `gorm:"index;not null" json:"user_id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// TableName 指定表名
func (DevicePlanTask) TableName() string {
	return "device_plan_tasks"
}
