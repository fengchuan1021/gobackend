package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"gobackend/internal/database"

	"github.com/gin-gonic/gin"
)

const (
	UserIDKey = "user_id"
	RoleIDKey = "role_id"
)

// Auth 从 token 头获取用户 ID，未找到则返回 401
func Auth(c *gin.Context) {
	token := c.GetHeader("token")
	fmt.Println("token", token)
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		c.Abort()
		return
	}

	val, err := database.RDB.Get(c.Request.Context(), "auth:token:"+token).Result()
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "登录已过期"})
		c.Abort()
		return
	}

	parts := strings.SplitN(val, ":", 2)
	if len(parts) != 2 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效 token"})
		c.Abort()
		return
	}
	userID, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效 token"})
		c.Abort()
		return
	}
	roleID, _ := strconv.ParseUint(parts[1], 10, 64)

	c.Set(UserIDKey, uint(userID))
	c.Set(RoleIDKey, uint(roleID))
	c.Next()
}
