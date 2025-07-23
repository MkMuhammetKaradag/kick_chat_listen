package bootstrap

import (
	"context"
	"kick-chat/domain"
	"kick-chat/internal/config"
	"kick-chat/internal/initializer"
	"time"

	"github.com/redis/go-redis/v9"
)

type SessionManager interface {
	GetRedisClient() *redis.Client
	CreateSession(ctx context.Context, userID, token string, userData map[string]string, duration time.Duration) error
	DeleteSession(ctx context.Context, token string) error
	DeleteAllUserSessions(ctx context.Context, userID string) error
	GetSession(ctx context.Context, token string) (*domain.Session, error)
	IsValid(ctx context.Context, token string) bool
}

func InitSessionRedis(config *config.Config) SessionManager {
	return initializer.InitSessionRedis(config)
}
