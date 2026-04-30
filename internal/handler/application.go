package handler

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gobackend/config"
	"gobackend/internal/database"
	"gobackend/internal/middleware"
	"gobackend/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	appIconDir = "images/appicon"
)

var wwwrootDir string

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

// EssentialAppLite 必备应用列表项（仅对外暴露包名与下载地址）
type EssentialAppLite struct {
	PackageName string `json:"package_name"`
	DownloadUrl string `json:"download_url"`
	ApkVersion  string `json:"apk_version"`
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
	// 延迟初始化 wwwrootDir，避免在 config.Cfg 还未加载时发生空指针
	if wwwrootDir == "" {
		baseDir := ""
		if config.Cfg != nil {
			baseDir = config.Cfg.SOLUTION_DIR
		}
		if baseDir == "" {
			baseDir = "."
		}
		wwwrootDir = filepath.Join(baseDir, "antares_assets")
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

// UpdateAppVersion 更新应用版本
func UpdateAppVersion(c *gin.Context) {
	version := strings.TrimSpace(c.Query("version"))
	if version == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "version不能为空"})
		return
	}

	var cfg model.Config
	err := database.DB.Where("config_key = ?", model.AppVersionConfigKey).First(&cfg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			cfg = model.Config{Key: model.AppVersionConfigKey, Value: version}
			if createErr := database.DB.Create(&cfg).Error; createErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "更新失败"})
				return
			}
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "更新失败"})
			return
		}
	} else {
		cfg.Value = version
		if saveErr := database.DB.Save(&cfg).Error; saveErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "更新失败"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功", "version": version})
}

// GetAppVersion 获取应用版本
func GetAppVersion(c *gin.Context) {
	var cfg model.Config
	err := database.DB.Where("config_key = ?", model.AppVersionConfigKey).First(&cfg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "获取成功", "version": ""})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "获取失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "获取成功", "version": cfg.Value})
}
func GetEssentialApps(c *gin.Context) {
	var apps []model.Application
	err := database.DB.Select("package_name", "download_url").Where("is_essential = 1").Find(&apps).Error
	if err != nil {
		fmt.Println("获取必备应用列表失败", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取失败"})
		return
	}
	out := make([]EssentialAppLite, 0, len(apps))
	for _, a := range apps {
		out = append(out, EssentialAppLite{
			PackageName: a.PackageName,
			DownloadUrl: a.DownloadUrl,
			ApkVersion:  a.ApkVersion,
		})
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "获取成功", "data": out})
}
func InstallRandomApp(c *gin.Context) {
	var app model.RandomLitteApk
	err := database.DB.Order("RAND()").Limit(1).First(&app).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "暂无数据", "data": gin.H{}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "获取成功",
		"data": gin.H{
			"package_name": app.PackageName,
			"download_url": app.DownloadURL,
		},
	})
}
