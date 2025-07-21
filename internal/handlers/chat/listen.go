package handlers

import (
	"context"
	"fmt"
	usecase "kick-chat/internal/usecases/chat"
)

type ListenRequest struct {
	UserName string `params:"username" binding:"required"`
}

type ListenResponse struct {
	Message string `json:"message"`
}
type ListenHandler struct {
	usecase usecase.ListenUseCase
}

func NewListenHandler(usecase usecase.ListenUseCase) *ListenHandler {
	return &ListenHandler{
		usecase: usecase,
	}
}

func (h *ListenHandler) Handle(ctx context.Context, req *ListenRequest) (*ListenResponse, error) {
	fmt.Println("listen user:", req.UserName)
	message, err := h.usecase.Execute(ctx, req.UserName)
	if err != nil {
		return nil, err
	}

	return &ListenResponse{Message: message}, nil
}
