package usecase

import (
	"context"
	"fmt"
	"kick-chat/domain"

	"github.com/google/uuid"
)

type PostgresRepository interface {
	SignUp(ctx context.Context, auth *domain.User) (uuid.UUID, error)
	SignIn(ctx context.Context, identifier, password string) (*domain.User, error)
}
type SignUpUseCase interface {
	Execute(ctx context.Context, user *domain.User) error
}

type signUpUseCase struct {
	postgresRepository PostgresRepository
}

type SignUpRequest struct {
	Username string
	Email    string
	Password string
}

func NewSignUpUseCase(repository PostgresRepository) SignUpUseCase {
	return &signUpUseCase{
		postgresRepository: repository,
	}
}

func (u *signUpUseCase) Execute(ctx context.Context, user *domain.User) error {
	userId, err := u.postgresRepository.SignUp(ctx, user)
	if err != nil {
		return err
	}
	fmt.Println("user signup_id:", userId)
	return nil
}
