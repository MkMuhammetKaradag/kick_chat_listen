package initializer

import (
	"fmt"
	"kick-chat/infra/postgres"
	"kick-chat/internal/config"
	"log"
)

func InitDatabase(appConfig *config.Config) *postgres.Repository {
	databaseURL := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", appConfig.Postgres.User, appConfig.Postgres.Password, appConfig.Postgres.Host, appConfig.Postgres.Port, appConfig.Postgres.DB)
	fmt.Println(databaseURL)
	repo, err := postgres.NewRepository(databaseURL)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	return repo
}
