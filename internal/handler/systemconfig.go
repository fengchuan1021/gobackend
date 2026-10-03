package handler

import (
	"net/http"
	"strings"

	"gobackend/internal/database"
	"gobackend/internal/model"

	"github.com/gin-gonic/gin"
)

// GetWebRTCConfig 返回画面投屏使用的中继和 STUN 地址。
func GetWebRTCConfig(c *gin.Context) {
	keys := []string{model.ScreenLinkRelayKey, model.ScreenLinkStunKey}
	var configs []model.Config
	if err := database.DB.Where("config_key IN ?", keys).Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取失败"})
		return
	}
	values := map[string]string{}
	for _, cfg := range configs {
		values[cfg.Key] = strings.TrimSpace(cfg.Value)
	}
	c.JSON(http.StatusOK, gin.H{
		"relay": values[model.ScreenLinkRelayKey],
		"stun":  values[model.ScreenLinkStunKey],
	})
}
