package middleware

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	UserIDKey = "user_id"
	RoleIDKey = "role_id"
)

// Auth 从 token 头解析 token（格式与 genToken 一致：hex(16字节b)+":"+hex(data^b)），解密得到 user_id、role_id
func Auth(c *gin.Context) {
	token := c.GetHeader("token")
	if token == "" {
		fmt.Println("not login")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		c.Abort()
		return
	}

	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		fmt.Println("invalid token1")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效 token"})
		c.Abort()
		return
	}

	b, err := hex.DecodeString(parts[0])
	if err != nil || len(b) != 16 {
		fmt.Println("invalid token2")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效 token"})
		c.Abort()
		return
	}

	xorResult, err := hex.DecodeString(parts[1])
	if err != nil || len(xorResult) == 0 {
		fmt.Println("invalid token3")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效 token"})
		c.Abort()
		return
	}

	// data = xorResult ^ b（与 genToken 中异或方向一致，还原出 "user_id:role_id"）
	data := make([]byte, len(xorResult))
	for i := range xorResult {
		data[i] = xorResult[i] ^ b[i%16]
	}

	userRole := strings.SplitN(string(data), ":", 2)
	if len(userRole) != 2 {
		fmt.Println("invalid token4")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效 token"})
		c.Abort()
		return
	}

	userID, err := strconv.ParseUint(userRole[0], 10, 64)
	fmt.Println("userid", userID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效 token"})
		c.Abort()
		return
	}
	roleID, _ := strconv.ParseUint(userRole[1], 10, 64)
	c.Set(UserIDKey, uint(userID))
	c.Set(RoleIDKey, uint(roleID))
	c.Next()
}
