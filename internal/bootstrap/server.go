package bootstrap

import (
	"kick-chat/handler"
	"kick-chat/internal/config"
	handlers "kick-chat/internal/handlers/chat"
	"kick-chat/server"
	"time"

	"github.com/gofiber/fiber/v2"
)

func SetupServer(config *config.Config, httpHandlers *Handlers) *fiber.App {

	serverConfig := server.Config{
		Port:         config.Server.Port,
		IdleTimeout:  5 * time.Second,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	app := server.NewFiberApp(serverConfig)

	helloHandler := httpHandlers.Hello
	listenHandler := httpHandlers.Listen

	app.Get("/hello/:name", handler.HandleBasic[handlers.HelloRequest, handlers.HelloResponse](helloHandler))
	app.Post("/listen/:username", handler.HandleBasic[handlers.ListenRequest, handlers.ListenResponse](listenHandler))

	return app
}
