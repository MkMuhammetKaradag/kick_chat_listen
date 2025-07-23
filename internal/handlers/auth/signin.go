package handlers

import (
	"context"
	"kick-chat/domain"
	usecase "kick-chat/internal/usecases/auth"

	"github.com/gofiber/fiber/v2"
)

type SignInRequest struct {
	Identifier string `json:"identifier" binding:"required"`
	Password   string `json:"password" binding:"required,min=8"`
}

type SignInResponse struct {
	User *domain.User `json:"user"`
}
type SignInHandler struct {
	usecase usecase.SignInUseCase
}

func NewSignInHandler(usecase usecase.SignInUseCase) *SignInHandler {
	return &SignInHandler{
		usecase: usecase,
	}
}

func (h *SignInHandler) Handle(fbrCtx *fiber.Ctx, ctx context.Context, req *SignInRequest) (*SignInResponse, error) {
	user, err := h.usecase.Execute(fbrCtx, ctx, req.Identifier, req.Password)
	if err != nil {
		return nil, err
	}

	return &SignInResponse{User: user}, nil
}
