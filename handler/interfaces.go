package handler

import (
	"context"

	"github.com/gofiber/fiber/v2"
)

type Request any
type Response any

type BasicHandler[R Request, Res Response] interface {
	Handle(ctx context.Context, req *R) (*Res, error)
}

type FiberHandler[R Request, Res Response] interface {
	Handle(fbrCtx *fiber.Ctx, ctx context.Context, req *R) (*Res, error)
}
