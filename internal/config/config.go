package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/joho/godotenv"
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
	TTL        time.Duration `yaml:"ttl" env:"TOKEN_TTL" env-default:"1h"`
	RefreshTTL time.Duration `yaml:"refresh_ttl" env:"TOKEN_REFRESH_TTL" env-default:"720h"`
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

	// .env — только для локальной разработки. В контейнерах/CI переменные
	// окружения обычно уже проброшены платформой
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	var cfg Config

	if err := cleanenv.ReadConfig(path, &cfg); err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	var errs []error

	if c.Storage.Driver == "" {
		errs = append(errs, fmt.Errorf("storage.driver must not be empty"))
	}
	if c.Storage.DSN == "" {
		errs = append(errs, fmt.Errorf("storage.dsn must not be empty"))
	}
	if c.GRPC.Port <= 0 {
		errs = append(errs, fmt.Errorf("grpc.port must be greater than 0"))
	}
	if c.GRPC.Timeout <= 0 {
		errs = append(errs, fmt.Errorf("grpc.timeout must be greater than 0"))
	}
	if c.Token.TTL <= 0 {
		errs = append(errs, fmt.Errorf("token.ttl must be greater than 0"))
	}
	if c.Token.RefreshTTL <= 0 {
		errs = append(errs, fmt.Errorf("token.refresh_ttl must be greater than 0"))
	}
	if c.Migrations.Path == "" {
		errs = append(errs, fmt.Errorf("migrations.path must not be empty"))
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid config: %w", errors.Join(errs...))
	}

	return nil
}

func fetchConfigPath() string {
	var res string

	if !flag.Parsed() {
		flag.StringVar(&res, "config", "", "path to configuration file")
		flag.Parse()
	}

	if res == "" {
		res = os.Getenv("CONFIG_PATH")
	}
	return res
}
