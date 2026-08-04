package auth_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"sso/internal/domain/models"
	grpcauth "sso/internal/grpc/auth"
	"sso/internal/grpc/interceptors"
	authsvc "sso/internal/services/auth"
	"sso/internal/storage"
	"sso/pkg/jwt"

	authv1 "github.com/Mas4trt/protos/gen/go/auth/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
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

func newTestClient(t *testing.T, svc grpcauth.AuthService, apps interceptors.AppSecretLookup) authv1.AuthClient {
	t.Helper()

	const bufSize = 1024 * 1024
	lis := bufconn.Listen(bufSize)

	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(interceptors.NewAuthInterceptor(apps, "/auth.v1.Auth/GetRole")),
	)
	grpcauth.Register(srv, svc)

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
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

	client := newTestClient(t, svc, new(appSecretMock))

	resp, err := client.Register(context.Background(), &authv1.RegisterRequest{
		Email:    "test@example.com",
		Password: "password123",
	})

	require.NoError(t, err)
	assert.Equal(t, uint64(1), resp.GetUserId())
}

func TestRegister_EmptyEmail(t *testing.T) {
	svc := new(authServiceMock)
	client := newTestClient(t, svc, new(appSecretMock))

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

	client := newTestClient(t, svc, new(appSecretMock))

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

	client := newTestClient(t, svc, new(appSecretMock))

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

	client := newTestClient(t, svc, new(appSecretMock))

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

	client := newTestClient(t, svc, new(appSecretMock))

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
	client := newTestClient(t, svc, new(appSecretMock))

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

	client := newTestClient(t, svc, new(appSecretMock))

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

	client := newTestClient(t, svc, new(appSecretMock))

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

	client := newTestClient(t, svc, new(appSecretMock))

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

	client := newTestClient(t, svc, new(appSecretMock))

	resp, err := client.Logout(context.Background(), &authv1.LogoutRequest{
		RefreshToken: "some-refresh-token",
	})

	require.NoError(t, err)
	assert.True(t, resp.GetSuccess())
}

func TestLogout_EmptyToken(t *testing.T) {
	svc := new(authServiceMock)
	client := newTestClient(t, svc, new(appSecretMock))

	_, err := client.Logout(context.Background(), &authv1.LogoutRequest{})

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, statusCode(t, err))
	svc.AssertNotCalled(t, "Logout")
}

// appSecretMock backs interceptors.AppSecretLookup in tests.
type appSecretMock struct{ mock.Mock }

func (m *appSecretMock) App(ctx context.Context, appID uint64) (models.App, error) {
	args := m.Called(ctx, appID)
	return args.Get(0).(models.App), args.Error(1)
}

func mintToken(t *testing.T, uid, appID uint64, secret string) string {
	t.Helper()
	tok, err := jwt.NewAccessToken(models.User{ID: uid}, models.App{ID: appID, Secret: secret}, time.Hour)
	require.NoError(t, err)
	return tok
}

func withBearer(ctx context.Context, token string) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))
}

// --- GetRole ---

func TestGetRole_Self(t *testing.T) {
	svc := new(authServiceMock)
	svc.On("IsAdmin", mock.Anything, uint64(1)).Return(false, nil)

	apps := new(appSecretMock)
	apps.On("App", mock.Anything, uint64(1)).Return(models.App{ID: 1, Secret: "s1"}, nil)

	client := newTestClient(t, svc, apps)
	token := mintToken(t, 1, 1, "s1")

	resp, err := client.GetRole(withBearer(context.Background(), token), &authv1.GetRoleRequest{UserId: 1})

	require.NoError(t, err)
	assert.Equal(t, authv1.Role_ROLE_USER, resp.GetRole())
}

func TestGetRole_OtherUser_Denied(t *testing.T) {
	svc := new(authServiceMock)
	svc.On("IsAdmin", mock.Anything, uint64(1)).Return(false, nil) // caller (1) is not admin

	apps := new(appSecretMock)
	apps.On("App", mock.Anything, uint64(1)).Return(models.App{ID: 1, Secret: "s1"}, nil)

	client := newTestClient(t, svc, apps)
	token := mintToken(t, 1, 1, "s1") // authenticated as user 1

	_, err := client.GetRole(withBearer(context.Background(), token), &authv1.GetRoleRequest{UserId: 2})

	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, statusCode(t, err))
	svc.AssertNotCalled(t, "IsAdmin", mock.Anything, uint64(2))
}

func TestGetRole_AdminCanViewOthers(t *testing.T) {
	svc := new(authServiceMock)
	svc.On("IsAdmin", mock.Anything, uint64(1)).Return(true, nil)  // caller is admin
	svc.On("IsAdmin", mock.Anything, uint64(2)).Return(false, nil) // target user

	apps := new(appSecretMock)
	apps.On("App", mock.Anything, uint64(1)).Return(models.App{ID: 1, Secret: "s1"}, nil)

	client := newTestClient(t, svc, apps)
	token := mintToken(t, 1, 1, "s1")

	resp, err := client.GetRole(withBearer(context.Background(), token), &authv1.GetRoleRequest{UserId: 2})

	require.NoError(t, err)
	assert.Equal(t, authv1.Role_ROLE_USER, resp.GetRole())
}

func TestGetRole_NoToken_Unauthenticated(t *testing.T) {
	svc := new(authServiceMock)
	apps := new(appSecretMock)
	client := newTestClient(t, svc, apps)

	_, err := client.GetRole(context.Background(), &authv1.GetRoleRequest{UserId: 1})

	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, statusCode(t, err))
	svc.AssertNotCalled(t, "IsAdmin")
}

func TestRegister_PasswordTooLong(t *testing.T) {
	svc := new(authServiceMock)
	apps := new(appSecretMock)
	client := newTestClient(t, svc, apps)

	_, err := client.Register(context.Background(), &authv1.RegisterRequest{
		Email:    "test@example.com",
		Password: strings.Repeat("a", 73),
	})

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, statusCode(t, err))
	svc.AssertNotCalled(t, "RegisterNewUser")
}
