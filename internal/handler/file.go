package handler

import (
	"fmt"
	"gobackend/config"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func UploadFile(c *gin.Context) {
	// 1. 获取上传文件
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": "获取文件失败：" + err.Error()})
		return
	}

	// 2. 获取文件后缀
	ext := filepath.Ext(file.Filename)

	// 3. 生成安全唯一的文件名（原文件名 + 时间戳，避免覆盖）
	// 这里去掉原后缀，避免重复
	fileOnlyName := strings.TrimSuffix(file.Filename, ext)
	newFileName := fmt.Sprintf("%s_%s%s",
		fileOnlyName,
		time.Now().Format("20060102150105"),
		ext,
	)

	// 4. 拼接安全路径（自动适配 Windows / Linux）
	baseDir := config.Cfg.SOLUTION_DIR
	saveDir := filepath.Join(baseDir, "antares_assets", "files")
	savePath := filepath.Join(saveDir, newFileName)

	// 5. 自动创建目录（关键！否则文件夹不存在会报错）
	err = os.MkdirAll(saveDir, 0755)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": "创建目录失败：" + err.Error()})
		return
	}

	// 6. 保存文件
	if err := c.SaveUploadedFile(file, savePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": "保存文件失败：" + err.Error()})
		return
	}

	// 7. 返回成功（可以返回文件路径/文件名，方便前端使用）
	c.JSON(http.StatusOK, gin.H{
		"data": "ok",
		"path": savePath,
		"name": newFileName,
	})
}
