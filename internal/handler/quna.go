package handler

import (
	"net/http"
	"time"

	"gobackend/internal/database"
	"gobackend/internal/model/third"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type getQuNaTaskReq struct {
	Serial  string `json:"serial" binding:"required"`
	TaskNum int    `json:"task_num" binding:"required"`
}

func GetQuNaTask(c *gin.Context) {
	var req getQuNaTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	if req.TaskNum <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "task_num 必须大于 0"})
		return
	}

	var tasks []third.QuNaTask
	now := time.Now()

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		// 加行锁，避免并发重复领取同一批任务
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("status = ?", 0).
			Order("id ASC").
			Limit(req.TaskNum).
			Find(&tasks).Error; err != nil {
			return err
		}
		if len(tasks) == 0 {
			return nil
		}

		ids := make([]uint, 0, len(tasks))
		for i := range tasks {
			ids = append(ids, tasks[i].ID)
			tasks[i].Status = 1
			tasks[i].DeviceSerial = req.Serial
			tasks[i].BeginTime = now
		}

		if err := tx.Model(&third.QuNaTask{}).
			Where("id IN ?", ids).
			Updates(map[string]interface{}{
				"status":        1,
				"device_serial": req.Serial,
				"begin_time":    now,
			}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "领取任务失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": tasks})
}

type updateQuNaTaskResultReq struct {
	TaskID uint   `json:"task_id" binding:"required"`
	Result string `json:"result" binding:"required"`
}

func UpdateQuNaTaskResult(c *gin.Context) {
	var req updateQuNaTaskResultReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "参数错误"})
		return
	}
	if err := database.DB.Where("id = ?", req.TaskID).First(&third.QuNaTask{}).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "task not found"})
		return
	}

	if err := database.DB.Model(&third.QuNaTask{}).
		Where("id = ?", req.TaskID).
		Updates(map[string]interface{}{
			"result":   req.Result,
			"status":   2,
			"end_time": time.Now(),
		}).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "update failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok"})
}
