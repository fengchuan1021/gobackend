package handler

import (
	"net/http"
	"strings"

	"gobackend/internal/database"
	"gobackend/internal/middleware"
	"gobackend/internal/model"

	"github.com/gin-gonic/gin"
)

// planTaskDeviceItem 计划任务下设备的扁平视图
type planTaskDeviceItem struct {
	ID            uint   `json:"id"`             // device_plan_tasks.id
	DeviceID      uint   `json:"device_id"`      // 设备 id
	Serial        string `json:"serial"`         // 设备序列号
	ProfileSerial string `json:"profile_serial"` // 设备编号(展示用)
}

// planTaskItemEntry 计划任务条目的扁平视图
type planTaskItemEntry struct {
	ID             uint   `json:"id"`
	PlanTaskID     uint   `json:"plan_task_id"`
	ScriptID       uint   `json:"script_id"`
	ScriptName     string `json:"script_name"`
	StartTime      string `json:"start_time"`
	Args           string `json:"args"`
	TotalRound     int    `json:"total_round"`
	DurationMinute int    `json:"duration_minute"`
	PackageName    string `json:"package_name"`
}

// planTaskItem 列表项视图
type planTaskItemView struct {
	ID             uint                 `json:"id"`
	Name           string               `json:"name"`
	UserID         uint                 `json:"user_id"`
	ExecutionOrder int                  `json:"execution_order"` // 1顺序 2乱序
	IsTimedTrigger bool                 `json:"is_timed_trigger"`
	Devices        []planTaskDeviceItem `json:"devices"`
	Items          []planTaskItemEntry  `json:"items"`
}

// ListPlanTasks 列出当前用户的全部计划任务及其设备
// POST /api/plan_tasks/list
func ListPlanTasks(c *gin.Context) {
	userIDRaw, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 500, "msg": "未登录"})
		return
	}
	uid := userIDRaw.(uint)

	var tasks []model.PlanTask
	if err := database.DB.Where("user_id = ?", uid).Order("id DESC").Find(&tasks).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "查询计划任务失败"})
		return
	}
	if len(tasks) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok", "data": []planTaskItemView{}})
		return
	}

	taskIDs := make([]uint, 0, len(tasks))
	for _, t := range tasks {
		taskIDs = append(taskIDs, t.ID)
	}

	var devicePlanTasks []model.DevicePlanTask
	if err := database.DB.Where("plan_task_id IN (?) AND user_id = ?", taskIDs, uid).Find(&devicePlanTasks).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "查询计划任务设备失败"})
		return
	}

	deviceIDSet := make(map[uint]struct{}, len(devicePlanTasks))
	for _, d := range devicePlanTasks {
		deviceIDSet[d.DeviceID] = struct{}{}
	}
	deviceIDs := make([]uint, 0, len(deviceIDSet))
	for id := range deviceIDSet {
		deviceIDs = append(deviceIDs, id)
	}
	deviceMap := make(map[uint]model.Device, len(deviceIDs))
	if len(deviceIDs) > 0 {
		var devices []model.Device
		if err := database.DB.Where("id IN (?)", deviceIDs).Find(&devices).Error; err == nil {
			for _, d := range devices {
				deviceMap[d.ID] = d
			}
		}
	}

	groupedByTask := make(map[uint][]planTaskDeviceItem, len(tasks))
	for _, dpt := range devicePlanTasks {
		dev, ok := deviceMap[dpt.DeviceID]
		serial := dpt.Serial
		profile := ""
		if ok {
			if serial == "" {
				serial = dev.Serial
			}
			profile = dev.ProfileSerial
		}
		groupedByTask[dpt.PlanTaskID] = append(groupedByTask[dpt.PlanTaskID], planTaskDeviceItem{
			ID:            dpt.ID,
			DeviceID:      dpt.DeviceID,
			Serial:        serial,
			ProfileSerial: profile,
		})
	}

	var planTaskItems []model.PlanTaskItem
	if err := database.DB.Where("plan_task_id IN (?)", taskIDs).Order("id ASC").Find(&planTaskItems).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "查询计划任务条目失败"})
		return
	}
	scriptIDSet := make(map[uint]struct{}, len(planTaskItems))
	for _, it := range planTaskItems {
		scriptIDSet[it.ScriptID] = struct{}{}
	}
	scriptIDs := make([]uint, 0, len(scriptIDSet))
	for id := range scriptIDSet {
		scriptIDs = append(scriptIDs, id)
	}
	scriptMap := make(map[uint]model.Script, len(scriptIDs))
	if len(scriptIDs) > 0 {
		var scripts []model.Script
		if err := database.DB.Where("id IN (?)", scriptIDs).Find(&scripts).Error; err == nil {
			for _, s := range scripts {
				scriptMap[s.ID] = s
			}
		}
	}
	itemsByTask := make(map[uint][]planTaskItemEntry, len(tasks))
	for _, it := range planTaskItems {
		scriptName := ""
		pkg := it.PackageName
		if s, ok := scriptMap[it.ScriptID]; ok {
			scriptName = s.Name
			if pkg == "" {
				pkg = s.PackageName
			}
		}
		itemsByTask[it.PlanTaskID] = append(itemsByTask[it.PlanTaskID], planTaskItemEntry{
			ID:             it.ID,
			PlanTaskID:     it.PlanTaskID,
			ScriptID:       it.ScriptID,
			ScriptName:     scriptName,
			StartTime:      it.StartTime,
			Args:           it.Args,
			TotalRound:     it.TotalRound,
			DurationMinute: it.DurationMinute,
			PackageName:    pkg,
		})
	}

	result := make([]planTaskItemView, 0, len(tasks))
	for _, t := range tasks {
		result = append(result, planTaskItemView{
			ID:             t.ID,
			Name:           t.Name,
			UserID:         t.UserID,
			ExecutionOrder: t.ExecutionOrder,
			IsTimedTrigger: t.IsTimedTrigger,
			Devices:        groupedByTask[t.ID],
			Items:          itemsByTask[t.ID],
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok", "data": result})
}

type savePlanTaskReq struct {
	ID             uint   `json:"id"`
	Name           string `json:"name" binding:"required"`
	ExecutionOrder int    `json:"execution_order"`
	IsTimedTrigger bool   `json:"is_timed_trigger"`
}

// CreatePlanTask 创建计划任务
// POST /api/plan_tasks/create
func CreatePlanTask(c *gin.Context) {
	userIDRaw, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 500, "msg": "未登录"})
		return
	}
	uid := userIDRaw.(uint)

	var req savePlanTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "参数错误"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "名称不能为空"})
		return
	}
	order := normalizePlanTaskOrder(req.ExecutionOrder)

	planTask := model.PlanTask{
		Name:           name,
		UserID:         uid,
		ExecutionOrder: order,
		IsTimedTrigger: req.IsTimedTrigger,
	}
	if err := database.DB.Create(&planTask).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok", "data": planTask})
}

