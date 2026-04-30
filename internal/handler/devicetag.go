package handler

import (
	"errors"
	"net/http"
	"strings"

	"gobackend/internal/database"
	"gobackend/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type getDeviceTagKeywordsReq struct {
	Serial string `json:"serial" binding:"required"`
}

func GetDeviceTagKeywords(c *gin.Context) {
	var req getDeviceTagKeywordsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "参数错误"})
		return
	}

	var device model.Device
	if err := database.DB.Where("serial = ?", req.Serial).First(&device).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "device不存在"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "查询device失败"})
		return
	}

	// 若设备未分配 tag，则随机挑一个 tag 并写回 device.tag_id
	if device.TagID == 0 {
		var randomTag model.Tag
		if err := database.DB.Order("RAND()").Limit(1).First(&randomTag).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "tag不存在"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "查询tag失败"})
			return
		}
		device.TagID = randomTag.ID
		if err := database.DB.Model(&device).Update("tag_id", randomTag.ID).Error; err != nil {
			c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "更新device tag失败"})
			return
		}
	}

	var tag model.Tag
	if err := database.DB.Where("id = ?", device.TagID).First(&tag).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "tag不存在"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "查询tag失败"})
		return
	}

	keywords := make([]string, 0)
	for _, item := range strings.Split(tag.Keywords, ",") {
		kw := strings.TrimSpace(item)
		if kw != "" {
			keywords = append(keywords, kw)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": keywords,
	})

}
