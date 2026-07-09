package config_test

import (
	"os"
	"path/filepath"
	"sso/internal/config"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_Success(t *testing.T) {

	cfg, err := config.Load("testdata/config.yaml")
	require.NoError(t, err)

	assert.Equal(t, "local", cfg.Env)
	assert.Equal(t, "postgres", cfg.Storage.Driver)
	assert.Equal(t, "./storage.db", cfg.Storage.DSN)
	assert.Equal(t, 44044, cfg.GRPC.Port)
	assert.Equal(t, time.Hour, cfg.GRPC.Timeout)
	assert.Equal(t, time.Hour, cfg.Token.TTL)
	assert.Equal(t, "./migrations", cfg.Migrations.Path)
}

func TestLoad_PathEmpty(t *testing.T) {
	_, err := config.Load("")
	require.Error(t, err)
	assert.EqualError(t, err, "config path is empty")
}

func TestLoad_FileNotExists(t *testing.T) {
	_, err := config.Load("testdata/not_exists.yaml")
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestLoad_InvalidConfig(t *testing.T) {
	_, err := config.Load("testdata/invalid.yaml")
	require.Error(t, err)
}

func TestLoad_MissingRequiredField(t *testing.T) {
	tests := []struct {
		name        string
		configData  string
		expectedErr string
	}{
		{
			name: "missing driver",
			configData: `
env: "local"

storage:
  dsn: "postgres://localhost:5432"

grpc:
  port: 44044
  timeout: 1h

token:
  ttl: 1h

migrations:
  path: "./migrations"
`,
			expectedErr: "storage.driver is required",
		},
		{
			name: "missing dsn",
			configData: `
env: "local"

storage:
  driver: "postgres"

grpc:
  port: 44044
  timeout: 1h

token:
  ttl: 1h

migrations:
  path: "./migrations"
`,
			expectedErr: "storage.dsn is required",
		},
		{
			name: "missing migrations path",
			configData: `
env: "local"

storage:
  driver: "postgres"
  dsn: "postgres://localhost:5432"

grpc:
  port: 44044
  timeout: 1h

token:
  ttl: 1h

migrations:
`,
			expectedErr: "migrations.path is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := createTempConfig(t, tt.configData)
			cfg, err := config.Load(path)

			require.Error(t, err)
			assert.Nil(t, cfg)
		})
	}
}

func createTempConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")

	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)

	return path
}