// UpdatePlanTask 修改计划任务
// POST /api/plan_tasks/update
func UpdatePlanTask(c *gin.Context) {
	userIDRaw, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 500, "msg": "未登录"})
		return
	}
	uid := userIDRaw.(uint)

	var req savePlanTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "参数错误"})
		return
	}
	if req.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "缺少 id"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "名称不能为空"})
		return
	}
	var planTask model.PlanTask
	if err := database.DB.Where("id = ? AND user_id = ?", req.ID, uid).First(&planTask).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "计划任务不存在"})
		return
	}
	planTask.Name = name
	planTask.ExecutionOrder = normalizePlanTaskOrder(req.ExecutionOrder)
	planTask.IsTimedTrigger = req.IsTimedTrigger
	if err := database.DB.Save(&planTask).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok", "data": planTask})
}

// DeletePlanTask 删除计划任务（同时清理 device_plan_tasks）
// POST /api/plan_tasks/delete
func DeletePlanTask(c *gin.Context) {
	userIDRaw, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 500, "msg": "未登录"})
		return
	}
	uid := userIDRaw.(uint)

	var req struct {
		ID uint `json:"id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "参数错误"})
		return
	}
	var planTask model.PlanTask
	if err := database.DB.Where("id = ? AND user_id = ?", req.ID, uid).First(&planTask).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "计划任务不存在"})
		return
	}
	tx := database.DB.Begin()
	if err := tx.Where("plan_task_id = ? AND user_id = ?", planTask.ID, uid).Delete(&model.DevicePlanTask{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除关联设备失败"})
		return
	}
	if err := tx.Where("plan_task_id = ?", planTask.ID).Delete(&model.PlanTaskItem{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除关联条目失败"})
		return
	}
	if err := tx.Delete(&planTask).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除失败"})
		return
	}
	tx.Commit()
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok"})
}

type setPlanTaskDevicesReq struct {
	PlanTaskID uint     `json:"plan_task_id" binding:"required"`
	Serials    []string `json:"serials"`
}

// SetPlanTaskDevices 全量同步计划任务对应的设备列表
// POST /api/plan_tasks/setDevices
func SetPlanTaskDevices(c *gin.Context) {
	userIDRaw, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 500, "msg": "未登录"})
		return
	}
	uid := userIDRaw.(uint)

	var req setPlanTaskDevicesReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "参数错误"})
		return
	}
	var planTask model.PlanTask
	if err := database.DB.Where("id = ? AND user_id = ?", req.PlanTaskID, uid).First(&planTask).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "计划任务不存在"})
		return
	}

	cleanSerials := make([]string, 0, len(req.Serials))
	seen := make(map[string]struct{}, len(req.Serials))
	for _, s := range req.Serials {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		cleanSerials = append(cleanSerials, s)
	}

	var devices []model.Device
	if len(cleanSerials) > 0 {
		if err := database.DB.Where("serial IN (?) AND user_id = ?", cleanSerials, uid).Find(&devices).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询设备失败"})
			return
		}
	}

	tx := database.DB.Begin()
	if err := tx.Where("plan_task_id = ? AND user_id = ?", planTask.ID, uid).Delete(&model.DevicePlanTask{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "清理旧关联失败"})
		return
	}
	for _, d := range devices {
		row := model.DevicePlanTask{
			DeviceID:   d.ID,
			PlanTaskID: planTask.ID,
			Serial:     d.Serial,
			UserID:     uid,
		}
		if err := tx.Create(&row).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "保存关联失败"})
			return
		}
	}
	tx.Commit()
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok", "data": gin.H{"count": len(devices)}})
}

type planTaskItemReq struct {
	ScriptID       uint   `json:"script_id"`
	StartTime      string `json:"start_time"`
	Args           string `json:"args"`
	TotalRound     int    `json:"total_round"`
	DurationMinute int    `json:"duration_minute"`
	PackageName    string `json:"package_name"`
}

type setPlanTaskItemsReq struct {
	PlanTaskID uint              `json:"plan_task_id" binding:"required"`
	Items      []planTaskItemReq `json:"items"`
}

// SetPlanTaskItems 全量同步指定计划任务的条目
// POST /api/plan_tasks/setItems
func SetPlanTaskItems(c *gin.Context) {
	userIDRaw, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 500, "msg": "未登录"})
		return
	}
	uid := userIDRaw.(uint)

	var req setPlanTaskItemsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "参数错误"})
		return
	}

	var planTask model.PlanTask
	if err := database.DB.Where("id = ? AND user_id = ?", req.PlanTaskID, uid).First(&planTask).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "计划任务不存在"})
		return
	}

	scriptIDSet := make(map[uint]struct{}, len(req.Items))
	for _, it := range req.Items {
		if it.ScriptID > 0 {
			scriptIDSet[it.ScriptID] = struct{}{}
		}
	}
	scriptIDs := make([]uint, 0, len(scriptIDSet))
	for id := range scriptIDSet {
		scriptIDs = append(scriptIDs, id)
	}
	scriptMap := make(map[uint]model.Script, len(scriptIDs))
	if len(scriptIDs) > 0 {
		var scripts []model.Script
		if err := database.DB.Where("id IN (?)", scriptIDs).Find(&scripts).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询脚本失败"})
			return
		}
		for _, s := range scripts {
			scriptMap[s.ID] = s
		}
	}

	tx := database.DB.Begin()
	if err := tx.Where("plan_task_id = ?", planTask.ID).Delete(&model.PlanTaskItem{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "清理旧条目失败"})
		return
	}
	created := 0
	for _, it := range req.Items {
		if it.ScriptID == 0 {
			continue
		}
		script, ok := scriptMap[it.ScriptID]
		if !ok {
			continue
		}
		round := it.TotalRound
		if round <= 0 {
			round = 1
		}
		duration := it.DurationMinute
		if duration <= 0 {
			duration = 40
		}
		startTime := strings.TrimSpace(it.StartTime)
		if !planTask.IsTimedTrigger {
			startTime = ""
		}
		pkg := strings.TrimSpace(it.PackageName)
		if pkg == "" {
			pkg = script.PackageName
		}
		args := it.Args
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		row := model.PlanTaskItem{
			PlanTaskID:     planTask.ID,
			ScriptID:       it.ScriptID,
			StartTime:      startTime,
			Args:           args,
			TotalRound:     round,
			DurationMinute: duration,
			PackageName:    pkg,
		}
		if err := tx.Create(&row).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "保存条目失败"})
			return
		}
		created++
	}
	tx.Commit()
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok", "data": gin.H{"count": created}})
}

// normalizePlanTaskOrder 把任意输入归一为合法的执行顺序值
func normalizePlanTaskOrder(v int) int {
	if v == model.PlanTaskExecutionOrderRandom {
		return model.PlanTaskExecutionOrderRandom
	}
	return model.PlanTaskExecutionOrderSequential
}
