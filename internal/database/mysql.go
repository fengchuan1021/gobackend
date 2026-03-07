package database

import (
	"strings"
	"time"

	"gobackend/config"
	"gobackend/internal/model"
	"log"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var DB *gorm.DB

func CheckAndAddSuperAdmin() {
	var existing model.User
	if err := DB.Where("username = ?", "bigmouth666").First(&existing).Error; err == nil {
		// 若密码为明文则更新为 bcrypt
		if !strings.HasPrefix(existing.Password, "$2a$") && !strings.HasPrefix(existing.Password, "$2b$") {
			hashed, _ := bcrypt.GenerateFromPassword([]byte("bigmouth666"), bcrypt.DefaultCost)
			DB.Model(&existing).Update("password", string(hashed))
		}
		return
	}

	hashed, _ := bcrypt.GenerateFromPassword([]byte("bigmouth666"), bcrypt.DefaultCost)
	user := model.User{
		Username:     "bigmouth666",
		Password:     string(hashed),
		RoleID:       1,
		IsBanned:     false,
		RegisterTime: time.Now(),
	}
	if err := DB.Create(&user).Error; err != nil {
		log.Fatalf("创建超级管理员失败: %v", err)
	}
}
func InitMySQL() error {
	dsn := config.Cfg.MySQL.DSN()
	var err error
	DB, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})

	return err
}
