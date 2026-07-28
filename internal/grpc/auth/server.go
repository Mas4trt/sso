package auth

import (
	"context"
	"errors"

	authsvc "sso/internal/services/auth"
	"sso/internal/storage"

	authv1 "github.com/Mas4trt/protos/gen/go/auth/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AuthService interface {
	RegisterNewUser(ctx context.Context, email, password string) (userID uint64, err error)
	Login(ctx context.Context, email, password string, appID uint64) (access, refresh string, err error)
	RefreshTokens(ctx context.Context, refreshToken string, appID uint64) (access, refresh string, err error)
	Logout(ctx context.Context, refreshToken string) error
	IsAdmin(ctx context.Context, userID uint64) (bool, error)
}

type serverAPI struct {
	authv1.UnimplementedAuthServer
	auth AuthService
}

func Register(gRPC *grpc.Server, auth AuthService) {
	authv1.RegisterAuthServer(gRPC, &serverAPI{auth: auth})
}

func (s *serverAPI) Register(ctx context.Context, in *authv1.RegisterRequest) (*authv1.RegisterResponse, error) {
	if err := validateRegister(in); err != nil {
		return nil, err
	}

	uid, err := s.auth.RegisterNewUser(ctx, in.GetEmail(), in.GetPassword())
	if err != nil {
		if errors.Is(err, authsvc.ErrUserExists) {
			return nil, status.Error(codes.AlreadyExists, "user already exists")
		}
		return nil, status.Error(codes.Internal, "failed to register user")
	}

	return &authv1.RegisterResponse{UserId: uid}, nil
}

func (s *serverAPI) Authenticate(ctx context.Context, in *authv1.LoginRequest) (*authv1.LoginResponse, error) {
	if err := validateLogin(in); err != nil {
		return nil, err
	}

	access, refresh, err := s.auth.Login(ctx, in.GetEmail(), in.GetPassword(), in.GetApplicationId())
	if err != nil {
		if errors.Is(err, authsvc.ErrInvalidCredentials) {
			return nil, status.Error(codes.InvalidArgument, "invalid email or password")
		}
		if errors.Is(err, storage.ErrAppNotFound) {
			return nil, status.Error(codes.NotFound, "app not found")
		}
		return nil, status.Error(codes.Internal, "failed to login")
	}

	return &authv1.LoginResponse{AccessToken: access, RefreshToken: refresh}, nil
}

func (s *serverAPI) GetRole(ctx context.Context, in *authv1.GetRoleRequest) (*authv1.GetRoleResponse, error) {
	if in.GetUserId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	isAdmin, err := s.auth.IsAdmin(ctx, in.GetUserId())
	if err != nil {
		if errors.Is(err, storage.ErrUserNotFound) {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		return nil, status.Error(codes.Internal, "failed to get role")
	}

	role := authv1.Role_ROLE_USER
	if isAdmin {
		role = authv1.Role_ROLE_ADMIN
	}

	return &authv1.GetRoleResponse{Role: role}, nil
}

func validateRegister(in *authv1.RegisterRequest) error {
	if in.GetEmail() == "" {
		return status.Error(codes.InvalidArgument, "email is required")
	}
	if in.GetPassword() == "" {
		return status.Error(codes.InvalidArgument, "password is required")
	}
	return nil
}

func validateLogin(in *authv1.LoginRequest) error {
	if in.GetEmail() == "" {
		return status.Error(codes.InvalidArgument, "email is required")
	}
	if in.GetPassword() == "" {
		return status.Error(codes.InvalidArgument, "password is required")
	}
	if in.GetApplicationId() == 0 {
		return status.Error(codes.InvalidArgument, "app_id is required")
	}
	return nil
}

func (s *serverAPI) RefreshTokens(ctx context.Context, in *authv1.RefreshTokensRequest) (*authv1.LoginResponse, error) {
	if in.GetRefreshToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "refresh_token is required")
	}
	if in.GetApplicationId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "app_id is required")
	}

	access, refresh, err := s.auth.RefreshTokens(ctx, in.GetRefreshToken(), in.GetApplicationId())
	if err != nil {
		if errors.Is(err, authsvc.ErrRefreshTokenInvalid) {
			return nil, status.Error(codes.Unauthenticated, "refresh token invalid or expired")
		}
		return nil, status.Error(codes.Internal, "failed to refresh tokens")
	}

	return &authv1.LoginResponse{AccessToken: access, RefreshToken: refresh}, nil
}

func (s *serverAPI) Logout(ctx context.Context, in *authv1.LogoutRequest) (*authv1.LogoutResponse, error) {
	if in.GetRefreshToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "refresh_token is required")
	}

	if err := s.auth.Logout(ctx, in.GetRefreshToken()); err != nil {
		return nil, status.Error(codes.Internal, "failed to logout")
	}

	return &authv1.LogoutResponse{Success: true}, nil
}
