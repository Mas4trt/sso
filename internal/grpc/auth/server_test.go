// internal/grpc/auth/server_test.go
package auth_test

import (
	"context"
	"net"
	"testing"

	grpcauth "sso/internal/grpc/auth"
	authsvc "sso/internal/services/auth"
	"sso/internal/storage"

	authv1 "github.com/Mas4trt/protos/gen/go/auth/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// --- mock service ---

type authServiceMock struct{ mock.Mock }

func (m *authServiceMock) RegisterNewUser(ctx context.Context, email, password string) (uint64, error) {
	args := m.Called(ctx, email, password)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *authServiceMock) Login(ctx context.Context, email, password string, appID uint64) (string, string, error) {
	args := m.Called(ctx, email, password, appID)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *authServiceMock) RefreshTokens(ctx context.Context, refreshToken string, appID uint64) (string, string, error) {
	args := m.Called(ctx, refreshToken, appID)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *authServiceMock) Logout(ctx context.Context, refreshToken string) error {
	args := m.Called(ctx, refreshToken)
	return args.Error(0)
}

func (m *authServiceMock) IsAdmin(ctx context.Context, userID uint64) (bool, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
}

// --- test harness ---

// newTestClient поднимает реальный grpc-сервер поверх in-memory bufconn —
// это проверяет не только логику хендлера, но и то, что сообщения
// реально доходят через grpc-транспорт с правильными status codes,
// а не просто вызывают Go-функцию напрямую.
func newTestClient(t *testing.T, svc grpcauth.AuthService) authv1.AuthClient {
	t.Helper()

	const bufSize = 1024 * 1024
	lis := bufconn.Listen(bufSize)

	srv := grpc.NewServer()
	grpcauth.Register(srv, svc)

	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return authv1.NewAuthClient(conn)
}

func statusCode(t *testing.T, err error) codes.Code {
	t.Helper()
	st, ok := status.FromError(err)
	require.True(t, ok, "expected a grpc status error, got: %v", err)
	return st.Code()
}

// --- Register ---

func TestRegister_Success(t *testing.T) {
	svc := new(authServiceMock)
	svc.On("RegisterNewUser", mock.Anything, "test@example.com", "password123").
		Return(uint64(1), nil)

	client := newTestClient(t, svc)

	resp, err := client.Register(context.Background(), &authv1.RegisterRequest{
		Email:    "test@example.com",
		Password: "password123",
	})

	require.NoError(t, err)
	assert.Equal(t, uint64(1), resp.GetUserId())
}

func TestRegister_EmptyEmail(t *testing.T) {
	svc := new(authServiceMock)
	client := newTestClient(t, svc)

	_, err := client.Register(context.Background(), &authv1.RegisterRequest{
		Password: "password123",
	})

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, statusCode(t, err))
	svc.AssertNotCalled(t, "RegisterNewUser")
}

func TestRegister_AlreadyExists(t *testing.T) {
	svc := new(authServiceMock)
	svc.On("RegisterNewUser", mock.Anything, mock.Anything, mock.Anything).
		Return(uint64(0), authsvc.ErrUserExists)

	client := newTestClient(t, svc)

	_, err := client.Register(context.Background(), &authv1.RegisterRequest{
		Email:    "test@example.com",
		Password: "password123",
	})

	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, statusCode(t, err))
}

// --- Authenticate ---

func TestAuthenticate_Success(t *testing.T) {
	svc := new(authServiceMock)
	svc.On("Login", mock.Anything, "test@example.com", "password123", uint64(1)).
		Return("access-token", "refresh-token", nil)

	client := newTestClient(t, svc)

	resp, err := client.Authenticate(context.Background(), &authv1.LoginRequest{
		Email:         "test@example.com",
		Password:      "password123",
		ApplicationId: 1,
	})

	require.NoError(t, err)
	assert.Equal(t, "access-token", resp.GetAccessToken())
	assert.Equal(t, "refresh-token", resp.GetRefreshToken())
}

func TestAuthenticate_InvalidCredentials(t *testing.T) {
	svc := new(authServiceMock)
	svc.On("Login", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("", "", authsvc.ErrInvalidCredentials)

	client := newTestClient(t, svc)

	_, err := client.Authenticate(context.Background(), &authv1.LoginRequest{
		Email:         "test@example.com",
		Password:      "wrong",
		ApplicationId: 1,
	})

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, statusCode(t, err))
}

