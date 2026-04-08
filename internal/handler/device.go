package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gobackend/internal/database"
	"gobackend/internal/middleware"
	"gobackend/internal/model"
	"gobackend/internal/udpserver"
	"gobackend/internal/websocket"

	"github.com/gin-gonic/gin"
)

// AppendLog 接收客户端日志：路径参数 serial，请求体为消息内容，转发给 WebSocket
// POST /api/devices/:serial/appendLog?tag=test
func AppendLog(c *gin.Context) {
	serial := c.Param("serial")
	if serial == "" {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "缺少 serial"})
		return
	}
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "读取请求体失败"})
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
// 请求体已由 AesRequest 中间件解密，此处直接绑定明文 JSON
// POST /api/devices/register
func RegisterDevice(c *gin.Context) {
	var req RegisterDeviceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Println(err)
		c.JSON(http.StatusBadRequest, gin.H{"msg": "参数错误"})
		return
	}
	var device model.Device
	err := database.DB.Where("serial = ?", req.Serial).First(&device).Error
	if err == nil {
		fmt.Println(err)
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "设备已存在", "data": device})
		return
	}
	userID, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"msg": "未登录"})
		return
	}
	uid := userID.(uint)

	var user model.User
	if err := database.DB.Where("id = ?", uid).First(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"msg": "用户不存在"})
		return
	}

	device = model.Device{Serial: req.Serial, UserID: uid, Username: user.Username}
	if err := database.DB.Create(&device).Error; err != nil {
		fmt.Println(err)
		c.JSON(http.StatusInternalServerError, gin.H{"msg": "注册失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "注册成功", "data": device})
}

// GetInitShellScripts 获取 init shell scripts 配置（cfg.Value 为 ";" 分割的 shell 语句，返回 JSON 数组）
// POST /api/devices/getinitshellscripts
func GetInitShellScripts(c *gin.Context) {
	var cfg model.Config
	err := database.DB.Where("config_key = ?", "initshellscripts").First(&cfg).Error
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": []string{}})
		return
	}
	parts := strings.Split(cfg.Value, ";")
	scripts := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			scripts = append(scripts, s)
		}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": scripts})
}
func GetDeviceExpireTime(c *gin.Context) {
	serial := c.Query("serial")
	if serial == "" {
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": ""})
		return
	}
	var device model.Device
	err := database.DB.Where("serial = ?", serial).First(&device).Error

	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": ""})
		return
	}
	data := ""
	if device.ExpireAt != nil {
		data = device.ExpireAt.Format("2006-01-02")
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": data})
}

// SearchDevices 按序列号搜索设备
func SearchDevices(c *gin.Context) {
	userIDvalue, exists := c.Get(middleware.UserIDKey)

	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"msg": "未登录"})
		return
	}
	roleID, exists := c.Get(middleware.RoleIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"msg": "未登录"})
		return
	}
	roleIDValue := roleID.(uint)
	userID := userIDvalue.(uint)
	serial := c.Query("serial")
	listAll := c.Query("all") == "1" || c.Query("all") == "true"

	var devices []model.Device
	var err error
	if listAll {
		if roleIDValue != 1 {
			err = database.DB.Where("user_id = ?", userID).Order("id ASC").Find(&devices).Error
		} else {
			err = database.DB.Order("id ASC").Find(&devices).Error
		}

	} else if serial == "" {
		c.JSON(http.StatusOK, gin.H{"data": []model.Device{}})
		return
	} else {
		if roleIDValue != 1 {
			err = database.DB.Where("user_id = ?", userID).Where("serial LIKE ?", "%"+serial+"%").Find(&devices).Error
		} else {
			err = database.DB.Where("serial LIKE ?", "%"+serial+"%").Find(&devices).Error
		}
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"msg": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": devices})
}

// UpdateDeviceReq 更新设备请求
type UpdateDeviceReq struct {
	//Username string  `json:"username"`
	//ExpireAt *string `json:"expire_at"` // ISO8601 如 2025-12-31，null 表示清除到期时间
	// AddDuration 按“月数”增加到期时间。
	// 如果设备原本的 ExpireAt <= 当前时间或为空，则从当前时间开始增加；
	// 否则从原 ExpireAt 开始增加。
	AddDuration *int `json:"add_duration"`
}

