package handler

import (
	"net/http"

	"gobackend/internal/database"
	"gobackend/internal/model"

	"github.com/gin-gonic/gin"
)

// GetTaskDetailReq 获取任务详情请求
type GetTaskDetailReq struct {
	TaskID int `json:"task_id" binding:"required"`
}

// GetTaskDetail 获取任务详情（含脚本内容），供设备端执行脚本
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
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"script": task.Script.Content,
			"task":   task,
		},
	})
}
