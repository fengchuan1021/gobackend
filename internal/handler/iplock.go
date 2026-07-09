package handler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gobackend/internal/database"

	"github.com/gin-gonic/gin"
)

type getIPLockReq struct {
	PackageName string `json:"package_name"`
	Serial      string `json:"serial"`
	Concurrency int    `json:"concurrency"`
}

func newLockUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func clientIPFromRequest(c *gin.Context) string {
	if ip := strings.TrimSpace(c.GetHeader("X-Real-IP")); ip != "" {
		return ip
	}
	return c.ClientIP()
}

// GetIPLock 按 client_ip + package_name 占用一个并发槽位（Redis SET NX EX 2400）。
// 依次尝试 key = client_ip:package_name:n（n=1..concurrency），任一成功则 ip_lock=true。
func GetIPLock(c *gin.Context) {
	var req getIPLockReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "参数错误"})
		return
	}

	packageName := strings.TrimSpace(req.PackageName)
	if packageName == "" {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "package_name is required"})
		return
	}

	concurrency := req.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}

	clientIP := clientIPFromRequest(c)
	if clientIP == "" {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "client ip not found"})
		return
	}

	if database.RDB == nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "redis unavailable"})
		return
	}

	ctx := c.Request.Context()
	//uuid := newLockUUID()
	const lockTTL = 2400 * time.Second

	for n := 1; n <= concurrency; n++ {
		key := fmt.Sprintf("%s:%s:%d", clientIP, packageName, n)
		ok, err := database.RDB.SetNX(ctx, key, req.Serial, lockTTL).Result()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "redis set failed"})
			return
		}
		if ok {
			c.JSON(http.StatusOK, gin.H{
				"code": 200,
				"data": gin.H{
					"ip_lock":      true,
					"ip_lock_slot": n,
				},
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"ip_lock":      false,
			"ip_lock_slot": 0,
		},
	})
}
