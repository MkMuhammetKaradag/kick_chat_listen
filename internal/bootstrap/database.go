package bootstrap

import (
	"kick-chat/internal/config"
	"kick-chat/internal/initializer"
)

type PostgresRepository interface {
}

func InitDatabase(config *config.Config) PostgresRepository {
	return initializer.InitDatabase(config)
}
