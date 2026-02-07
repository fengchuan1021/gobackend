package database

import (
	"context"
	"gobackend/config"

	"github.com/redis/go-redis/v9"
)

var RDB *redis.Client

func InitRedis() error {
	RDB = redis.NewClient(&redis.Options{
		Addr:     config.Cfg.Redis.Addr,
		Password: config.Cfg.Redis.Password,
		DB:       config.Cfg.Redis.DB,
	})

	ctx := context.Background()
	return RDB.Ping(ctx).Err()
}
