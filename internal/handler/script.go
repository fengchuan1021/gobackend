package handler

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gobackend/config"
	"gobackend/internal/database"
	"gobackend/internal/model"

	"github.com/gin-gonic/gin"
)

var goScriptsBaseDir string
var qjscPath string

type goScriptCacheEntry struct {
	Content    []byte
	CompiledAt time.Time
}

var goScriptCache sync.Map

// sizeRe 匹配 const uint32_t qjsc_xxx_size = N;
var sizeRe = regexp.MustCompile(`const uint32_t qjsc_\w+_size\s*=\s*(\d+)\s*;`)
var hexRe = regexp.MustCompile(`0x[0-9a-fA-F]{2}`)

// extractBytecodeFromC 从 qjsc 生成的 .c 源码中解析出 qjsc_* 数组的二进制内容，返回最后一个数组的字节（主脚本 bytecode）
func extractBytecodeFromC(cSource []byte) ([]byte, error) {
	sizes := sizeRe.FindAllSubmatch(cSource, -1)
	if len(sizes) == 0 {
		return nil, fmt.Errorf("no qjsc_*_size found in C source")
	}
	// 找最后一个 size，对应主脚本
	lastSize := sizes[len(sizes)-1]
	n, err := strconv.Atoi(string(lastSize[1]))
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("invalid size in C source")
	}
	// 找所有 0xXX 出现的位置，从最后一个 size 声明之后开始收集，取前 n 个
	idx := bytes.LastIndex(cSource, lastSize[0])
	if idx < 0 {
		return nil, fmt.Errorf("size line not found")
	}
	afterSize := cSource[idx+len(lastSize[0]):]
	hexMatches := hexRe.FindAll(afterSize, -1)
	if len(hexMatches) < n {
		return nil, fmt.Errorf("not enough bytes in C array (need %d, got %d)", n, len(hexMatches))
	}
	out := make([]byte, 0, n)
	for i := 0; i < n && i < len(hexMatches); i++ {
		v, err := strconv.ParseUint(string(hexMatches[i][2:]), 16, 8)
		if err != nil {
			return nil, err
		}
		out = append(out, byte(v))
	}
	return out, nil
}

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
	FilePath    string `json:"file_path"`
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
		FilePath:    req.FilePath,
		PackageName: req.PackageName,
		IconURL:     req.IconURL,
	}
	if err := database.DB.Create(&s).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": s})
}

func GetGoScripts(c *gin.Context) {
	fileName := strings.TrimPrefix(c.Param("file_name"), "/")
	fmt.Println("fileName", fileName)
	if fileName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	// 延迟初始化与配置相关的路径，避免包初始化阶段访问未加载的 config.Cfg
	if goScriptsBaseDir == "" || qjscPath == "" {
		baseDir := ""
		if config.Cfg != nil {
			baseDir = config.Cfg.BASE_DIR
		}
		if baseDir == "" {
			baseDir = "."
		}
		goScriptsBaseDir = filepath.Join(baseDir, "antares_assets")
		qjscPath = filepath.Join(baseDir, "antares", "quickjs", "qjsc")
	}

	baseDir, err := filepath.Abs(goScriptsBaseDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置错误"})
		return
	}
	// 解析路径并限制在 baseDir 下，防止路径穿越
	joined := filepath.Join(baseDir, fileName)
	cleanPath := filepath.Clean(joined)
	fmt.Println("cleanPath", cleanPath)
	if !strings.HasPrefix(cleanPath, baseDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "路径非法"})
		return
	}
	if !strings.HasSuffix(strings.ToLower(cleanPath), ".js") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 .js 文件"})
		return
	}

	info, err := os.Stat(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("file not exist", cleanPath)
			c.JSON(http.StatusNotFound, gin.H{"error": "文件不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取文件失败"})
		return
	}
	if info.IsDir() {
		fmt.Println("file is a directory", cleanPath)
		c.JSON(http.StatusBadRequest, gin.H{"error": "不能是目录"})
		return
	}
	sourceModTime := info.ModTime()

	// 缓存 key 使用相对于 baseDir 的路径
	relPath, _ := filepath.Rel(baseDir, cleanPath)
	if relPath == "" || relPath == "." {
		relPath = fileName
	} else {
		relPath = filepath.ToSlash(relPath)
	}
	fmt.Println("relPath", relPath)
	if v, ok := goScriptCache.Load(relPath); ok {
		entry := v.(*goScriptCacheEntry)
		// 源文件的更新时间小于等于缓存编译时间则直接返回缓存（缓存的是字节码二进制）
		if !sourceModTime.After(entry.CompiledAt) {
			c.Data(http.StatusOK, "application/octet-stream", entry.Content)
			return
		}
	}

	// 输出 .c 路径：同目录下同名 .c（相对 baseDir）
	relOut := filepath.ToSlash(filepath.Join(filepath.Dir(relPath), filepath.Base(cleanPath[:len(cleanPath)-3])+".c"))
	absOut := filepath.Join(baseDir, relOut)
	fmt.Println("relOut", relOut)

	relSrc := filepath.ToSlash(relPath)

	cmd := exec.Command(qjscPath, "-o", relOut, "-m", "-s", "-c", relSrc)
	fmt.Println("cmd", cmd.String())
	cmd.Dir = baseDir
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		fmt.Println("compile failed", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "编译失败", "detail": err.Error()})
		return
	}

	cSource, err := os.ReadFile(absOut)
	fmt.Println("cSource", string(cSource))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取编译结果失败"})
		return
	}

	bytecode, err := extractBytecodeFromC(cSource)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "解析编译结果失败", "detail": err.Error()})
		return
	}

	compiledAt := time.Now()
	goScriptCache.Store(relPath, &goScriptCacheEntry{Content: bytecode, CompiledAt: compiledAt})

	c.Data(http.StatusOK, "application/octet-stream", bytecode)
}

// UpdateScriptReq 更新脚本请求（字段均可选）
type UpdateScriptReq struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	CategoryID  *uint   `json:"category_id"`
	FilePath    *string `json:"file_path"`
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
	if req.FilePath != nil {
		s.FilePath = *req.FilePath
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
