package handlers

import (
	"context"
	usecase "kick-chat/internal/usecases/chat"
)

type HelloRequest struct {
	Name string `params:"name" binding:"required"`
}

type HelloResponse struct {
	Message string `json:"message"`
}
type HelloHandler struct {
	usecase usecase.HelloUseCase
}

func NewHelloHandler(usecase usecase.HelloUseCase) *HelloHandler {
	return &HelloHandler{
		usecase: usecase,
	}
}

func (h *HelloHandler) Handle(ctx context.Context, req *HelloRequest) (*HelloResponse, error) {
	message, err := h.usecase.Execute(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	return &HelloResponse{Message: message}, nil
}
