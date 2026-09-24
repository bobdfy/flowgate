package redisclient

// import (
// 	"context"
// 	"fmt"
// 	"log"

// 	"github.com/redis/go-redis/v9"
// )

// type Client struct {
// 	client *redis.Client
// }

// func NewRedisClient(ctx context.Context, addr, password string) (*Client, error) {
// 	client := redis.NewClient(&redis.Options{
// 		Addr:     addr,
// 		Password: password,
// 		DB:       0,
// 	})
// 	if err := client.Ping(ctx).Err(); err != nil {
// 		client.Close()
// 		return nil, fmt.Errorf("Ping redis error : %w", err)
// 	}

// 	log.Println("redis connected")
// 	return &Client{client: client}, nil
// }
