package handler

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"gobackend/internal/database"
	"gobackend/internal/udpserver"

	"github.com/gin-gonic/gin"
)

const devScriptKeyPrefix = "dev_script:"
const devScriptTTL = 20 * time.Second

// GetDevices 获取已连接的设备列表（adb devices）
// GET /api/dev/getDevices
func GetDevices(c *gin.Context) {
	fmt.Println("GetDevices")
	cmd := exec.Command("adb", "devices")
	out, err := cmd.Output()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取设备列表失败: " + err.Error()})
		return
	}

	var devices []gin.H
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "List of") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[1] == "device" {
			devices = append(devices, gin.H{"serial": parts[0]})
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": devices})
}

// GetScreenShot 根据设备序列号获取截图
// GET /api/dev/getScreenShot?serial=xxx
// 通过 UDP 向设备发送截图命令，返回 PNG 图片流
func GetScreenShot(c *gin.Context) {
	serial := c.Query("serial")
	if serial == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "serial 参数必填"})
		return
	}

	data, err := udpserver.SendCommand(serial, udpserver.CmdGetScreenshot, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "截图获取失败: " + err.Error()})
		return
	}

	if len(data) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "截图为空"})
		return
	}

	// 客户端返回 base64 编码的 PNG
	pngData, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "截图解码失败"})
		return
	}

	if len(pngData) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "截图为空"})
		return
	}

	c.Data(http.StatusOK, "image/png", pngData)
}

// RunDevScriptReq 执行设备脚本请求
type RunDevScriptReq struct {
	Serial string `json:"serial" binding:"required"`
	Script string `json:"script" binding:"required"`
}

// RunDevScript 在指定设备上执行脚本：生成 script_id 存 Redis（20s 过期），UDP 下发 script_id；设备凭 script_id HTTP 拉取脚本内容后执行
// POST /api/dev/runDevScript
func RunDevScript(c *gin.Context) {
	var req RunDevScriptReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误，需 serial 与 script"})
		return
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成 script_id 失败"})
		return
	}
	scriptID := hex.EncodeToString(b)
	key := devScriptKeyPrefix + scriptID
	ctx := context.Background()
	if err := database.RDB.Set(ctx, key, req.Script, devScriptTTL).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "写入脚本缓存失败"})
		return
	}

	data, err := udpserver.SendCommand(req.Serial, udpserver.CmdExecuteDevScript, []byte(scriptID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "执行脚本失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": string(data)})
}

// GetDevScriptContent 根据 script_id 从 Redis 返回脚本内容（设备拉取后执行），未找到或已过期返回 404
// GET /api/dev/getDevScriptContent/:id（可不鉴权，script_id 不可猜测且 20s 过期）
func GetDevScriptContent(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 参数必填"})
		return
	}
	key := devScriptKeyPrefix + id
	ctx := context.Background()
	content, err := database.RDB.Get(ctx, key).Result()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "脚本不存在或已过期"})
		return
	}
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(content))
}
