package postgres_test

import (
	"context"
	"testing"
	"time"

	"sso/internal/storage"
	"sso/internal/storage/postgres"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupTestDB(t *testing.T) (*postgres.Storage, string) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("sso_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, pgContainer.Terminate(ctx))
	})

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	require.NoError(t, runMigrations(dsn))

	store, err := postgres.New(ctx, dsn)
	require.NoError(t, err)

	return store, dsn
}

func TestStorage_SaveAndGetUser(t *testing.T) {
	store, _ := setupTestDB(t)
	ctx := context.Background()

	id, err := store.SaveUser(ctx, "test@example.com", []byte("hash"))
	require.NoError(t, err)
	require.NotZero(t, id)

	user, err := store.User(ctx, "test@example.com")
	require.NoError(t, err)
	require.Equal(t, "test@example.com", user.Email)
}

func TestStorage_SaveUser_Duplicate(t *testing.T) {
	store, _ := setupTestDB(t)
	ctx := context.Background()

	_, err := store.SaveUser(ctx, "dup@example.com", []byte("hash"))
	require.NoError(t, err)

	_, err = store.SaveUser(ctx, "dup@example.com", []byte("hash"))
	require.ErrorIs(t, err, storage.ErrUserExists)
}
func TestStorage_User_NotFound(t *testing.T) {
	store, _ := setupTestDB(t)

	_, err := store.User(context.Background(), "ghost@example.com")
	require.ErrorIs(t, err, storage.ErrUserNotFound)
}

func TestStorage_RefreshTokenLifecycle(t *testing.T) {
	store, dsn := setupTestDB(t)
	ctx := context.Background()

	uid, err := store.SaveUser(ctx, "rt@example.com", []byte("hash"))
	require.NoError(t, err)

	appID, err := seedApp(ctx, dsn, "isolated-test-app", "secret")
	require.NoError(t, err)

	tokenHash := []byte("fake-hash-32-bytes-aaaaaaaaaaaaa")
	expiresAt := time.Now().Add(time.Hour)

	err = store.SaveRefreshToken(ctx, tokenHash, uid, appID, expiresAt)
	require.NoError(t, err)

	rt, err := store.RefreshToken(ctx, tokenHash)
	require.NoError(t, err)
	require.Equal(t, uid, rt.UserID)
	require.Nil(t, rt.RevokedAt)

	err = store.RevokeRefreshToken(ctx, tokenHash)
	require.NoError(t, err)

	rt, err = store.RefreshToken(ctx, tokenHash)
	require.NoError(t, err)
	require.NotNil(t, rt.RevokedAt)
}

func TestStorage_RevokeRefreshToken_AlreadyRevoked(t *testing.T) {
	store, dsn := setupTestDB(t)
	ctx := context.Background()

	uid, err := store.SaveUser(ctx, "replay@example.com", []byte("hash"))
	require.NoError(t, err)

	appID, err := seedApp(ctx, dsn, "replay-test-app", "secret")
	require.NoError(t, err)

	tokenHash := []byte("replay-hash-32-bytes-aaaaaaaaaaa")
	require.NoError(t, store.SaveRefreshToken(ctx, tokenHash, uid, appID, time.Now().Add(time.Hour)))

	require.NoError(t, store.RevokeRefreshToken(ctx, tokenHash)) // first revoke wins

	err = store.RevokeRefreshToken(ctx, tokenHash) // simulated raced/replayed second call
	require.ErrorIs(t, err, storage.ErrRefreshTokenInvalid)
}
