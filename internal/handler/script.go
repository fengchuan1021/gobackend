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

// ListScriptCategories 脚本分类列表（管理端）
func ListScriptCategories(c *gin.Context) {
	var list []model.ScriptCategory
	err := database.DB.Order("sort_order ASC").Find(&list).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// CreateScriptCategoryReq 创建/更新分类请求
type CreateScriptCategoryReq struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	IsNew       bool   `json:"is_new"`
	IsHot       bool   `json:"is_hot"`
	SortOrder   int    `json:"sort_order"`
}

// CreateScriptCategory 创建脚本分类
func CreateScriptCategory(c *gin.Context) {
	var req CreateScriptCategoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	cat := model.ScriptCategory{
		Name:        req.Name,
		Description: req.Description,
		IsNew:       req.IsNew,
		IsHot:       req.IsHot,
		SortOrder:   req.SortOrder,
	}
	if err := database.DB.Create(&cat).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": cat})
}

// UpdateScriptCategory 更新脚本分类
func UpdateScriptCategory(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	var req CreateScriptCategoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	var cat model.ScriptCategory
	if err := database.DB.First(&cat, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "分类不存在"})
		return
	}
	cat.Name = req.Name
	cat.Description = req.Description
	cat.IsNew = req.IsNew
	cat.IsHot = req.IsHot
	cat.SortOrder = req.SortOrder
	if err := database.DB.Save(&cat).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": cat})
}

// DeleteScriptCategory 删除脚本分类（软删）
func DeleteScriptCategory(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	result := database.DB.Delete(&model.ScriptCategory{}, id)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "分类不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0})
}
