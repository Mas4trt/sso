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

func TestLoad_Success_RefreshTTLDefault(t *testing.T) {
	// testdata/config.yaml намеренно не задаёт refresh_ttl — проверяем,
	// что env-default подхватывается, а не остаётся нулевым значением
	cfg, err := config.Load("testdata/config.yaml")
	require.NoError(t, err)

	assert.Equal(t, 720*time.Hour, cfg.Token.RefreshTTL)
}

func TestLoad_Success_RefreshTTLFromYAML(t *testing.T) {
	content := `
env: "local"

storage:
  driver: "postgres"
  dsn: "postgres://localhost:5432"

grpc:
  port: 44044
  timeout: 1h

token:
  ttl: 1h
  refresh_ttl: 48h

migrations:
  path: "./migrations"
`
	path := createTempConfig(t, content)

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, 48*time.Hour, cfg.Token.RefreshTTL)
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
			expectedErr: `field "Driver" is required`,
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
			expectedErr: `field "DSN" is required`,
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
			expectedErr: `field "Path" is required`,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			path := createTempConfig(t, tt.configData)

			cfg, err := config.Load(path)

			require.Error(t, err)
			assert.Nil(t, cfg)
			assert.ErrorContains(t, err, tt.expectedErr)
		})
	}
}

func TestLoad_EnvVarOverridesYAML(t *testing.T) {
	// В контейнерах/CI секреты приходят через переменные окружения и должны
	// иметь приоритет над значением в yaml
	content := `
env: "local"

storage:
  driver: "postgres"
  dsn: "yaml-dsn-should-be-overridden"

grpc:
  port: 44044
  timeout: 1h

token:
  ttl: 1h

migrations:
  path: "./migrations"
`
	path := createTempConfig(t, content)

	t.Setenv("STORAGE_DSN", "postgres://env-user:env-pass@env-host:5432/db")

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, "postgres://env-user:env-pass@env-host:5432/db", cfg.Storage.DSN)
}

func TestLoad_EnvFileMissing_NotAnError(t *testing.T) {
	origWD, err := os.Getwd()
	require.NoError(t, err)

	dir := t.TempDir()

	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(origWD))
	})

	content := `
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
  path: "./migrations"
`

	path := createTempConfig(t, content)

	_, err = config.Load(path)
	require.NoError(t, err)
}

func TestLoad_EnvFileMalformed_ReturnsError(t *testing.T) {
	origWD, err := os.Getwd()
	require.NoError(t, err)

	dir := t.TempDir()

	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(origWD))
	})

	malformed := "this is not a valid env line without equals sign \x00"
	require.NoError(t,
		os.WriteFile(filepath.Join(dir, ".env"), []byte(malformed), 0o644),
	)

	content := `
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
  path: "./migrations"
`

	path := createTempConfig(t, content)

	_, err = config.Load(path)

	require.Error(t, err)
	assert.ErrorContains(t, err, ".env")
}

func createTempConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")

	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)

	return path
}
