package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gobackend/config"

	"github.com/gin-gonic/gin"
)

type randomContact struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

func GetRandomContacts(c *gin.Context) {
	var req struct {
		Serial   string `json:"serial"`
		Language string `json:"language"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "参数错误"})
		return
	}

	script := filepath.Join(config.Cfg.BASE_DIR, "cmd", "pythonclaw", "getrandomcontacts.py")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", script, strings.TrimSpace(req.Language))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": msg})
		return
	}

	var contacts []randomContact
	if err := json.Unmarshal(bytes.TrimSpace(out), &contacts); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "解析联系人失败"})
		return
	}
	if len(contacts) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "未生成联系人"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok", "data": contacts})
}
