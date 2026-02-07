package main

import (
	"log"
	"os"

	"gobackend/config"
	"gobackend/internal/database"
	"gobackend/internal/handler"
	"gobackend/internal/middleware"
	"gobackend/internal/model"

	"github.com/gin-gonic/gin"
)

func main() {
	// 根据 APP_ENV 加载 dev.env 或 product.env
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "dev"
	}

	if err := config.Load(env); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	if err := database.InitMySQL(); err != nil {
		log.Fatalf("MySQL 连接失败: %v", err)
	}
	log.Println("MySQL 连接成功")

	// 自动迁移
	if err := database.DB.AutoMigrate(&model.ScriptCategory{}, &model.Script{}, &model.User{}, &model.Device{}, &model.Task{}, &model.Application{}); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}

	if err := database.InitRedis(); err != nil {
		log.Fatalf("Redis 连接失败: %v", err)
	}
	log.Println("Redis 连接成功")

	gin.SetMode(config.Cfg.Server.Mode)
	r := gin.Default()

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	api := r.Group("/api")
	{
		api.GET("/scripts_tree", handler.GetScriptsTree)
		api.POST("/user/login", handler.Login)
		api.GET("/user/profile", middleware.Auth, handler.GetUserProfile)
		api.POST("/applications", middleware.Auth, handler.SaveApplications)
	}

	addr := ":" + config.Cfg.Server.Port
	log.Printf("服务启动: http://localhost%s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
