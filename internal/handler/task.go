package handler

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
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
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "用户不存在"})
		return
	}
	uid := userID.(uint)
	var user model.User
	if err := database.DB.Where("id = ?", uid).First(&user).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "用户不存在"})
		return
	}
	if user.IsActive == false {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "用户未激活"})
		return
	}
	var req ClientAddTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
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
	database.DB.Where("device_serial in (?) and status = ?", req.Serials, model.TaskStatusNotStarted).Delete(&model.Task{})
	now := time.Now()
	database.DB.Model(&model.Task{}).
		Where("device_serial IN ?", req.Serials).
		Where("status = ?", model.TaskStatusRunning).
		Updates(map[string]interface{}{
			"status":   model.TaskStatusAbnormalEnd,
			"end_time": &now,
		})
	database.DB.Model(&model.Task{}).
		Where("device_serial IN ?", req.Serials).
		Where("status IN ?", []int{model.TaskStatusOnHold}).
		Update("status", model.TaskStatusAbnormalEnd)
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
	if err := database.DB.Where("device_serial in (?) and status=1", req.Serials).Delete(&model.Task{}).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "delete task failed"})
		return
	}
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
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "task_id is required"})
		return
	}
	var task model.Task
	if err := database.DB.Where("id = ?", req.TaskID).First(&task).Error; err != nil {
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
