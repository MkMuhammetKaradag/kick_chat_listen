package usecase

import (
	"context"
	"kick-chat/domain"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type SessionManager interface {
	CreateSession(ctx context.Context, userID, token string, userData map[string]string, duration time.Duration) error
	DeleteSession(ctx context.Context, token string) error
	DeleteAllUserSessions(ctx context.Context, userID string) error
	GetSession(ctx context.Context, token string) (*domain.Session, error)
}
type SignInUseCase interface {
	Execute(fbrCtx *fiber.Ctx, ctx context.Context, identifier, password string) (*domain.User, error)
}
type signInUseCase struct {
	postgresRepository PostgresRepository
	sesionManager      SessionManager
}

func NewSignInUseCase(repository PostgresRepository, sesionManager SessionManager) SignInUseCase {
	return &signInUseCase{
		postgresRepository: repository,
		sesionManager:      sesionManager,
	}
}

func (u *signInUseCase) Execute(fbrCtx *fiber.Ctx, ctx context.Context, identifier, password string) (*domain.User, error) {
	user, err := u.postgresRepository.SignIn(ctx, identifier, password)
	if err != nil {
		return nil, err
	}
	sessionKey := user.ID
	sessionToken := uuid.New().String()
	device := fbrCtx.Get("User-Agent")
	ip := fbrCtx.IP()

	userData := map[string]string{
		"id":     user.ID,
		"device": device,
		"ip":     ip,
	}
	if err := u.sesionManager.CreateSession(ctx, sessionKey, sessionToken, userData, 24*time.Hour); err != nil {
		return nil, err
	}
	fbrCtx.Cookie(&fiber.Cookie{
		Name:     "session_token",
		Value:    sessionToken,
		Path:     "/",
		MaxAge:   60 * 60 * 24,
		HTTPOnly: true,
		Secure:   false,
		SameSite: "Lax",
	})

	return user, nil
}
