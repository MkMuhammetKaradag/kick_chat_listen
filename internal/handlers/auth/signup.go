package handlers

import (
	"context"
	"kick-chat/domain"
	usecase "kick-chat/internal/usecases/auth"
)

type SignUpRequest struct {
	Username string `json:"username" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

type SignUpResponse struct {
	Message string `json:"message"`
}
type SignUpHandler struct {
	usecase usecase.SignUpUseCase
}

func NewSignUpHandler(usecase usecase.SignUpUseCase) *SignUpHandler {
	return &SignUpHandler{
		usecase: usecase,
	}
}

func (h *SignUpHandler) Handle(ctx context.Context, req *SignUpRequest) (*SignUpResponse, error) {
	err := h.usecase.Execute(ctx, &domain.User{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		return nil, err
	}

	return &SignUpResponse{Message: "User singup "}, nil
}
