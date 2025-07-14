package main

import (
	"kick-chat/internal/bootstrap"
	"kick-chat/internal/config"
	"log"

	_ "kick-chat/logging"

	"go.uber.org/zap"
)

func main() {
	cfg, err := config.Read()
	if err != nil {
		log.Fatal("Config error:", err)
	}

	app, err := bootstrap.NewApp(cfg)
	if err != nil {
		log.Fatal("Bootstrap error:", err)
	}

	if err := app.Start(); err != nil {
		log.Fatal("Server error:", zap.Error(err)) // Structured logging
	}
}
