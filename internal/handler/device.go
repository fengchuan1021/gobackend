package handler

import (
	"net/http"
	"time"

	"gobackend/internal/database"
	"gobackend/internal/middleware"
	"gobackend/internal/model"

	"github.com/gin-gonic/gin"
)

// SearchDevices 按序列号搜索设备
func SearchDevices(c *gin.Context) {
	_, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	serial := c.Query("serial")
	if serial == "" {
		c.JSON(http.StatusOK, gin.H{"data": []model.Device{}})
		return
	}

	var devices []model.Device
	err := database.DB.Where("serial LIKE ?", "%"+serial+"%").Find(&devices).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": devices})
}

// UpdateDeviceReq 更新设备请求
type UpdateDeviceReq struct {
	Username   string  `json:"username"`
	ExpireAt   *string `json:"expire_at"`   // ISO8601 如 2025-12-31，null 表示清除到期时间
}

// UpdateDevice 更新设备（绑定用户、到期时间）
func UpdateDevice(c *gin.Context) {
	_, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	var req UpdateDeviceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	var device model.Device
	if err := database.DB.First(&device, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "设备不存在"})
		return
	}

	updates := make(map[string]interface{})

	if req.Username != "" {
		var user model.User
		if err := database.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "用户不存在"})
			return
		}
		updates["user_id"] = user.ID
		updates["username"] = user.Username
	}

	if req.ExpireAt != nil {
		if *req.ExpireAt == "" {
			updates["expire_at"] = nil
		} else {
			t, err := time.Parse("2006-01-02", *req.ExpireAt)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "到期时间格式错误，请使用 YYYY-MM-DD"})
				return
			}
			updates["expire_at"] = &t
		}
	}

	if len(updates) > 0 {
		if err := database.DB.Model(&device).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
			return
		}
	}

	database.DB.First(&device, device.ID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功", "data": device})
}
