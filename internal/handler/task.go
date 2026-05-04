package handler

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"gobackend/config"
	"gobackend/internal/base21"
	"gobackend/internal/database"
	"gobackend/internal/middleware"
	"gobackend/internal/model"
	"gobackend/internal/udpserver"

	"github.com/gin-gonic/gin"
)

// GetTaskDetailReq 获取任务详情请求
type GetTaskDetailReq struct {
	TaskID int `json:"task_id" binding:"required"`
}

// GetTaskDetail 获取任务详情（含脚本内容），供设备端执行脚本；请求体由 AesRequest 中间件解密后为 {"task_id": ...}
func GetTaskDetail(c *gin.Context) {
	var req GetTaskDetailReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "task_id is required"})
		return
	}
	var task model.Task
	if err := database.DB.Preload("Script").Preload("Device").First(&task, req.TaskID).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "task not found"})
		return
	}
	if task.Device.ExpireAt == nil || task.Device.ExpireAt.Before(time.Now()) {
		c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "device expired"})
		return
	}
	file_path := task.Script.FilePath
	BASE_DIR := config.Cfg.SOLUTION_DIR + "/antares_assets"
	full_path := filepath.Join(BASE_DIR, file_path)
	content, err := os.ReadFile(full_path)
	fmt.Println("content", string(content))
	if err != nil {
		fmt.Println("read file failed", err)
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "read file failed"})
		return
	}
	now := time.Now()
	task.StartTime = &now
	task.Status = model.TaskStatusRunning
	database.DB.Save(&task)

	scriptEncoded := base21.EncodeToString(content)
	commonjs_path := config.Cfg.SOLUTION_DIR + "/antares_assets/common.js"
	commonjs_info, err := os.Stat(commonjs_path)
	var commonjs_version int64 = 1
	if err == nil {
		commonjs_version = commonjs_info.ModTime().Unix()
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"msg":  "ok",
		"data": gin.H{
			"script":          scriptEncoded,
			"args":            task.Args,
			"total_minutes":   task.TotalMinutes,
			"package_name":    task.Script.PackageName,
			"commonjsversion": commonjs_version,
			"scriptid":        fmt.Sprintf("%v", task.ScriptID),
		},
	})
}

type ClientAddTaskReq struct {
	ScriptIDs []int                  `json:"script_ids" binding:"required"`
	Time      int                    `json:"time" binding:"required"`
	Rounds    int                    `json:"rounds" binding:"required"`
	Params    map[string]interface{} `json:"params" binding:"required"`
	Serials   []string               `json:"serials" binding:"required"`
}

func ClientAddTask(c *gin.Context) {
	userID, exists := c.Get(middleware.UserIDKey)
	if !exists {
		fmt.Println("user not found")
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "用户不存在"})
		return
	}
	uid := userID.(uint)
	var user model.User
	if err := database.DB.Where("id = ?", uid).First(&user).Error; err != nil {
		fmt.Println("user not found2")
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "用户不存在"})
		return
	}
	if user.IsActive == false {
		fmt.Println("user not active")
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "用户未激活"})
		return
	}
	var req ClientAddTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Println("input not correct")
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "输入不正确"})
		return
	}
	// argsBytes, err := json.Marshal(req.Params)
	// if err != nil {
	// 	c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "invalid params"})
	// 	return
	// }
	//argsStr := string(argsBytes)
	argsStr := ""
	if len(req.Serials) <= 0 {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "序列号未填"})
	}
	if req.Rounds == 0 {
		req.Rounds = 1
	}
	now := time.Now()
	database.DB.Model(&model.Task{}).
		Where("device_serial in (?) and (status = ? or status = ? or status = ?)", req.Serials, model.TaskStatusNotStarted, model.TaskStatusRunning, model.TaskStatusOnHold).
		Updates(map[string]interface{}{
			"status":   model.TaskStatusAbnormalEnd,
			"end_time": &now,
		})
	added := false

	for _, serial := range req.Serials {
		var device model.Device
		if err := database.DB.Where("serial = ?", serial).First(&device).Error; err != nil {
			continue
		}

		if device.ExpireAt == nil || device.ExpireAt.Before(time.Now()) {
			continue
		}
		var task model.Task
		//random shuffle scriptIDs
		rand.Shuffle(len(req.ScriptIDs), func(i, j int) {
			req.ScriptIDs[i], req.ScriptIDs[j] = req.ScriptIDs[j], req.ScriptIDs[i]
		})
		for _, scriptID := range req.ScriptIDs {
			task = model.Task{
				UserID:       device.UserID,
				DeviceID:     device.ID,
				DeviceSerial: serial,
				ScriptID:     uint(scriptID),
				Args:         argsStr,
				StartTime:    nil,
				EndTime:      nil,
				TotalMinutes: req.Time,
				TotalRound:   req.Rounds,
				LeftRound:    req.Rounds,
				LeftMinute:   req.Time,
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			}
			if err := database.DB.Create(&task).Error; err != nil {
				continue
			}

			added = true
		}
		if task.ID > 0 {
			fmt.Printf("run task %d for serial %s\n", task.ID, serial)
			if len(req.Serials) == 1 {
				go udpserver.SendCommand(serial, udpserver.CmdRunTaskScript, []byte(strconv.Itoa(int(task.ID))), device.UserID)

			} else {
				go udpserver.SendCommand(serial, udpserver.CmdStopTask, []byte(""), 0)
			}
		}
	}

	if added {

		c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "no device found"})
}

