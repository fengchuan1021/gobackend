package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"gobackend/internal/base21"
	"gobackend/internal/database"
	"gobackend/internal/model"

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
	if err := database.DB.Preload("Script").First(&task, req.TaskID).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "task not found"})
		return
	}
	if task.ScriptID == 0 || task.Script.ID == 0 {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "script not found"})
		return
	}
	file_path := task.Script.FilePath
	BASE_DIR := "/root/scorpio/antares_scripts"
	full_path := filepath.Join(BASE_DIR, file_path)
	content, err := os.ReadFile(full_path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "read file failed"})
		return
	}
	scriptEncoded := base21.EncodeToString(content)
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
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
	argsBytes, err := json.Marshal(req.Params)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "invalid params"})
		return
	}
	argsStr := string(argsBytes)

	added := false
	for _, serial := range req.Serials {
		var device model.Device
		if err := database.DB.Where("serial = ?", serial).First(&device).Error; err != nil {
			continue
		}
		if device.ExpireAt == nil || device.ExpireAt.Before(time.Now()) {
			continue
		}

		for _, scriptID := range req.ScriptIDs {
			task := model.Task{
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
	}
	if added {
		c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "no device found"})
}
