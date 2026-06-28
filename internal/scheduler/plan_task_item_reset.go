package scheduler

import (
	"context"
	"log"
	"time"

	"gobackend/internal/database"
	"gobackend/internal/model"
	"gobackend/internal/udpserver"
)

const planTaskItemResetHour = 0
const planTaskItemResetMinute = 1
const planTaskItemRedisBatchSize = 1000

// nextPlanTaskItemResetTime 返回下一次每日重置时刻（本地时区 00:01）。
func nextPlanTaskItemResetTime(now time.Time) time.Time {
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), planTaskItemResetHour, planTaskItemResetMinute, 0, 0, loc)
	if now.Before(today) {
		return today
	}
	return today.Add(24 * time.Hour)
}

// ResetPlanTaskItemLeftRounds 按 DevicePlanTask 关联关系，将 Redis 中
// device_script_left_round:{deviceID}_{scriptID} 重置为对应 PlanTaskItem 的 total_round。
func ResetPlanTaskItemLeftRounds() error {
	if database.RDB == nil {
		return nil
	}

	var devicePlanTasks []model.DevicePlanTask
	if err := database.DB.Find(&devicePlanTasks).Error; err != nil {
		return err
	}
	if len(devicePlanTasks) == 0 {
		return nil
	}

	planTaskIDSet := make(map[uint]struct{}, len(devicePlanTasks))
	for _, dpt := range devicePlanTasks {
		planTaskIDSet[dpt.PlanTaskID] = struct{}{}
	}
	planTaskIDs := make([]uint, 0, len(planTaskIDSet))
	for id := range planTaskIDSet {
		planTaskIDs = append(planTaskIDs, id)
	}

	var items []model.PlanTaskItem
	if err := database.DB.Where("plan_task_id IN ?", planTaskIDs).Find(&items).Error; err != nil {
		return err
	}
	itemsByPlan := make(map[uint][]model.PlanTaskItem, len(planTaskIDs))
	for _, item := range items {
		itemsByPlan[item.PlanTaskID] = append(itemsByPlan[item.PlanTaskID], item)
	}

	ctx := context.Background()
	pipe := database.RDB.Pipeline()
	batchCount := 0
	totalCount := 0
	flush := func() error {
		if batchCount == 0 {
			return nil
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
		pipe = database.RDB.Pipeline()
		batchCount = 0
		return nil
	}
	for _, dpt := range devicePlanTasks {
		for _, item := range itemsByPlan[dpt.PlanTaskID] {
			if item.ScriptID == 0 {
				continue
			}
			totalRound := item.TotalRound
			if totalRound <= 0 {
				totalRound = 1
			}
			key := udpserver.DeviceScriptLeftRoundKey(dpt.DeviceID, item.ScriptID)
			pipe.Set(ctx, key, totalRound, 0)
			batchCount++
			totalCount++
			if batchCount >= planTaskItemRedisBatchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
	if totalCount == 0 {
		return nil
	}
	if err := flush(); err != nil {
		return err
	}
	log.Printf("plan task item left_round reset: %d keys", totalCount)
	return nil
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
