package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"gobackend/internal/database"
	"gobackend/internal/middleware"
	"gobackend/internal/model"
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
			tasks[i].BeginTime = &now
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

func UploadQuNaTask(c *gin.Context) {
	userIDVal, ok := c.Get(middleware.UserIDKey)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未登录"})
		return
	}
	userID, ok := userIDVal.(uint)
	if !ok || userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "无效用户"})
		return
	}

	var user model.User
	if err := database.DB.Select("id", "username").Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "用户不存在"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请选择上传文件"})
		return
	}

	fh, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "文件读取失败"})
		return
	}
	defer fh.Close()

	tasks := make([]third.QuNaTask, 0, 128)

	bodyBytes, err := io.ReadAll(fh)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "文件读取失败"})
		return
	}

	type uploadQuNaTaskItem struct {
		CityUrl   string `json:"cityUrl"`
		City      string `json:"city"`
		HotelId   string `json:"hotelId"`
		HotelName string `json:"hotelName"`
	}

	var items []uploadQuNaTaskItem
	if err := json.Unmarshal(bodyBytes, &items); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "文件格式错误，仅支持 JSON 数组"})
		return
	}

	for _, item := range items {
		if item.CityUrl == "" {
			continue
		}
		tasks = append(tasks, third.QuNaTask{
			CityUrl:   item.CityUrl,
			City:      item.City,
			HotelId:   item.HotelId,
			HotelName: item.HotelName,
			Status:    0,
		})
	}

	if len(tasks) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "文件中没有可导入的数据"})
		return
	}

	now := time.Now()
	for i := range tasks {
		tasks[i].CreatedAt = now
		tasks[i].UpdatedAt = now
	}

	summary := third.QuNaTaskSummary{
		TotalTasks:        len(tasks),
		CompletedTasks:    0,
		StartTime:         &now,
		EndTime:           nil,
		TimeoutTasks:      0,
		PublisherID:       user.ID,
		PublisherUsername: user.Username,
	}

	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&tasks).Error; err != nil {
			return err
		}
		if err := tx.Create(&summary).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "上传失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "上传成功", "data": summary})
}

func GetQuNaTaskSummaryList(c *gin.Context) {
	var list []third.QuNaTaskSummary
	if err := database.DB.Order("id DESC").Limit(100).Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}
