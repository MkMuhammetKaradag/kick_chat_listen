package bootstrap

import (
	"context"
	"kick-chat/graceful"
	"kick-chat/internal/config"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
)

type App struct {
	config         *config.Config
	fiberApp       *fiber.App
	httpHandlers   *Handlers
	postgresRepo   PostgresRepository
	sessionManager SessionManager
}

func NewApp(config *config.Config) (*App, error) {
	app := &App{
		config: config,
	}
	// Bağımlılıkları başlat
	app.initDependencies()

	return app, nil
}

func (a *App) initDependencies() {
	a.sessionManager = InitSessionRedis(a.config)
	//database bağlantisi oluştur
	a.postgresRepo = InitDatabase(a.config)

	// HTTP handler'larını hazırla
	a.httpHandlers = SetupHTTPHandlers(a.postgresRepo, a.sessionManager)

	// HTTP sunucusu kurulumu
	a.fiberApp = SetupServer(a.config, a.httpHandlers)

}

// func (a *App) Start() {

// 	go func() {
// 		port := a.config.Server.Port
// 		if err := a.fiberApp.Listen(":" + port); err != nil {
// 			fmt.Errorf("Failed to start server", err)
// 		}
// 	}()

// 	fmt.Println("Server started on port:", a.config.Server.Port)

//		graceful.WaitForShutdown(a.fiberApp, 5*time.Second, context.Background())
//	}
func (a *App) Start() error {
	errChan := make(chan error, 1)
	go func() {
		if err := a.fiberApp.Listen(":" + a.config.Server.Port); err != nil {
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		return err
	case <-time.After(100 * time.Millisecond): // Sunucunun bind etmesi için kısa bekleme
		log.Println("Server started on port:", a.config.Server.Port)
		graceful.WaitForShutdown(a.fiberApp, 5*time.Second, context.Background())
		return nil
	}
}
