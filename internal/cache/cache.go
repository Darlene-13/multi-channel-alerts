package cache

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Darlene-13/multi-channel-alerts/internal/env"
)

var RedisClient *redis.Client

func init() {
	addr := env.Get("REDIS_HOST")
	if addr == "" {
		addr = "localhost:6379"
	}

	RedisClient = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: env.Get("REDIS_PASSWORD"),
		DB:       env.GetInt("REDIS_DB"),
		Protocol: env.GetInt("REDIS_PROTOCOL"),
	})
}

func GetRedisClient() *redis.Client {
	return RedisClient
}

func client() (*redis.Client, error) {
	if RedisClient == nil {
		return nil, fmt.Errorf("redis client is not initialized")
	}
	return RedisClient, nil
}

func Remember(key string, value interface{}, expiration int64) error {
	c, err := client()
	if err != nil {
		return err
	}

	duration := time.Duration(expiration) * time.Second
	if err := c.Set(context.Background(), key, value, duration).Err(); err != nil {
		return fmt.Errorf("failed to set key %s: %w", key, err)
	}
	return nil
}

func RememberForever(key string, value interface{}) error {
	return Remember(key, value, 0)
}

func Forget(key string) error {
	c, err := client()
	if err != nil {
		return err
	}

	if err := c.Del(context.Background(), key).Err(); err != nil {
		return fmt.Errorf("failed to delete key %s: %w", key, err)
	}
	return nil
}

func Read(key string) (string, error) {
	c, err := client()
	if err != nil {
		return "", err
	}

	value, err := c.Get(context.Background(), key).Result()
	if err != nil {
		if err == redis.Nil {
			return "", fmt.Errorf("key %s does not exist", key)
		}
		return "", fmt.Errorf("failed to get key %s: %w", key, err)
	}
	return value, nil
}

func ZAdd(key string, member interface{}, score float64) error {
	c, err := client()
	if err != nil {
		return err
	}

	if err := c.ZAdd(context.Background(), key, redis.Z{Score: score, Member: member}).Err(); err != nil {
		return fmt.Errorf("failed to ZADD to %s: %w", key, err)
	}
	return nil
}

func ZRangeByScoreDue(key string, maxScore float64) ([]string, error) {
	c, err := client()
	if err != nil {
		return nil, err
	}

	members, err := c.ZRangeByScore(context.Background(), key, &redis.ZRangeBy{
		Min: "-inf",
		Max: strconv.FormatFloat(maxScore, 'f', -1, 64),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to ZRANGEBYSCORE on %s: %w", key, err)
	}
	return members, nil
}

func ZRem(key string, members ...interface{}) error {
	c, err := client()
	if err != nil {
		return err
	}

	if err := c.ZRem(context.Background(), key, members...).Err(); err != nil {
		return fmt.Errorf("failed to ZREM from %s: %w", key, err)
	}
	return nil
}

func XAdd(key string, value interface{}) error {
	c, err := client()
	if err != nil {
		return err
	}

	err = c.XAdd(context.Background(), &redis.XAddArgs{
		Stream: key,
		Values: map[string]interface{}{"value": value},
		MaxLen: 1000,
	}).Err()
	if err != nil {
		return fmt.Errorf("failed to add to stream %s: %w", key, err)
	}
	return nil
}
