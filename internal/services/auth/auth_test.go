package auth_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"sso/internal/domain/models"
	authsvc "sso/internal/services/auth"
	"sso/internal/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// --- mocks ---

type userSaverMock struct{ mock.Mock }

func (m *userSaverMock) SaveUser(ctx context.Context, email string, passHash []byte) (uint64, error) {
	args := m.Called(ctx, email, passHash)
	return args.Get(0).(uint64), args.Error(1)
}

type userProviderMock struct{ mock.Mock }

func (m *userProviderMock) User(ctx context.Context, email string) (models.User, error) {
	args := m.Called(ctx, email)
	return args.Get(0).(models.User), args.Error(1)
}

func (m *userProviderMock) UserByID(ctx context.Context, userID uint64) (models.User, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).(models.User), args.Error(1)
}

func (m *userProviderMock) IsAdmin(ctx context.Context, userID uint64) (bool, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
}

type appProviderMock struct{ mock.Mock }

func (m *appProviderMock) App(ctx context.Context, appID uint64) (models.App, error) {
	args := m.Called(ctx, appID)
	return args.Get(0).(models.App), args.Error(1)
}

type tokenSaverMock struct{ mock.Mock }

func (m *tokenSaverMock) SaveRefreshToken(ctx context.Context, tokenHash []byte, userID, appID uint64, expiresAt time.Time) error {
	args := m.Called(ctx, tokenHash, userID, appID, expiresAt)
	return args.Error(0)
}

func (m *tokenSaverMock) RefreshToken(ctx context.Context, tokenHash []byte) (models.RefreshToken, error) {
	args := m.Called(ctx, tokenHash)
	return args.Get(0).(models.RefreshToken), args.Error(1)
}

func (m *tokenSaverMock) RevokeRefreshToken(ctx context.Context, tokenHash []byte) error {
	args := m.Called(ctx, tokenHash)
	return args.Error(0)
}

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestAuth собирает Auth с нужным подмножеством моков, остальные — nil,
// если конкретный тест их не использует.
func newTestAuth(
	saver authsvc.UserSaver,
	provider authsvc.UserProvider,
	appProvider authsvc.AppProvider,
	tokenSaver authsvc.TokenSaver,
) *authsvc.Auth {
	return authsvc.New(noopLogger(), saver, provider, appProvider, tokenSaver, time.Hour, 720*time.Hour)
}

// --- tests ---

func TestRegisterNewUser_Success(t *testing.T) {
	saver := new(userSaverMock)
	saver.On("SaveUser", mock.Anything, "test@example.com", mock.Anything).
		Return(uint64(1), nil)

	svc := newTestAuth(saver, nil, nil, nil)

	id, err := svc.RegisterNewUser(context.Background(), "test@example.com", "password123")

	require.NoError(t, err)
	assert.Equal(t, uint64(1), id)
	saver.AssertExpectations(t)
}

func TestRegisterNewUser_AlreadyExists(t *testing.T) {
	saver := new(userSaverMock)
	saver.On("SaveUser", mock.Anything, mock.Anything, mock.Anything).
		Return(uint64(0), storage.ErrUserExists)

	svc := newTestAuth(saver, nil, nil, nil)

	_, err := svc.RegisterNewUser(context.Background(), "test@example.com", "password123")

	require.Error(t, err)
	assert.ErrorIs(t, err, authsvc.ErrUserExists)
}

func TestLogin_Success(t *testing.T) {
	password := "password123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)

	provider := new(userProviderMock)
	provider.On("User", mock.Anything, "test@example.com").
		Return(models.User{ID: 1, Email: "test@example.com", PassHash: hash}, nil)

	appProvider := new(appProviderMock)
	appProvider.On("App", mock.Anything, uint64(1)).
		Return(models.App{ID: 1, Name: "test-app", Secret: "super-secret"}, nil)

	tokenSaver := new(tokenSaverMock)
	tokenSaver.On("SaveRefreshToken", mock.Anything, mock.Anything, uint64(1), uint64(1), mock.Anything).
		Return(nil)

	svc := newTestAuth(nil, provider, appProvider, tokenSaver)

	access, refresh, err := svc.Login(context.Background(), "test@example.com", password, 1)

	require.NoError(t, err)
	assert.NotEmpty(t, access)
	assert.NotEmpty(t, refresh)
	tokenSaver.AssertExpectations(t)
}

