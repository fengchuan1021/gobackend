package scheduler

import (
	"context"
	"log"
	"time"

	"gobackend/internal/database"
	"gobackend/internal/model"

	"gorm.io/gorm"
)

const planTaskItemResetHour = 0
const planTaskItemResetMinute = 1

// nextPlanTaskItemResetTime 返回下一次每日重置时刻（本地时区 00:01）。
func nextPlanTaskItemResetTime(now time.Time) time.Time {
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), planTaskItemResetHour, planTaskItemResetMinute, 0, 0, loc)
	if now.Before(today) {
		return today
	}
	return today.Add(24 * time.Hour)
}

// ResetPlanTaskItemLeftRounds 将所有 PlanTaskItem 的 left_round 重置为 total_round。
func ResetPlanTaskItemLeftRounds() error {
	result := database.DB.Model(&model.PlanTaskItem{}).
		Update("left_round", gorm.Expr("total_round"))
	return result.Error
}

// StartPlanTaskItemLeftRoundResetLoop 每天 00:01 重置 PlanTaskItem 剩余轮次。
func StartPlanTaskItemLeftRoundResetLoop(ctx context.Context) {
	for {
		next := nextPlanTaskItemResetTime(time.Now())
		wait := time.Until(next)
		log.Printf("plan task item left_round reset scheduled at %s (in %s)", next.Format(time.RFC3339), wait)

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
			if err := ResetPlanTaskItemLeftRounds(); err != nil {
				log.Printf("plan task item left_round reset failed: %v", err)
			} else {
				log.Printf("plan task item left_round reset completed")
			}
		}
	}
}