type ClientStopTaskReq struct {
	Serials []string `json:"serials" binding:"required"`
}

func ClientStopTask(c *gin.Context) {
	var req ClientStopTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "serials is required"})
		return
	}
	// if err := database.DB.Where("device_serial IN (?)", req.Serials).Delete(&model.Task{}).Error; err != nil {
	// 	c.JSON(http.StatusOK, gin.H{"code": 500, "msg": ""})
	// 	return
	// }
	//delete from tasks where device_serial in (?) and status=1
	if len(req.Serials) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "serials is empty"})
		return
	}
	now := time.Now()
	database.DB.Model(&model.Task{}).
		Where("device_serial in (?) and (status = ? or status = ? or status = ?)", req.Serials, model.TaskStatusNotStarted, model.TaskStatusRunning, model.TaskStatusOnHold).
		Updates(map[string]interface{}{
			"status":   model.TaskStatusAbnormalEnd,
			"end_time": &now,
		})

	for _, serial := range req.Serials {
		go udpserver.SendCommand(serial, udpserver.CmdStopTask, []byte(""), 0)
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok"})
}

type ClientFinishTaskReq struct {
	TaskID int    `json:"task_id" binding:"required"`
	Status int    `json:"status" binding:"required"`
	Serial string `json:"serial" binding:"required"`
}

func ClientFinishTask(c *gin.Context) {
	var req ClientFinishTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Println("input not correct", err)
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "task_id is required"})
		return
	}
	var task model.Task
	if err := database.DB.Where("id = ?", req.TaskID).First(&task).Error; err != nil {
		fmt.Println("task not found", err)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "task not found"})
		return
	}
	now := time.Now()
	task.LeftRound--
	if req.Status == model.TaskStatusCompleted {
		if task.LeftRound > 0 {
			clone := model.Task{
				UserID:       task.UserID,
				DeviceID:     task.DeviceID,
				DeviceSerial: task.DeviceSerial,
				ScriptID:     task.ScriptID,
				Args:         task.Args,
				StartTime:    nil,
				EndTime:      nil,
				TotalMinutes: task.TotalMinutes,
				TotalRound:   task.TotalRound,
				LeftRound:    task.LeftRound,
				LeftMinute:   task.TotalMinutes,
				Status:       model.TaskStatusNotStarted,
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			database.DB.Create(&clone)
		}

		task.EndTime = &now
		task.Status = model.TaskStatusCompleted
		task.LeftRound = 0
		database.DB.Save(&task)

	}

	if req.Status == model.TaskStatusAbnormalEnd {
		task.EndTime = &now
		task.LeftRound = 0
		task.Status = model.TaskStatusAbnormalEnd
		database.DB.Save(&task)
	}

	if req.Status == model.TaskStatusAccountBan {
		task.EndTime = &now
		task.LeftRound = 0
		task.Status = model.TaskStatusAccountBan
		database.DB.Save(&task)
	}
	// var newTask model.Task
	// if err := database.DB.Where("device_serial = ? and (status=0 or status=3 or status=1)", req.Serial).Order("left_round desc").First(&newTask).Error; err != nil {
	// 	c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "task not found"})
	// 	return
	// }
	// if newTask.ID == 0 {
	// 	c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "task not found"})
	// 	return
	// }
	// go udpserver.SendCommand(req.Serial, udpserver.CmdRunTaskScript, []byte(strconv.Itoa(int(newTask.ID))), newTask.UserID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok"})
}

// TaskExecutionStatsReq 批量任务执行统计（仅正常结束）
type TaskExecutionStatsReq struct {
	DeviceSerial string `json:"device_serial" binding:"required"`
	ScriptIDs    []uint `json:"script_ids"`
}

