package handler

import (
	"encoding/base64"
	"fmt"
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
	wwwrootDir = "/root/scorpio/antares_assets"
	appIconDir = "images/appicon"
)

// ApplicationItem 应用项
type ApplicationItem struct {
	PackageName string `json:"packageName" binding:"required"`
	Name        string `json:"name"`
	IconBase64  string `json:"iconBase64"`
	Whitelist   bool   `json:"whitelist"`
	BackupData  bool   `json:"backup_data"`
}

// SaveApplicationsReq 保存应用请求
type SaveApplicationsReq struct {
	Apps []ApplicationItem `json:"apps" binding:"required"`
}

// ListApplications 获取应用列表（管理端合并已保存的配置用）
func ListApplications(c *gin.Context) {
	_, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var list []model.Application
	if err := database.DB.Order("id ASC").Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// SaveApplications 保存应用配置
func SaveApplications(c *gin.Context) {

	_, exists := c.Get(middleware.UserIDKey)
	if !exists {
		fmt.Println("not login")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var req SaveApplicationsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Println("args invalid")
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	fmt.Println("5555555")
	for _, app := range req.Apps {
		iconPath := saveIconToFile(app.PackageName, app.IconBase64)

		var existing model.Application
		err := database.DB.Where("package_name = ?", app.PackageName).First(&existing).Error
		if err == nil {
			fmt.Println("create", app.PackageName)
			existing.Name = app.Name
			existing.IconPath = iconPath
			existing.Whitelist = app.Whitelist
			existing.BackupData = app.BackupData

			database.DB.Save(&existing)
		} else {
			fmt.Println("create", app.PackageName)
			database.DB.Create(&model.Application{
				PackageName: app.PackageName,
				Name:        app.Name,
				IconPath:    iconPath,
				Whitelist:   app.Whitelist,
				BackupData:  app.BackupData,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "保存成功"})
}

// saveIconToFile 将 base64 图标保存到 antares_assets/images/appicon，返回相对路径
// 根据 data URL 的 MIME 保存为 .png 或 .jpg，PNG 可保留透明通道，避免 JPG 导致黑边
func saveIconToFile(packageName, iconBase64 string) string {
	if iconBase64 == "" {
		return ""
	}
	ext := ".jpg"
	if idx := strings.Index(iconBase64, ","); idx >= 0 {
		mime := strings.ToLower(strings.TrimSpace(iconBase64[:idx]))
		if strings.Contains(mime, "image/png") {
			ext = ".png"
		}
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
	filename := strings.ReplaceAll(packageName, ".", "_") + ext
	fullPath := filepath.Join(dir, filename)
	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		return ""
	}
	return filepath.ToSlash(filepath.Join(appIconDir, filename))
}
