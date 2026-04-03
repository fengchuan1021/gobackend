package main

import (
	"log"
	"os"

	"gobackend/config"
	"gobackend/internal/database"
	"gobackend/internal/handler"
	"gobackend/internal/middleware"
	"gobackend/internal/model"
	"gobackend/internal/model/third"
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
	if err := database.DB.AutoMigrate(
		&model.ScriptCategory{},
		&model.Script{},
		&model.User{},
		&model.Device{},
		&model.Task{},
		&model.Application{},
		&model.Config{},
		&model.Log{},
		&model.TrickStoreConfig{},
		&model.UserActivateLog{},
		&third.QuNaTask{},
		&third.QuNaTaskSummary{},
		&model.Backup{},
		&model.Blacklist{},
	); err != nil {
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
	api := r.Group("/api")
	{
		api.POST("/backup/backupApps", middleware.Auth, handler.BackupApps)
		api.POST("/backup/listBackups", middleware.Auth, handler.ListBackups)
		api.POST("/backup/uploadBackup", middleware.Auth, handler.UploadBackup)
		api.POST("/backup/setProcessStatus", middleware.Auth, middleware.AesRequest, middleware.AesResponse, handler.SetProcessStatus)

		// 该接口由客户端通过 post2serverraw 拉取（不做 AES 处理），因此不挂 AES 中间件。
		api.POST("/blacklist/listBlacklist", middleware.Auth, handler.ListBlacklist)

		api.GET("/updateAppVersion", handler.UpdateAppVersion)
		api.GET("/getAppVersion", handler.GetAppVersion)
		api.GET("/go_scripts/*file_name", handler.GetGoScripts)
		api.GET("/ws", websocket.Handle(wsHub))
		api.GET("/scripts_tree", handler.GetScriptsTree)
		api.POST("/file/uploadFile", handler.UploadFile)
		api.GET("/scripts", middleware.Auth, handler.ListScripts)
		api.GET("/scripts/:id", middleware.Auth, handler.GetScript)
		api.POST("/scripts", middleware.Auth, handler.CreateScript)
		api.PATCH("/scripts/:id", middleware.Auth, handler.UpdateScript)
		api.PATCH("/scripts/:id/category", middleware.Auth, handler.UpdateScriptCategoryOnly)
		api.POST("/scripts/AddScriptToCategory", middleware.Auth, handler.AddScriptToCategory)
		api.DELETE("/scripts/:id", middleware.Auth, handler.DeleteScript)
		api.GET("/script_categories", middleware.Auth, handler.ListScriptCategories)

		api.POST("/script_categories", middleware.Auth, handler.CreateScriptCategory)
		api.PATCH("/script_categories/:id", middleware.Auth, handler.UpdateScriptCategory)
		api.DELETE("/script_categories/:id", middleware.Auth, handler.DeleteScriptCategory)
		api.POST("/user/login", handler.Login)
		api.POST("/user/register", handler.Register)
		api.POST("/devices/register", middleware.Auth, middleware.AesRequest, middleware.AesResponse, handler.RegisterDevice)
		api.POST("/devices/:serial/appendLog", handler.AppendLog)
		api.POST("/devices/getinitshellscripts", middleware.Auth, middleware.AesRequest, middleware.AesResponse, handler.GetInitShellScripts)
		api.POST("/device/gettrickeystoreconfig", middleware.Auth, middleware.AesRequest, middleware.AesResponse, handler.GetTrickStoreConfig)
		api.POST("/device/getwhitelistapps", middleware.Auth, middleware.AesRequest, middleware.AesResponse, handler.GetWhitelistApps)
		api.GET("/devices/expireTime", middleware.Auth, handler.GetDeviceExpireTime)
		api.GET("/user/profile", middleware.Auth, handler.GetUserProfile)
		api.POST("/user", middleware.Auth, handler.CreateUser)
		api.POST("/user/activate", middleware.Auth, handler.ActivateUser)
		api.GET("/applications", middleware.Auth, handler.ListApplications)
		api.POST("/applications", middleware.Auth, handler.SaveApplications)
		api.GET("/devices", middleware.Auth, handler.SearchDevices)
		api.PATCH("/devices/add_device_expire_time/:id", middleware.Auth, handler.UpdateDevice)
		api.POST("/task/getTaskDetail", middleware.Auth, middleware.AesRequest, middleware.AesResponse, handler.GetTaskDetail)
		api.POST("/task/clientAddTask", middleware.Auth, handler.ClientAddTask)
		api.POST("/task/clientStopTask", middleware.Auth, handler.ClientStopTask)
		api.POST("/task/clientFinishTask", middleware.Auth, middleware.AesRequest, middleware.AesResponse, handler.ClientFinishTask)
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
		api.POST("/third/getQuNaTask", handler.GetQuNaTask)
		api.POST("/third/updateQuNaTaskResult", handler.UpdateQuNaTaskResult)
		api.POST("/third/uploadQuNaTask", middleware.Auth, handler.UploadQuNaTask)
		api.POST("/third/getQuNaTaskSummaryList", middleware.Auth, handler.GetQuNaTaskSummaryList)

	}

	r.Static("/images", config.Cfg.SOLUTION_DIR+"/antares_assets/images")
	r.Static("/files", config.Cfg.SOLUTION_DIR+"/antares_assets/files")

	go udpserver.Run(config.Cfg.Server.UDPPort)

	addr := ":" + config.Cfg.Server.Port

	if err := r.Run(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
