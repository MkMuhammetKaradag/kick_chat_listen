package bootstrap

import (
	"kick-chat/handler"
	"kick-chat/internal/config"
	authHandlers "kick-chat/internal/handlers/auth"
	chatHandlers "kick-chat/internal/handlers/chat"
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
	signupHandler := httpHandlers.Signup
	signinHandler := httpHandlers.Signin

	app.Get("/hello/:name", handler.HandleBasic[chatHandlers.HelloRequest, chatHandlers.HelloResponse](helloHandler))
	app.Post("/signup", handler.HandleBasic[authHandlers.SignUpRequest, authHandlers.SignUpResponse](signupHandler))
	app.Post("/signin", handler.HandleWithFiber[authHandlers.SignInRequest, authHandlers.SignInResponse](signinHandler))
	app.Post("/listen/:username", handler.HandleBasic[chatHandlers.ListenRequest, chatHandlers.ListenResponse](listenHandler))

	return app
}
