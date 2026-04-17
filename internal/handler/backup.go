package handler

import (
	"fmt"
	"gobackend/config"
	"gobackend/internal/database"
	"gobackend/internal/middleware"
	"gobackend/internal/model"
	"gobackend/internal/udpserver"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type BackupAppsReq struct {
	Serial string   `json:"serial"`
	Pkgs   []string `json:"pkgs"`
}

type ListBackupsReq struct {
	Serial string `json:"serial"`
}

func BackupApps(c *gin.Context) {
	var req BackupAppsReq
	c.ShouldBindJSON(&req)
	_, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	userID := c.GetUint(middleware.UserIDKey)
	backup := model.Backup{
		Serial:   req.Serial,
		Pkgs:     strings.Join(req.Pkgs, ","),
		Status:   model.BackupStatusPending,
		Progress: 0,
		UserID:   userID,
	}
	database.DB.Create(&backup)
	go udpserver.SendCommand(req.Serial, udpserver.CmdBackupApps, []byte(strconv.Itoa(int(backup.ID))+","+strings.Join(req.Pkgs, ",")), userID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "ok"})
}

func ListBackups(c *gin.Context) {
	var req ListBackupsReq
	c.ShouldBindJSON(&req)

}

type SetProcessStatusReq struct {
	ID       uint `json:"id"`
	Status   int  `json:"status"`
	Progress int  `json:"progress"`
}

func SetProcessStatus(c *gin.Context) {
	var req SetProcessStatusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}
	if req.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "id不能为空"})
		return
	}
	if req.Progress < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "progress不能为负数"})
		return
	}

	updates := map[string]interface{}{
		"status":   req.Status,
		"progress": req.Progress,
	}
	res := database.DB.Model(&model.Backup{}).
		Where("id = ?", req.ID).
		Updates(updates)
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "更新失败"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "记录不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"})
}

func UploadBackup(c *gin.Context) {
	backupIDStr := c.Query("id")
	if backupIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "id不能为空"})
		return
	}
	backupID, err := strconv.ParseUint(backupIDStr, 10, 64)
	if err != nil || backupID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "id无效"})
		return
	}

	userIDAny, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": -1, "msg": "未登录"})
		return
	}
	uid := userIDAny.(uint)

	filename := c.Query("filename")
	if filename == "" {
		filename = fmt.Sprintf("backup_%d.tgz", backupID)
	}
	// 防止路径穿越：只允许文件名
	filename = filepath.Base(filename)
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "filename无效"})
		return
	}

	backupDir := filepath.Join(config.Cfg.SOLUTION_DIR, "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "创建备份目录失败"})
		return
	}

	outPath := filepath.Join(backupDir, filename)
	tmpPath := outPath + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "创建临时文件失败"})
		return
	}
	written, copyErr := io.Copy(tmpFile, c.Request.Body)
	closeErr := tmpFile.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tmpPath)
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "写入上传内容失败"})
		return
	}
	if written == 0 {
		_ = os.Remove(tmpPath)
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "上传内容为空"})
		return
	}

	_ = os.Remove(outPath)
	if err := os.Rename(tmpPath, outPath); err != nil {
		_ = os.Remove(tmpPath)
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "保存文件失败"})
		return
	}

	// 标记为完成
	update := map[string]interface{}{
		"status":   model.BackupStatusCompleted,
		"progress": 100,
	}
	res := database.DB.Model(&model.Backup{}).
		Where("id = ? and user_id = ?", backupID, uid).
		Updates(update)

	// 如果数据库记录不存在/不属于当前用户，仍返回文件落盘成功信息
	if res.Error != nil || res.RowsAffected == 0 {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "ok",
			"path": outPath,
			// 不强制失败：便于排查“设备传了但 DB 没找到记录”的问题
			"warn": "db update skipped",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"path": outPath,
	})
}
