package handler

import (
	"net/http"

	"gobackend/internal/database"
	"gobackend/internal/model"

	"github.com/gin-gonic/gin"
)

// ScriptListItem 脚本列表项（不含 content）
type ScriptListItem struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	IconURL     string `json:"icon_url"`
	CategoryID  uint   `json:"category_id"`
	Description string `json:"description"`
}

// CategoryWithScripts 分类及其脚本树节点
type CategoryWithScripts struct {
	model.ScriptCategory
	Scripts []ScriptListItem `json:"scripts"`
}

// GetScriptsTree 返回所有分类及其下的脚本（不含脚本内容）
func GetScriptsTree(c *gin.Context) {
	var categories []model.ScriptCategory
	err := database.DB.Order("sort_order ASC").Find(&categories).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	result := make([]CategoryWithScripts, 0, len(categories))
	for _, cat := range categories {
		var scripts []model.Script
		// 排除 content 字段，不加载到内存
		err := database.DB.Model(&model.Script{}).
			Select("id", "name", "icon_url", "category_id", "description", "created_at", "updated_at").
			Where("category_id = ?", cat.ID).
			Find(&scripts).Error
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询脚本失败"})
			return
		}

		items := make([]ScriptListItem, 0, len(scripts))
		for _, s := range scripts {
			items = append(items, ScriptListItem{
				ID:          s.ID,
				Name:        s.Name,
				IconURL:     s.IconURL,
				CategoryID:  s.CategoryID,
				Description: s.Description,
			})
		}

		result = append(result, CategoryWithScripts{
			ScriptCategory: cat,
			Scripts:        items,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}
