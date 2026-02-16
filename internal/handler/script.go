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

// GetScript 获取单个脚本（含 content，管理端/加载用）
func GetScript(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	var s model.Script
	if err := database.DB.First(&s, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "脚本不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": s})
}

// ListScripts 脚本列表（管理端，不含 content）
func ListScripts(c *gin.Context) {
	var list []model.Script
	err := database.DB.Select("id", "name", "icon_url", "category_id", "description", "package_name", "created_at", "updated_at").
		Order("id desc").Find(&list).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// UpdateScriptCategoryReq 仅更新脚本分类请求
type UpdateScriptCategoryReq struct {
	CategoryID uint `json:"category_id" binding:"required"`
}

// UpdateScriptCategoryOnly 更新脚本的分类（管理端）
func UpdateScriptCategoryOnly(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	var req UpdateScriptCategoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	result := database.DB.Model(&model.Script{}).Where("id = ?", id).Update("category_id", req.CategoryID)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "脚本不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0})
}

// DeleteScript 删除脚本（软删）
func DeleteScript(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	result := database.DB.Delete(&model.Script{}, id)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "脚本不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0})
}

// CreateScriptReq 创建脚本请求
type CreateScriptReq struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	CategoryID  uint   `json:"category_id" binding:"required"`
	Content     string `json:"content"`
	PackageName string `json:"package_name"`
	IconURL     string `json:"icon_url"`
}

// CreateScript 创建脚本
func CreateScript(c *gin.Context) {
	var req CreateScriptReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	s := model.Script{
		Name:        req.Name,
		Description: req.Description,
		CategoryID:  req.CategoryID,
		Content:     req.Content,
		PackageName: req.PackageName,
		IconURL:     req.IconURL,
	}
	if err := database.DB.Create(&s).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": s})
}

// UpdateScriptReq 更新脚本请求（字段均可选）
type UpdateScriptReq struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	CategoryID  *uint   `json:"category_id"`
	Content     *string `json:"content"`
	PackageName *string `json:"package_name"`
	IconURL     *string `json:"icon_url"`
}

// UpdateScript 更新脚本
func UpdateScript(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	var req UpdateScriptReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	var s model.Script
	if err := database.DB.First(&s, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "脚本不存在"})
		return
	}
	if req.Name != nil {
		s.Name = *req.Name
	}
	if req.Description != nil {
		s.Description = *req.Description
	}
	if req.CategoryID != nil {
		s.CategoryID = *req.CategoryID
	}
	if req.Content != nil {
		s.Content = *req.Content
	}
	if req.PackageName != nil {
		s.PackageName = *req.PackageName
	}
	if req.IconURL != nil {
		s.IconURL = *req.IconURL
	}
	if err := database.DB.Save(&s).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": s})
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
	var count int64
	if err := database.DB.Model(&model.Script{}).Where("category_id = ?", id).Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该分类下还有脚本，请先移动或删除脚本后再删除分类"})
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
