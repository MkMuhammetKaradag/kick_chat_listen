package usecase

import (
	"context"
	"fmt"
)

type HelloUseCase interface {
	Execute(ctx context.Context, name string) (string, error)
}

type helloUseCase struct {
	text string
}

func NewhelloUseCase(text string) HelloUseCase {
	return &helloUseCase{
		text: text,
	}
}

func (u *helloUseCase) Execute(ctx context.Context, name string) (string, error) {
	message := fmt.Sprintf("%s: %s", u.text, name)
	fmt.Println(message)

	return message, nil
}