func TestAuthenticate_AppNotFound(t *testing.T) {
	svc := new(authServiceMock)
	svc.On("Login", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("", "", storage.ErrAppNotFound)

	client := newTestClient(t, svc)

	_, err := client.Authenticate(context.Background(), &authv1.LoginRequest{
		Email:         "test@example.com",
		Password:      "password123",
		ApplicationId: 999,
	})

	require.Error(t, err)
	assert.Equal(t, codes.NotFound, statusCode(t, err))
}

func TestAuthenticate_MissingAppID(t *testing.T) {
	svc := new(authServiceMock)
	client := newTestClient(t, svc)

	_, err := client.Authenticate(context.Background(), &authv1.LoginRequest{
		Email:    "test@example.com",
		Password: "password123",
	})

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, statusCode(t, err))
	svc.AssertNotCalled(t, "Login")
}

// --- RefreshTokens ---

func TestRefreshTokens_Success(t *testing.T) {
	svc := new(authServiceMock)
	svc.On("RefreshTokens", mock.Anything, "old-refresh", uint64(1)).
		Return("new-access", "new-refresh", nil)

	client := newTestClient(t, svc)

	resp, err := client.RefreshTokens(context.Background(), &authv1.RefreshTokensRequest{
		RefreshToken:  "old-refresh",
		ApplicationId: 1,
	})

	require.NoError(t, err)
	assert.Equal(t, "new-access", resp.GetAccessToken())
	assert.Equal(t, "new-refresh", resp.GetRefreshToken())
}

func TestRefreshTokens_InvalidToken(t *testing.T) {
	svc := new(authServiceMock)
	svc.On("RefreshTokens", mock.Anything, mock.Anything, mock.Anything).
		Return("", "", authsvc.ErrRefreshTokenInvalid)

	client := newTestClient(t, svc)

	_, err := client.RefreshTokens(context.Background(), &authv1.RefreshTokensRequest{
		RefreshToken:  "expired-or-revoked",
		ApplicationId: 1,
	})

	require.Error(t, err)
	// Unauthenticated, а не InvalidArgument — клиент должен понимать,
	// что нужно заново логиниться, а не просто "исправить запрос".
	assert.Equal(t, codes.Unauthenticated, statusCode(t, err))
}

func TestRefreshTokens_AppNotFound(t *testing.T) {
	svc := new(authServiceMock)
	svc.On("RefreshTokens", mock.Anything, mock.Anything, mock.Anything).
		Return("", "", storage.ErrAppNotFound)

	client := newTestClient(t, svc)

	_, err := client.RefreshTokens(context.Background(), &authv1.RefreshTokensRequest{
		RefreshToken:  "some-token",
		ApplicationId: 999,
	})

	require.Error(t, err)
	assert.Equal(t, codes.NotFound, statusCode(t, err))
}

// --- Logout ---

func TestLogout_Success(t *testing.T) {
	svc := new(authServiceMock)
	svc.On("Logout", mock.Anything, "some-refresh-token").Return(nil)

	client := newTestClient(t, svc)

	resp, err := client.Logout(context.Background(), &authv1.LogoutRequest{
		RefreshToken: "some-refresh-token",
	})

	require.NoError(t, err)
	assert.True(t, resp.GetSuccess())
}

func TestLogout_EmptyToken(t *testing.T) {
	svc := new(authServiceMock)
	client := newTestClient(t, svc)

	_, err := client.Logout(context.Background(), &authv1.LogoutRequest{})

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, statusCode(t, err))
	svc.AssertNotCalled(t, "Logout")
}

// --- GetRole ---

func TestGetRole_Admin(t *testing.T) {
	svc := new(authServiceMock)
	svc.On("IsAdmin", mock.Anything, uint64(1)).Return(true, nil)

	client := newTestClient(t, svc)

	resp, err := client.GetRole(context.Background(), &authv1.GetRoleRequest{UserId: 1})

	require.NoError(t, err)
	assert.Equal(t, authv1.Role_ROLE_ADMIN, resp.GetRole())
}

func TestGetRole_RegularUser(t *testing.T) {
	svc := new(authServiceMock)
	svc.On("IsAdmin", mock.Anything, uint64(1)).Return(false, nil)

	client := newTestClient(t, svc)

	resp, err := client.GetRole(context.Background(), &authv1.GetRoleRequest{UserId: 1})

	require.NoError(t, err)
	assert.Equal(t, authv1.Role_ROLE_USER, resp.GetRole())
}

func TestGetRole_UserNotFound(t *testing.T) {
	svc := new(authServiceMock)
	svc.On("IsAdmin", mock.Anything, uint64(999)).Return(false, storage.ErrUserNotFound)

	client := newTestClient(t, svc)

	_, err := client.GetRole(context.Background(), &authv1.GetRoleRequest{UserId: 999})

	require.Error(t, err)
	assert.Equal(t, codes.NotFound, statusCode(t, err))
}

func TestGetRole_MissingUserID(t *testing.T) {
	svc := new(authServiceMock)
	client := newTestClient(t, svc)

	_, err := client.GetRole(context.Background(), &authv1.GetRoleRequest{})

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, statusCode(t, err))
	svc.AssertNotCalled(t, "IsAdmin")
}
