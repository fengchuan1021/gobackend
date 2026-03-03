package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"gobackend/internal/aes_utils"
	"gobackend/internal/database"
	"gobackend/internal/middleware"
	"gobackend/internal/model"
	"gobackend/internal/websocket"

	"github.com/gin-gonic/gin"
)

// AppendLog 接收客户端日志：路径参数 serial，请求体为消息内容，转发给 WebSocket
// POST /api/devices/:serial/appendLog?tag=test
func AppendLog(c *gin.Context) {
	serial := c.Param("serial")
	if serial == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 serial"})
		return
	}
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}
	message := string(body)

	if websocket.DefaultHub != nil {
		tag := c.Query("tag")
		if tag == "" {
			tag = "info"
		}
		at := time.Now().Format(time.RFC3339)
		payload, _ := json.Marshal(map[string]string{
			"level": tag, "message": message, "at": at,
		})
		websocket.DefaultHub.BroadcastToMonitor(serial, payload)
	}

	c.JSON(http.StatusOK, gin.H{"code": 0})
}

// RegisterDeviceReq 设备注册请求
type RegisterDeviceReq struct {
	Serial string `json:"serial" binding:"required"`
	Token  string `json:"token"`
}

// RegisterDevice 设备注册，若设备不存在则入库
// POST /api/devices/register
func RegisterDevice(c *gin.Context) {
	var aes_req aes_utils.Aes_request
	var req RegisterDeviceReq
	if err := c.ShouldBindJSON(&aes_req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
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
	var device model.Device
	err = database.DB.Where("serial = ?", req.Serial).First(&device).Error
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "设备已存在", "data": device})
		return
	}

	device = model.Device{Serial: req.Serial}
	if err := database.DB.Create(&device).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "注册失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "注册成功", "data": device})
}

// GetInitShellScripts 获取 init shell scripts 配置
// POST /api/devices/getinitshellscripts
func GetInitShellScripts(c *gin.Context) {
	var cfg model.Config
	err := database.DB.Where("config_key = ?", "initshellscripts").First(&cfg).Error
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": ""})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfg.Value})
}

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
	Username string  `json:"username"`
	ExpireAt *string `json:"expire_at"` // ISO8601 如 2025-12-31，null 表示清除到期时间
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
