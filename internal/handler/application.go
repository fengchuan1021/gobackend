package handler

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gobackend/internal/database"
	"gobackend/internal/middleware"
	"gobackend/internal/model"

	"github.com/gin-gonic/gin"
)

const (
	wwwrootDir = "/root/scorpio/wwwroot"
	appIconDir = "images/appicon"
)

// ApplicationItem 应用项
type ApplicationItem struct {
	PackageName string `json:"packageName" binding:"required"`
	Name        string `json:"name"`
	IconBase64  string `json:"iconBase64"`
	ToClean     bool   `json:"toClean"`
	BackupData  bool   `json:"backupData"`
}

// SaveApplicationsReq 保存应用请求
type SaveApplicationsReq struct {
	Apps []ApplicationItem `json:"apps" binding:"required"`
}

// SaveApplications 保存应用配置
func SaveApplications(c *gin.Context) {
	_, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var req SaveApplicationsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	for _, app := range req.Apps {
		iconPath := saveIconToFile(app.PackageName, app.IconBase64)

		var existing model.Application
		err := database.DB.Where("package_name = ?", app.PackageName).First(&existing).Error
		if err == nil {
			existing.Name = app.Name
			existing.IconPath = iconPath
			existing.ToClean = app.ToClean
			existing.BackupData = app.BackupData
			database.DB.Save(&existing)
		} else {
			database.DB.Create(&model.Application{
				PackageName: app.PackageName,
				Name:        app.Name,
				IconPath:    iconPath,
				ToClean:     app.ToClean,
				BackupData:  app.BackupData,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "保存成功"})
}

// saveIconToFile 将 base64 图标保存到 wwwroot/images/appicon，返回相对路径
func saveIconToFile(packageName, iconBase64 string) string {
	if iconBase64 == "" {
		return ""
	}
	// 支持 data:image/jpeg;base64,xxx 格式
	idx := strings.Index(iconBase64, ",")
	if idx >= 0 {
		iconBase64 = iconBase64[idx+1:]
	}
	data, err := base64.StdEncoding.DecodeString(iconBase64)
	if err != nil || len(data) == 0 {
		return ""
	}

	dir := filepath.Join(wwwrootDir, appIconDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ""
	}
	filename := strings.ReplaceAll(packageName, ".", "_") + ".jpg"
	fullPath := filepath.Join(dir, filename)
	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		return ""
	}
	return filepath.ToSlash(filepath.Join(appIconDir, filename))
}
