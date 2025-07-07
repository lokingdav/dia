package config

import (
	"os"
	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// New loads configuration from environment variables into any given struct type.
// It uses generics to work with different config structs.
func New[T any]() (*T, error) {
	cfg := new(T)
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadEnv loads content of ENV_FILE (e.g .env.{server}) into environment variables
func LoadEnv() (error) {
	envfile := os.Getenv("ENV_FILE")

	if envfile == "" {
		return godotenv.Load();
	}

	return godotenv.Load(envfile);
}