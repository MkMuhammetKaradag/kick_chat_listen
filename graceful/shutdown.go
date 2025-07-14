package graceful

import (
	"context"
	"fmt"

	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

func WaitForShutdown(app *fiber.App, timeout time.Duration, ctx context.Context) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	fmt.Println("Shutting down server...")

	// Fiber app'i kapat
	if err := app.ShutdownWithTimeout(timeout); err != nil {
		zap.L().Error("Shutdown failed", zap.Error(err))
		os.Exit(1)
	}

	fmt.Println("Server gracefully stopped")
}
