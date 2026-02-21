package handler

import (
	"encoding/json"
	"net/http"

	"gobackend/internal/aes_utils"
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
	var aes_req aes_utils.Aes_request
	var req GetTaskDetailReq
	if err := c.ShouldBindJSON(&aes_req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "task_id is required"})
		return
	}
	data, err := aes_utils.Decrypt(aes_req.Data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "decrypt failed"})
		return
	}
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "unmarshal failed"})
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
	content := task.Script.Content
	encrypted_content, err := aes_utils.Encrypt(content)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "encrypt failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"script": encrypted_content,
			"task":   task,
		},
	})
}
