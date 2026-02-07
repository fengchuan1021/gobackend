package handler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"gobackend/internal/database"
	"gobackend/internal/middleware"
	"gobackend/internal/model"

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

	token := genToken()
	ctx := c.Request.Context()
	key := "auth:token:" + token
	val := fmt.Sprintf("%d:%d", user.ID, user.RoleID)
	if err := database.RDB.Set(ctx, key, val, 365*24*time.Hour).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "登录失败"})
		return
	}

	profile := toProfile(&user)
	c.JSON(http.StatusOK, gin.H{"data": LoginResp{Token: token, User: profile}})
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

func genToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
