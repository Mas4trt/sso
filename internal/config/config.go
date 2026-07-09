package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Env        string           `yaml:"env" env:"ENV" env-default:"local"`
	Storage    StorageConfig    `yaml:"storage"`
	GRPC       GRPSConfig       `yaml:"grpc"`
	Token      TokenConfig      `yaml:"token"`
	Migrations MigrationsConfig `yaml:"migrations"`
}

type StorageConfig struct {
	Driver string `yaml:"driver" env:"STORAGE_DRIVER" env-required:"true"`
	DSN    string `yaml:"dsn" env:"STORAGE_DSN" env-required:"true"`
}

type GRPSConfig struct {
	Port    int           `yaml:"port" env:"GRPC_PORT" env-default:"44044"`
	Timeout time.Duration `yaml:"timeout" env:"GRPC_TIMEOUT" env-default:"5s"`
}

type TokenConfig struct {
	TTL time.Duration `yaml:"ttl" env:"TOKEN_TTL" env-default:"1h"`
}

type MigrationsConfig struct {
	Path string `yaml:"path" env:"MIGRATIONS_PATH" env-required:"true"`
}

func MustLoad() *Config {
	cfg, err := Load(fetchConfigPath())
	if err != nil {
		panic(err)
	}

	return cfg
}

func Load(path string) (*Config, error) {
	if path == "" {
		return nil, errors.New("config path is empty")
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("stat config file %q: %w", path, err)
	}

	var cfg Config

	if err := cleanenv.ReadConfig(path, &cfg); err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	return &cfg, nil
}

func fetchConfigPath() string {
	var res string

	flag.StringVar(&res, "config", "", "path to configuration file")
	flag.Parse()

	// Если флаг пустой, смотрим в окружение
	if res == "" {
		res = os.Getenv("CONFIG_PATH")
	}
	return res
}
