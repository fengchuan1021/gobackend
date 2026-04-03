

package handler

import (
	"net/http"

	"gobackend/internal/database"
	"gobackend/internal/model"

	"github.com/gin-gonic/gin"
)

// ListBlacklist 获取 blacklist 表中所有 package_name 返回给客户端。
// 供客户端根据黑名单卸载对应应用。
func ListBlacklist(c *gin.Context) {
	pkgs, err := model.GetAllBlacklistPackageNames(database.DB)
	if err != nil {
		// 给客户端稳定返回 data 字段，避免客户端解析失败。
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": []string{},
			"msg":  "query blacklist failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": pkgs,
	})
}