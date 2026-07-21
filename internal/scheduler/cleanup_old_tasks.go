package scheduler

import (
	"context"
	"log"
	"time"

	"gobackend/internal/database"
	"gobackend/internal/model"
)

const cleanupOldTasksHour = 1
const cleanupOldTasksMinute = 0

// nextCleanupOldTasksTime 返回下一次每日清理时刻（本地时区 01:00）。
func nextCleanupOldTasksTime(now time.Time) time.Time {
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), cleanupOldTasksHour, cleanupOldTasksMinute, 0, 0, loc)
	if now.Before(today) {
		return today
	}
	return today.Add(24 * time.Hour)
}

// CleanupOldTasks 删除 CreatedAt 早于一个月前的任务。
func CleanupOldTasks() error {
	cutoff := time.Now().AddDate(0, -1, 0)
	result := database.DB.Where("created_at < ?", cutoff).Delete(&model.Task{})
	if result.Error != nil {
		return result.Error
	}
	log.Printf("cleanup old tasks: deleted %d rows (created_at < %s)", result.RowsAffected, cutoff.Format(time.RFC3339))
	return nil
}

// StartCleanupOldTasksLoop 每天 01:00 删除一个月以前的任务。
func StartCleanupOldTasksLoop(ctx context.Context) {
	for {
		next := nextCleanupOldTasksTime(time.Now())
		wait := time.Until(next)
		log.Printf("cleanup old tasks scheduled at %s (in %s)", next.Format(time.RFC3339), wait)

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
			if err := CleanupOldTasks(); err != nil {
				log.Printf("cleanup old tasks failed: %v", err)
			} else {
				log.Printf("cleanup old tasks completed")
			}
		}
	}
}
