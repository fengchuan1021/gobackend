package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gobackend/internal/base21"
	"gobackend/internal/database"
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
	BASE_DIR := "/root/scorpio/antares_assets"
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
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"msg":  "ok",
		"data": gin.H{
			"script": scriptEncoded,
			"task":   task,
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
	var req ClientAddTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "task_id is required"})
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
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "serials is required"})
	}
	if req.Rounds == 0 {
		req.Rounds = 1
	}
	database.DB.Where("device_serial in (?)", req.Serials).Delete(&model.Task{})
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
			go udpserver.SendCommand(serial, udpserver.CmdRunTaskScript, []byte(strconv.Itoa(int(task.ID))))
		}
	}

	if added {

		c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "no device found"})
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

			task.EndTime = &now
			task.Status = model.TaskStatusRoundEnd
			database.DB.Save(&task)
			//go udpserver.SendCommand(req.Serial, udpserver.CmdRunTaskScript, []byte(strconv.Itoa(int(task.ID))))
		} else if task.LeftRound == 0 {
			task.EndTime = &now
			task.Status = model.TaskStatusCompleted
			database.DB.Save(&task)
		}
	}
	if req.Status == model.TaskStatusAbnormalEnd {
		task.EndTime = &now
		task.LeftRound = 0
		task.Status = model.TaskStatusAbnormalEnd
		database.DB.Save(&task)
	}
	var newTask model.Task
	if err := database.DB.Where("device_serial = ? and (status=0 or status=3 or status=1)", req.Serial).Order("left_round desc").First(&newTask).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "task not found"})
		return
	}
	if newTask.ID == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "task not found"})
		return
	}
	go udpserver.SendCommand(req.Serial, udpserver.CmdRunTaskScript, []byte(strconv.Itoa(int(newTask.ID))))
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok"})
}
