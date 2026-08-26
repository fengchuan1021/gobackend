package handler

import (
	"net/http"

	"gobackend/internal/database"
	"gobackend/internal/middleware"
	"gobackend/internal/model"

	"github.com/gin-gonic/gin"
)

// crontabTaskClientItem 设备端 listCrontabTasks 返回项
type crontabTaskClientItem struct {
	TimeRange   []int  `json:"time_range"`
	TaskID      int    `json:"task_id"`
	PackageName string `json:"package_name"`
}
type gameKeywordClientItem struct {
}
type saveCrontabTaskReq struct {
	ID             uint   `json:"id"`
	Name           string `json:"name"`
	TaskID         int    `json:"task_id"`
	TimeRangeStart int    `json:"time_range_start"`
	TimeRangeEnd   int    `json:"time_range_end"`
	TimeRange      []int  `json:"time_range"` // 可选：[start, end]，优先于 start/end 字段
	Enabled        *bool  `json:"enabled"`
}

type deleteCrontabTaskReq struct {
	ID uint `json:"id" binding:"required"`
}

func normalizeTimeRange(req *saveCrontabTaskReq) (start, end int, ok bool) {
	start, end = req.TimeRangeStart, req.TimeRangeEnd
	if len(req.TimeRange) >= 2 {
		start, end = req.TimeRange[0], req.TimeRange[1]
	}
	if start < 0 || end < 0 || start > 1439 || end > 1439 || start > end {
		return 0, 0, false
	}
	if req.TaskID <= 0 {
		return 0, 0, false
	}
	return start, end, true
}

// ListCrontabTasks 设备端拉取定时任务列表
// POST /api/crontabtasks/listCrontabTasks
// 返回: {"code":200,"data":[{"time_range":[start,end],"task_id":123}, ...]}
func ListCrontabTasks(c *gin.Context) {
	userIDRaw, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 500, "msg": "未登录"})
		return
	}
	uid := userIDRaw.(uint)

	var rows []model.CrontabTask
	if err := database.DB.Where("(user_id = ? or user_id=0) AND enabled = ?", uid, true).
		Order("time_range_start ASC").Find(&rows).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "查询失败", "data": []crontabTaskClientItem{}})
		return
	}

	var row2 []model.GameKeywords
	var out2 [][2]string
	if err := database.DB.Find(&row2).Error; err == nil {
		out2 = make([][2]string, 0, len(row2))
		for _, r := range row2 {
			out2 = append(out2, [2]string{r.Keyword, r.CategoryName})
		}
	}

	out := make([]crontabTaskClientItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, crontabTaskClientItem{
			TimeRange:   r.TimeRangeMinutes(),
			TaskID:      r.TaskID,
			PackageName: r.PackageName,
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok", "crontab_tasks": out, "gamekeywords": out2})
}

// ListCrontabTasksAdmin 管理端完整列表
// POST /api/crontabtasks/list
func ListCrontabTasksAdmin(c *gin.Context) {
	userIDRaw, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 500, "msg": "未登录"})
		return
	}
	uid := userIDRaw.(uint)

	var rows []model.CrontabTask
	if err := database.DB.Where("user_id = ?", uid).Order("id DESC").Find(&rows).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok", "data": rows})
}

// CreateCrontabTask 创建定时任务
// POST /api/crontabtasks/create
func CreateCrontabTask(c *gin.Context) {
	userIDRaw, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 500, "msg": "未登录"})
		return
	}
	uid := userIDRaw.(uint)

	var req saveCrontabTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "参数错误"})
		return
	}
	start, end, ok := normalizeTimeRange(&req)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "time_range/task_id 无效"})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	row := model.CrontabTask{
		UserID:         uid,
		Name:           req.Name,
		TaskID:         req.TaskID,
		TimeRangeStart: model.TimeFromMinutes(start),
		TimeRangeEnd:   model.TimeFromMinutes(end),
		Enabled:        enabled,
	}
	if err := database.DB.Create(&row).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "创建失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok", "data": row})
}

// UpdateCrontabTask 更新定时任务
// POST /api/crontabtasks/update
func UpdateCrontabTask(c *gin.Context) {
	userIDRaw, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 500, "msg": "未登录"})
		return
	}
	uid := userIDRaw.(uint)

	var req saveCrontabTaskReq
	if err := c.ShouldBindJSON(&req); err != nil || req.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "参数错误"})
		return
	}
	start, end, ok := normalizeTimeRange(&req)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "time_range/task_id 无效"})
		return
	}

	var row model.CrontabTask
	if err := database.DB.Where("id = ? AND user_id = ?", req.ID, uid).First(&row).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "记录不存在"})
		return
	}
	row.Name = req.Name
	row.TaskID = req.TaskID
	row.TimeRangeStart = model.TimeFromMinutes(start)
	row.TimeRangeEnd = model.TimeFromMinutes(end)
	if req.Enabled != nil {
		row.Enabled = *req.Enabled
	}
	if err := database.DB.Save(&row).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "更新失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok", "data": row})
}

// DeleteCrontabTask 删除定时任务
// POST /api/crontabtasks/delete
func DeleteCrontabTask(c *gin.Context) {
	userIDRaw, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 500, "msg": "未登录"})
		return
	}
	uid := userIDRaw.(uint)

	var req deleteCrontabTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "参数错误"})
		return
	}
	res := database.DB.Where("id = ? AND user_id = ?", req.ID, uid).Delete(&model.CrontabTask{})
	if res.Error != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "删除失败"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "记录不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok"})
}