// add_device_expire_time 更新设备（增加到期时间）
// PATCH /api/devices/add_device_expire_time/:id
func UpdateDevice(c *gin.Context) {
	userIDRaw, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 500, "msg": "未登录"})
		return
	}
	uid := userIDRaw.(uint)

	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "参数错误"})
		return
	}

	var req UpdateDeviceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 500, "msg": "参数错误"})
		return
	}

	var device model.Device
	if err := database.DB.First(&device, id).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "设备不存在"})
		return
	}
	var deviceUser model.User
	if err := database.DB.First(&deviceUser, device.UserID).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "设备用户不存在"})
		return
	}
	if !deviceUser.IsActive {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "设备用户未激活"})
		return
	}
	updates := make(map[string]interface{})
	var newExpireAt *time.Time
	var months int

	if req.AddDuration != nil {
		months = *req.AddDuration
		if months <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"msg": "add_duration 必须是正数（单位：月）"})
			return
		}

		now := time.Now()
		base := now
		// 按需求：ExpireAt 为空或 <= 当前时间，从当前时间开始；否则从原 ExpireAt 开始。
		if device.ExpireAt != nil && !device.ExpireAt.IsZero() && device.ExpireAt.After(now) {
			base = *device.ExpireAt
		}

		t := base.AddDate(0, months, 0)
		updates["expire_at"] = &t
		newExpireAt = &t
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "add_duration 不能为空"})
		return
	}

	if len(updates) > 0 {
		if err := database.DB.Model(&device).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"msg": "更新失败"})
			return
		}

		// 写入操作日志（记录增到期的月数与新到期时间）
		var user model.User
		if err := database.DB.First(&user, uid).Error; err == nil {
			log := model.Log{
				UserID:       uid,
				Username:     user.Username,
				LogType:      "device_expire_add",
				Remark:       fmt.Sprintf("增加设备到期时间：%d 个月", months),
				DeviceSerial: device.Serial,
				DeviceID:     device.ID,
				AddDuration:  months,
				NewExpireAt:  newExpireAt,
			}
			_ = database.DB.Create(&log).Error
		}
	}

	database.DB.First(&device, device.ID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "更新成功", "data": device})
}

// GetTrickStoreConfig 获取 trick store config
// POST /api/device/gettrickeystoreconfig
func GetTrickStoreConfig(c *gin.Context) {
	var cfg model.TrickStoreConfig
	var req struct {
		Serial string `json:"serial"`
		Model  string `json:"model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "参数错误"})
		return
	}
	err := database.DB.Where("model = ? or model=''", req.Model).Order("id desc").First(&cfg).Error
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "data": nil})
		return
	} else {
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": cfg})
	}
}

// GetWhitelistApps 获取白名单应用
// POST /api/device/getwhitelistapps
func GetWhitelistApps(c *gin.Context) {
	const cacheKey = "whitelistapps:apps"

	ctx := c.Request.Context()

	// 先从 Redis 读取缓存
	if database.RDB != nil {
		if cached, err := database.RDB.Get(ctx, cacheKey).Result(); err == nil && cached != "" {
			var apps []string
			if err := json.Unmarshal([]byte(cached), &apps); err == nil {
				c.JSON(http.StatusOK, gin.H{"code": 0, "data": apps})
				return
			}
		}
	}

	apps := make([]string, 0)
	var rows []string

	// 查询 scripts 中 category_id != 0 的 package_name
	var scriptPackages []string
	if err := database.DB.Table("scripts").Where("category_id != 0").Pluck("package_name", &scriptPackages).Error; err == nil {
		rows = append(rows, scriptPackages...)
	}

	// 查询 applications 中 whitelist = 1 的 package_name
	var appPackages []string
	if err := database.DB.Table("applications").Where("whitelist = 1").Pluck("package_name", &appPackages).Error; err == nil {
		rows = append(rows, appPackages...)
	}

	// 去重，并存到 apps
	appMap := make(map[string]struct{}, len(rows))
	for _, pkg := range rows {
		if pkg = strings.TrimSpace(pkg); pkg != "" {
			appMap[pkg] = struct{}{}
		}
	}
	apps = make([]string, 0, len(appMap))
	for pkg := range appMap {
		apps = append(apps, pkg)
	}

	// 写入 Redis，10 分钟有效期
	if database.RDB != nil {
		if b, err := json.Marshal(apps); err == nil {
			_ = database.RDB.Set(ctx, cacheKey, b, 10*time.Minute).Err()
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": apps})
}
func SaveProfileNote(c *gin.Context) {
	serial := c.Param("serial")
	if serial == "" {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "缺少 serial"})
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "参数错误"})
		return
	}
	database.DB.Model(&model.Device{}).Where("serial = ?", serial).Update("note", req.Note)
	c.JSON(http.StatusOK, gin.H{"msg": "保存成功"})
}
func GetProfileNote(c *gin.Context) {
	serial := c.Param("serial")
	if serial == "" {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "缺少 serial"})
		return
	}
	var device model.Device
	err := database.DB.Where("serial = ?", serial).First(&device).Error
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "设备不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"msg": "获取成功", "data": device.Note})

}
func ResetDeviceBySerial(c *gin.Context) {
	serial := c.Param("serial")
	if serial == "" {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "缺少 serial"})
		return
	}
	go udpserver.SendCommand(serial, udpserver.CmdResetDevice, []byte(""))

	c.JSON(http.StatusOK, gin.H{"msg": "重置成功"})
}
