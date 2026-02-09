package handler

import (
	"bufio"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/gin-gonic/gin"
)

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
// 返回 PNG 图片流
func GetScreenShot(c *gin.Context) {
	serial := c.Query("serial")
	if serial == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "serial 参数必填"})
		return
	}

	// adb -s <serial> exec-out screencap -p
	cmd := exec.Command("adb", "-s", serial, "exec-out", "screencap", "-p")
	out, err := cmd.Output()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "截图获取失败: " + err.Error()})
		return
	}

	if len(out) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "截图为空"})
		return
	}

	c.Data(http.StatusOK, "image/png", out)
}