// completedTaskDurationMinutes 单次正常结束任务的执行时长（分钟）。
// end 不早于 start 时直接取时间差；若 end 早于 start 则视为跨自然日：start 到 start 当日 24:00 + end 当日 0:00 到 end。
func completedTaskDurationMinutes(start, end time.Time, loc *time.Location) int {
	if !end.Before(start) {
		return int(end.Sub(start).Minutes())
	}
	startL := start.In(loc)
	endL := end.In(loc)
	startDay := time.Date(startL.Year(), startL.Month(), startL.Day(), 0, 0, 0, 0, loc)
	nextMidnight := startDay.AddDate(0, 0, 1)
	part1 := nextMidnight.Sub(startL)
	endDayStart := time.Date(endL.Year(), endL.Month(), endL.Day(), 0, 0, 0, 0, loc)
	part2 := endL.Sub(endDayStart)
	m := int((part1 + part2).Minutes())
	if m < 0 {
		return 0
	}
	return m
}

// computeExecutionStatsPayload 将同一 script 下的已完成任务聚合成 last_7_days / today_durations（按开始日归属）
func computeExecutionStatsPayload(tasks []model.Task, loc *time.Location, todayStart time.Time, todayKey string) gin.H {
	dayTotals := make(map[string]int)
	type runRec struct {
		start time.Time
		min   int
	}
	var todayRuns []runRec

	for _, t := range tasks {
		if t.EndTime == nil || t.StartTime == nil {
			continue
		}
		// 如果 StartTime 和 EndTime 的时间差小于 10 分钟，跳过
		if t.EndTime.Sub(*t.StartTime).Minutes() < 10 {
			continue
		}
		mins := completedTaskDurationMinutes(*t.StartTime, *t.EndTime, loc)
		if mins < 0 {
			mins = 0
		}
		startLocal := t.StartTime.In(loc)
		key := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, loc).Format("2006-01-02")
		dayTotals[key] += mins
		if key == todayKey {
			todayRuns = append(todayRuns, runRec{start: *t.StartTime, min: mins})
		}
	}
	sort.Slice(todayRuns, func(i, j int) bool {
		return todayRuns[i].start.Before(todayRuns[j].start)
	})
	todayDurations := make([]int, 0, len(todayRuns))
	for _, r := range todayRuns {
		todayDurations = append(todayDurations, r.min)
	}
	last7 := make([]gin.H, 0, 7)
	for i := 0; i < 7; i++ {
		d := todayStart.AddDate(0, 0, -7+i)
		key := d.Format("2006-01-02")
		total := dayTotals[key]
		last7 = append(last7, gin.H{
			"total_minutes": total,
			"executed":      total > 0,
		})
	}
	return gin.H{
		"last_7_days":     last7,
		"today_durations": todayDurations,
	}
}

// GetTaskExecutionStats 批量：device_serial + script_ids，一次查询统计多个脚本（仅 status=正常结束）
func GetTaskExecutionStats(c *gin.Context) {
	userID, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "未登录"})
		return
	}
	uid := userID.(uint)
	var req TaskExecutionStatsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "device_serial 必填"})
		return
	}
	serial := req.DeviceSerial
	if serial == "" {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "device_serial 无效"})
		return
	}
	var device model.Device
	if err := database.DB.Where("serial = ? AND user_id = ?", serial, uid).First(&device).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "设备不存在或无权限"})
		return
	}
	seen := make(map[uint]struct{})
	var scriptIDs []uint
	for _, id := range req.ScriptIDs {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		scriptIDs = append(scriptIDs, id)
	}

	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	todayStart := time.Date(
		now.Year(), now.Month(), now.Day(),
		0, 0, 0, 0, loc,
	)
	todayKey := todayStart.Format("2006-01-02")
	cutoff := todayStart.AddDate(0, 0, -7)

	statsOut := gin.H{}
	if len(scriptIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"msg":  "ok",
			"data": gin.H{"stats": statsOut},
		})
		return
	}

	var tasks []model.Task
	if err := database.DB.Where(
		"device_serial = ? AND (status = ? OR status = ? ) AND start_time >= ?  AND script_id IN ?",
		serial, model.TaskStatusCompleted, model.TaskStatusAbnormalEnd, cutoff, scriptIDs,
	).Find(&tasks).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "查询失败"})
		return
	}

	byScript := make(map[uint][]model.Task)
	for _, t := range tasks {
		byScript[t.ScriptID] = append(byScript[t.ScriptID], t)
	}
	for _, id := range scriptIDs {
		list := byScript[id]
		statsOut[strconv.FormatUint(uint64(id), 10)] = computeExecutionStatsPayload(list, loc, todayStart, todayKey)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"msg":  "ok",
		"data": gin.H{
			"stats": statsOut,
		},
	})
}
