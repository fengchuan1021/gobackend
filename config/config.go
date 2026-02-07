package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

var Cfg *Config

type Config struct {
	MySQL  MySQLConfig
	Redis  RedisConfig
	Server ServerConfig
}

type MySQLConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type ServerConfig struct {
	Port string
	Mode string
}

func Load(env string) error {
	envFile := "dev.env"
	if env == "product" || env == "prod" {
		envFile = "product.env"
	}

	if err := godotenv.Load(envFile); err != nil {
		return fmt.Errorf("加载 %s 失败: %w", envFile, err)
	}

	redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))
	mysqlPort, _ := strconv.Atoi(getEnv("MYSQL_PORT", "3306"))

	Cfg = &Config{
		MySQL: MySQLConfig{
			Host:     getEnv("MYSQL_HOST", "127.0.0.1"),
			Port:     mysqlPort,
			User:     getEnv("MYSQL_USER", "root"),
			Password: getEnv("MYSQL_PASSWORD", ""),
			Database: getEnv("MYSQL_DATABASE", "gobackend"),
		},
		Redis: RedisConfig{
			Addr:     getEnv("REDIS_ADDR", "127.0.0.1:6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       redisDB,
		},
		Server: ServerConfig{
			Port: getEnv("SERVER_PORT", "8080"),
			Mode: getEnv("GIN_MODE", "debug"),
		},
	}

	return nil
}

func (c *MySQLConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		c.User, c.Password, c.Host, c.Port, c.Database)
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
