package main

import (
	"log"
	"os"

	"gobackend/config"
	"gobackend/internal/database"
	"gobackend/internal/handler"
	"gobackend/internal/middleware"
	"gobackend/internal/model"
	"gobackend/internal/udpserver"
	"gobackend/internal/websocket"

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
	if err := database.DB.AutoMigrate(&model.ScriptCategory{}, &model.Script{}, &model.User{}, &model.Device{}, &model.Task{}, &model.Application{}, &model.Config{}); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	database.CheckAndAddSuperAdmin()
	if err := database.InitRedis(); err != nil {
		log.Fatalf("Redis 连接失败: %v", err)
	}
	log.Println("Redis 连接成功")

	gin.SetMode(config.Cfg.Server.Mode)
	r := gin.Default()

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// WebSocket
	wsHub := websocket.NewHub()
	websocket.DefaultHub = wsHub
	go wsHub.Run()
	r.GET("/ws", websocket.Handle(wsHub))
	r.GET("/go_scripts/*file_name", handler.GetGoScripts)
	api := r.Group("/api")
	{
		api.GET("/scripts_tree", handler.GetScriptsTree)
		api.GET("/scripts", middleware.Auth, handler.ListScripts)
		api.GET("/scripts/:id", middleware.Auth, handler.GetScript)
		api.POST("/scripts", middleware.Auth, handler.CreateScript)
		api.PATCH("/scripts/:id", middleware.Auth, handler.UpdateScript)
		api.PATCH("/scripts/:id/category", middleware.Auth, handler.UpdateScriptCategoryOnly)
		api.DELETE("/scripts/:id", middleware.Auth, handler.DeleteScript)
		api.GET("/script_categories", middleware.Auth, handler.ListScriptCategories)

		api.POST("/script_categories", middleware.Auth, handler.CreateScriptCategory)
		api.PATCH("/script_categories/:id", middleware.Auth, handler.UpdateScriptCategory)
		api.DELETE("/script_categories/:id", middleware.Auth, handler.DeleteScriptCategory)
		api.POST("/user/login", handler.Login)
		api.POST("/devices/register", middleware.Auth, middleware.AesRequest, middleware.AesResponse, handler.RegisterDevice)
		api.POST("/devices/:serial/appendLog", handler.AppendLog)
		api.POST("/devices/getinitshellscripts", middleware.Auth, middleware.AesRequest, middleware.AesResponse, handler.GetInitShellScripts)
		api.POST("/device/gettrickeystoreconfig", middleware.Auth, middleware.AesRequest, middleware.AesResponse, handler.GetTrickStoreConfig)
		api.POST("/device/getwhitelistapps", middleware.Auth, middleware.AesRequest, middleware.AesResponse, handler.GetWhitelistApps)
		api.GET("/devices/expireTime", middleware.Auth, handler.GetDeviceExpireTime)
		api.GET("/user/profile", middleware.Auth, handler.GetUserProfile)
		api.POST("/user", middleware.Auth, handler.CreateUser)
		api.GET("/applications", middleware.Auth, handler.ListApplications)
		api.POST("/applications", middleware.Auth, handler.SaveApplications)
		api.GET("/devices", middleware.Auth, handler.SearchDevices)
		api.PATCH("/devices/:id", middleware.Auth, handler.UpdateDevice)
		api.POST("/task/getTaskDetail", middleware.Auth, middleware.AesRequest, middleware.AesResponse, handler.GetTaskDetail)
		api.POST("/task/clientAddTask", middleware.Auth, handler.ClientAddTask)
		api.POST("/udp/cmdcallback", handler.CmdCallback)
		// 设备凭 script_id 拉取脚本内容，无鉴权（script_id 不可猜测且 20s 过期）
		api.POST("/dev/getDevScriptContent/:id", handler.GetDevScriptContent)
		dev := api.Group("/dev", middleware.Auth)
		{
			dev.GET("/getDevices", handler.GetDevices)
			dev.GET("/getScreenShot", handler.GetScreenShot)
			dev.GET("/getXmlLayout", handler.GetXmlLayout)
			dev.POST("/runDevScript", handler.RunDevScript)
		}
	}

	r.Static("/images", "/root/scorpio/antares_assets/images")

	go udpserver.Run(config.Cfg.Server.UDPPort)

	addr := ":" + config.Cfg.Server.Port
	log.Printf("服务启动: http://localhost%s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
