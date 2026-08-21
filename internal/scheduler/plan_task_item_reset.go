package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"gobackend/internal/database"
	"gobackend/internal/model"
	"gobackend/internal/udpserver"

	"github.com/redis/go-redis/v9"
)

const planTaskItemResetHour = 0
const planTaskItemResetMinute = 1
const planTaskItemRedisBatchSize = 1000

// planTaskItemLeftRoundResetDayKey 记录最近一次成功重置的本地日期（YYYY-MM-DD）。
const planTaskItemLeftRoundResetDayKey = "plan_task_item_left_round_reset_day"

const planTaskItemLeftRoundResetDayTTL = 48 * time.Hour

// nextPlanTaskItemResetTime 返回下一次每日重置时刻（本地时区 00:01）。
func nextPlanTaskItemResetTime(now time.Time) time.Time {
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), planTaskItemResetHour, planTaskItemResetMinute, 0, 0, loc)
	if now.Before(today) {
		return today
	}
	return today.Add(24 * time.Hour)
}

func todayResetMoment(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), planTaskItemResetHour, planTaskItemResetMinute, 0, 0, now.Location())
}

func todayDateString(now time.Time) string {
	return now.In(now.Location()).Format("2006-01-02")
}

func isPlanTaskLeftRoundResetDoneToday(ctx context.Context, now time.Time) (bool, error) {
	if database.RDB == nil {
		return false, nil
	}
	val, err := database.RDB.Get(ctx, planTaskItemLeftRoundResetDayKey).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return val == todayDateString(now), nil
}

func markPlanTaskLeftRoundResetToday(ctx context.Context, now time.Time) error {
	if database.RDB == nil {
		return nil
	}
	return database.RDB.Set(ctx, planTaskItemLeftRoundResetDayKey, todayDateString(now), planTaskItemLeftRoundResetDayTTL).Err()
}

// ensurePlanTaskLeftRoundResetToday 若已过当日 00:01 且今天尚未重置，则执行一次重置。
func ensurePlanTaskLeftRoundResetToday(ctx context.Context) error {
	now := time.Now()
	if now.Before(todayResetMoment(now)) {
		return nil
	}
	done, err := isPlanTaskLeftRoundResetDoneToday(ctx, now)
	if err != nil {
		return err
	}
	if done {
		log.Printf("plan task item left_round already reset for %s", todayDateString(now))
		return nil
	}
	if err := ResetPlanTaskItemLeftRounds(); err != nil {
		return err
	}
	log.Printf("plan task item left_round reset completed for %s", todayDateString(now))
	return nil
}

// ResetPlanTaskItemLeftRounds 按 DevicePlanTask 关联关系，将 Redis 中
// device_script_left_round:{deviceID}_{scriptID}_{deviceUserID} 重置为对应 PlanTaskItem 的 total_round。
func ResetPlanTaskItemLeftRounds() error {
	if database.RDB == nil {
		return nil
	}

	db := database.DB.Debug()

	var devicePlanTasks []model.DevicePlanTask
	if err := db.
		Joins("JOIN devices ON devices.id = device_plan_tasks.device_id").
		Where("devices.expire_at > ?", time.Now()).
		Find(&devicePlanTasks).Error; err != nil {
		return err
	}
	if len(devicePlanTasks) == 0 {
		return markPlanTaskLeftRoundResetToday(context.Background(), time.Now())
	}

	planTaskIDSet := make(map[uint]struct{}, len(devicePlanTasks))
	serialSet := make(map[string]struct{}, len(devicePlanTasks))
	for _, dpt := range devicePlanTasks {
		planTaskIDSet[dpt.PlanTaskID] = struct{}{}
		if dpt.Serial != "" {
			serialSet[dpt.Serial] = struct{}{}
		}
	}
	planTaskIDs := make([]uint, 0, len(planTaskIDSet))
	for id := range planTaskIDSet {
		planTaskIDs = append(planTaskIDs, id)
	}
	serials := make([]string, 0, len(serialSet))
	for s := range serialSet {
		serials = append(serials, s)
	}

	var items []model.PlanTaskItem
	if err := db.Where("plan_task_id IN ?", planTaskIDs).Find(&items).Error; err != nil {
		return err
	}
	itemsByPlan := make(map[uint][]model.PlanTaskItem, len(planTaskIDs))
	for _, item := range items {
		itemsByPlan[item.PlanTaskID] = append(itemsByPlan[item.PlanTaskID], item)
	}

	profilesBySerial := make(map[string][]uint, len(serials))
	if len(serials) > 0 {
		var devices []model.Device
		if err := db.Select("serial", "is_svip").Where("serial IN ?", serials).Find(&devices).Error; err != nil {
			return err
		}
		svipSerials := make([]string, 0, len(devices))
		for _, d := range devices {
			if d.IsSvip {
				svipSerials = append(svipSerials, d.Serial)
			} else {
				profilesBySerial[d.Serial] = []uint{0}
			}
		}
		if len(svipSerials) > 0 {
			var profiles []model.DeviceUserProfile
			if err := db.Where("device_serial IN ?", svipSerials).Find(&profiles).Error; err != nil {
				return err
			}
			for _, p := range profiles {
				profilesBySerial[p.DeviceSerial] = append(profilesBySerial[p.DeviceSerial], p.UserID)
			}
		}
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
		deviceUserIDs := profilesBySerial[dpt.Serial]
		if len(deviceUserIDs) == 0 {
			deviceUserIDs = []uint{0}
		}
		for _, item := range itemsByPlan[dpt.PlanTaskID] {
			if item.ScriptID == 0 {
				continue
			}
			totalRound := item.TotalRound
			fmt.Println("totalRound", totalRound)
			if totalRound <= 0 {
				fmt.Println("totalRound <= 0", totalRound)
				totalRound = 1
			}
			for _, deviceUserID := range deviceUserIDs {
				key := udpserver.DevicePlanTaskScriptLeftRoundKey(dpt.DeviceID, item.ScriptID, deviceUserID, item.ID)
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
	}
	if totalCount == 0 {
		return markPlanTaskLeftRoundResetToday(ctx, time.Now())
	}
	if err := flush(); err != nil {
		return err
	}
	if err := markPlanTaskLeftRoundResetToday(ctx, time.Now()); err != nil {
		return err
	}
	log.Printf("plan task item left_round reset: %d keys", totalCount)
	return nil
}

// StartPlanTaskItemLeftRoundResetLoop 每天 00:01 重置 PlanTaskItem 剩余轮次；
// 启动时若今天尚未重置则先补跑；panic 后自动恢复继续调度。
func StartPlanTaskItemLeftRoundResetLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("plan task item left_round reset loop panic: %v", r)
				}
			}()

			if err := ensurePlanTaskLeftRoundResetToday(ctx); err != nil {
				log.Printf("plan task item left_round startup/catch-up reset failed: %v", err)
				return // 交给外层短间隔重试，避免失败后干等到下一天
			}

			next := nextPlanTaskItemResetTime(time.Now())
			wait := time.Until(next)
			if wait < 0 {
				wait = 0
			}
			log.Printf("plan task item left_round reset scheduled at %s (in %s)", next.Format(time.RFC3339), wait)

			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
				if err := ensurePlanTaskLeftRoundResetToday(ctx); err != nil {
					log.Printf("plan task item left_round reset failed: %v", err)
				}
			}
		}()

		if ctx.Err() != nil {
			return
		}
		// 避免 panic 后 tight loop；正常跑完一天也会极短 pause 再进入下一轮调度
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
