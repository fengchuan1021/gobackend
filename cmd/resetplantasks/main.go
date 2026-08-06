package main

import (
	"log"
	"os"

	"gobackend/config"
	"gobackend/internal/database"
	"gobackend/internal/scheduler"
)

func main() {
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "dev"
	}
	if err := config.Load(env); err != nil {
		log.Fatalf("load config failed: %v", err)
	}
	if err := database.InitMySQL(); err != nil {
		log.Fatalf("mysql init failed: %v", err)
	}
	if err := database.InitRedis(); err != nil {
		log.Fatalf("redis init failed: %v", err)
	}

	if err := scheduler.ResetPlanTaskItemLeftRounds(); err != nil {
		log.Fatalf("reset plan task left rounds failed: %v", err)
	}
	log.Println("reset plan task left rounds ok")
}