func TestLogin_InvalidPassword(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.DefaultCost)
	require.NoError(t, err)

	provider := new(userProviderMock)
	provider.On("User", mock.Anything, "test@example.com").
		Return(models.User{ID: 1, Email: "test@example.com", PassHash: hash}, nil)

	svc := newTestAuth(nil, provider, nil, nil)

	_, _, err = svc.Login(context.Background(), "test@example.com", "wrong-password", 1)

	require.Error(t, err)
	assert.ErrorIs(t, err, authsvc.ErrInvalidCredentials)
}

func TestLogin_UserNotFound(t *testing.T) {
	provider := new(userProviderMock)
	provider.On("User", mock.Anything, "unknown@example.com").
		Return(models.User{}, storage.ErrUserNotFound)

	svc := newTestAuth(nil, provider, nil, nil)

	_, _, err := svc.Login(context.Background(), "unknown@example.com", "any", 1)

	require.Error(t, err)
	assert.ErrorIs(t, err, authsvc.ErrInvalidCredentials) // важно: не палим наличие юзера
}

func TestRefreshTokens_Success(t *testing.T) {
	tokenSaver := new(tokenSaverMock)
	oldHash := []byte("old-hash")

	tokenSaver.On("RefreshToken", mock.Anything, mock.Anything).
		Return(models.RefreshToken{
			UserID:    1,
			AppID:     1,
			ExpiresAt: time.Now().Add(time.Hour),
		}, nil)
	tokenSaver.On("RevokeRefreshToken", mock.Anything, mock.Anything).Return(nil)
	tokenSaver.On("SaveRefreshToken", mock.Anything, mock.Anything, uint64(1), uint64(1), mock.Anything).
		Return(nil)

	provider := new(userProviderMock)
	provider.On("UserByID", mock.Anything, uint64(1)).
		Return(models.User{ID: 1, Email: "test@example.com"}, nil)

	appProvider := new(appProviderMock)
	appProvider.On("App", mock.Anything, uint64(1)).
		Return(models.App{ID: 1, Name: "test-app", Secret: "secret"}, nil)

	svc := newTestAuth(nil, provider, appProvider, tokenSaver)

	access, refresh, err := svc.RefreshTokens(context.Background(), string(oldHash), 1)

	require.NoError(t, err)
	assert.NotEmpty(t, access)
	assert.NotEmpty(t, refresh)
}

func TestRefreshTokens_Revoked(t *testing.T) {
	tokenSaver := new(tokenSaverMock)
	revokedAt := time.Now()

	tokenSaver.On("RefreshToken", mock.Anything, mock.Anything).
		Return(models.RefreshToken{
			UserID:    1,
			AppID:     1,
			ExpiresAt: time.Now().Add(time.Hour),
			RevokedAt: &revokedAt,
		}, nil)

	svc := newTestAuth(nil, nil, nil, tokenSaver)

	_, _, err := svc.RefreshTokens(context.Background(), "some-token", 1)

	require.Error(t, err)
	assert.ErrorIs(t, err, authsvc.ErrRefreshTokenInvalid)
}

func TestRefreshTokens_Expired(t *testing.T) {
	tokenSaver := new(tokenSaverMock)

	tokenSaver.On("RefreshToken", mock.Anything, mock.Anything).
		Return(models.RefreshToken{
			UserID:    1,
			AppID:     1,
			ExpiresAt: time.Now().Add(-time.Hour), // уже истёк
		}, nil)

	svc := newTestAuth(nil, nil, nil, tokenSaver)

	_, _, err := svc.RefreshTokens(context.Background(), "some-token", 1)

	require.Error(t, err)
	assert.ErrorIs(t, err, authsvc.ErrRefreshTokenInvalid)
}

func TestLogout_Success(t *testing.T) {
	tokenSaver := new(tokenSaverMock)
	tokenSaver.On("RevokeRefreshToken", mock.Anything, mock.Anything).Return(nil)

	svc := newTestAuth(nil, nil, nil, tokenSaver)

	err := svc.Logout(context.Background(), "some-refresh-token")

	require.NoError(t, err)
	tokenSaver.AssertExpectations(t)
}

func TestIsAdmin(t *testing.T) {
	tests := []struct {
		name     string
		mockErr  error
		wantErr  error
		expected bool
	}{
		{name: "user is admin", expected: true},
		{name: "user not found", mockErr: storage.ErrUserNotFound, wantErr: storage.ErrUserNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := new(userProviderMock)
			provider.On("IsAdmin", mock.Anything, uint64(1)).
				Return(tt.expected, tt.mockErr)

			svc := newTestAuth(nil, provider, nil, nil)

			got, err := svc.IsAdmin(context.Background(), 1)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantErr))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}
