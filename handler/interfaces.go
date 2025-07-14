package handler

import (
	"context"
)

type Request any
type Response any

type BasicHandler[R Request, Res Response] interface {
	Handle(ctx context.Context, req *R) (*Res, error)
}
