package handler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"gobackend/internal/database"
	"gobackend/internal/middleware"
	"gobackend/internal/model"
	"gobackend/internal/udpserver"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// LoginReq 登录请求
type LoginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResp 登录响应
type LoginResp struct {
	Token string      `json:"token"`
	User  UserProfile `json:"user"`
}

// UserProfile 用户资料（不含密码）
type UserProfile struct {
	ID           uint      `json:"id"`
	Username     string    `json:"username"`
	RoleID       uint      `json:"role_id"`
	IsBanned     bool      `json:"is_banned"`
	RegisterTime time.Time `json:"register_time"`
}
type RegisterReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Register 注册
func Register(c *gin.Context) {
	var req RegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	var existing model.User
	if err := database.DB.Where("username = ?", req.Username).First(&existing).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "用户名已存在"})
		return
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "加密失败"})
		return
	}
	user := model.User{
		Username:     req.Username,
		Password:     string(hashed),
		RoleID:       0,
		IsBanned:     false,
		RegisterTime: time.Now(),
	}
	if err := database.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "注册失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "注册成功"})

}

// Login 登录
func Login(c *gin.Context) {
	var req LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	var user model.User
	if err := database.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	if user.IsBanned {
		c.JSON(http.StatusForbidden, gin.H{"error": "账号已被封禁"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	token := genToken(user.ID, user.RoleID)
	profile := toProfile(&user)
	c.JSON(http.StatusOK, gin.H{"data": LoginResp{Token: token, User: profile}})
}

// CreateUserReq 添加用户请求
type CreateUserReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// CreateUser 添加用户（需登录，parent_id 为当前用户）
func CreateUser(c *gin.Context) {
	userID, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	parentID := userID.(uint)

	var req CreateUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	var existing model.User
	if err := database.DB.Where("username = ?", req.Username).First(&existing).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户名已存在"})
		return
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "加密失败"})
		return
	}

	user := model.User{
		Username:     req.Username,
		Password:     string(hashed),
		ParentID:     &parentID,
		RoleID:       0,
		IsBanned:     false,
		RegisterTime: time.Now(),
	}
	if err := database.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "创建成功", "data": toProfile(&user)})
}

// GetUserProfile 获取当前用户资料
func GetUserProfile(c *gin.Context) {
	userID, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var user model.User
	if err := database.DB.Where("id = ?", userID.(uint)).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": toProfile(&user)})
}

func toProfile(u *model.User) UserProfile {
	return UserProfile{
		ID:           u.ID,
		Username:     u.Username,
		RoleID:       u.RoleID,
		IsBanned:     u.IsBanned,
		RegisterTime: u.RegisterTime,
	}
}

// genToken 生成 token，格式：hex(16字节随机数) + ":" + hex(data 与 b 异或后的结果)，data 为 "user_id:role_id"
func genToken(user_id uint, role_id uint) string {
	b := make([]byte, 16)
	rand.Read(b)
	data := []byte(fmt.Sprintf("%d:%d", user_id, role_id))
	xorResult := make([]byte, len(data))
	for i := range data {
		xorResult[i] = data[i] ^ b[i%16]
	}
	return hex.EncodeToString(b) + ":" + hex.EncodeToString(xorResult)
}

type ActivateUserReq struct {
	Username string `json:"username" binding:"required"`
}

func ActivateUser(c *gin.Context) {
	userID, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "未登录"})
		return
	}
	uid := userID.(uint)
	var req ActivateUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "参数错误"})
		return
	}
	var user model.User
	if err := database.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "用户不存在"})
		return
	}

	// 用事务同时更新用户和写入激活日志
	tx := database.DB.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "激活失败"})
		return
	}

	user.IsActive = true
	if err := tx.Save(&user).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "激活失败"})
		return
	}

	// 获取操作者username（写入日志）
	var operator model.User
	if err := tx.First(&operator, uid).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "激活失败"})
		return
	}

	log := model.UserActivateLog{
		OperatorUID:      uid,
		OperatorUsername: operator.Username,
		TargetUID:        user.ID,
		TargetUsername:   user.Username,
		CreatedAt:        time.Now(),
	}

	if err := tx.Create(&log).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "激活失败"})
		return
	}

	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "激活失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "激活成功"})
}

func SaveIpGroupLimit(c *gin.Context) {
	uid := c.Query("uid")
	if uid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "缺少 uid"})
		return
	}
	var req struct {
		MaxDevicesPerIp int `json:"max_devices_per_ip"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Println("参数错误", err)
		c.JSON(http.StatusBadRequest, gin.H{"msg": "参数错误"})
		return
	}
	fmt.Println("max_devices_per_ip", req.MaxDevicesPerIp)
	database.DB.Model(&model.User{}).Where("id = ?", uid).Update("max_devices_per_ip", req.MaxDevicesPerIp)
	if err := database.DB.Error; err != nil {
		fmt.Println("保存失败", err)
		c.JSON(http.StatusInternalServerError, gin.H{"msg": "保存失败"})
		return
	}
	uidInt, err := strconv.ParseUint(uid, 10, 32)
	if err == nil {
		udpserver.UpdateMaxDevicesPerIp(uint(uidInt), req.MaxDevicesPerIp)
	}

	c.JSON(http.StatusOK, gin.H{"msg": "保存成功"})
}

func GetIpGroupLimit(c *gin.Context) {
	uid := c.Query("uid")
	if uid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "缺少 uid"})
		return
	}
	var user model.User
	if err := database.DB.Where("id = ?", uid).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"msg": "用户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"msg": "获取成功", "data": user.MaxDevicesPerIp})
}
