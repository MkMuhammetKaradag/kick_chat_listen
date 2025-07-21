package usecase

import (
	"context"
	"fmt"
)

type HelloUseCase interface {
	Execute(ctx context.Context, name string) (string, error)
}
type HelloPostgresRepository interface {
}

type helloUseCase struct {
	text string
	repo HelloPostgresRepository
}

func NewhelloUseCase(repo HelloPostgresRepository, text string) HelloUseCase {
	return &helloUseCase{
		text: text,
		repo: repo,
	}
}

func (u *helloUseCase) Execute(ctx context.Context, name string) (string, error) {
	message := fmt.Sprintf("%s: %s", u.text, name)
	fmt.Println(message)

	return message, nil
}
