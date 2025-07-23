package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	App          AppConfig          `mapstructure:"app"`
	Server       ServerConfig       `mapstructure:"server"`
	Postgres     PostgresConfig     `mapstructure:"postgres"`
	SessionRedis SessionRedisConfig `mapstructure:"sessionredis"`
}

type AppConfig struct {
	Name    string `mapstructure:"name"`
	Version string `mapstructure:"version"`
}

type ServerConfig struct {
	Port        string `mapstructure:"port"`
	Host        string `mapstructure:"host"`
	Description string `mapstructure:"description"`
}
type PostgresConfig struct {
	Port     string `mapstructure:"port"`
	Host     string `mapstructure:"host"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DB       string `mapstructure:"db"`
}
type SessionRedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     string `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

func Read() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("../")

	viper.AddConfigPath(".")    // Çalışma dizini (Docker'da /app)
	viper.AddConfigPath("/app") // Docker içinde tam path
	viper.AddConfigPath("/")    // Yine de root denensin

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("config read error: %w", err)
	}
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config parse error: %w", err)
	}
	return &cfg, nil

}
