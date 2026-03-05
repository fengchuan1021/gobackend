package handler

import (
	"net/http"
	"os"
	"path/filepath"

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
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"script": string(content),
			"task":   task,
		},
	})
}
