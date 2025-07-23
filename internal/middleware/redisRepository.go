package middleware

import (
	"context"
	"kick-chat/domain"
)

type SessionManagerType interface {
	GetSession(ctx context.Context, key string) (*domain.Session, error)
	IsValid(ctx context.Context, token string) bool
}
