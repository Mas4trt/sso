package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"sso/internal/domain/models"
	"sso/internal/storage"
	"sso/pkg/jwt"
	"sso/pkg/sl"

	"golang.org/x/crypto/bcrypt"
)

type UserSaver interface {
	SaveUser(ctx context.Context, email string, passHash []byte) (uint64, error)
}

type UserProvider interface {
	User(ctx context.Context, email string) (models.User, error)
	UserByID(ctx context.Context, userID uint64) (models.User, error)
	IsAdmin(ctx context.Context, userID uint64) (bool, error)
}

type AppProvider interface {
	App(ctx context.Context, appID uint64) (models.App, error)
}

type TokenSaver interface {
	SaveRefreshToken(ctx context.Context, tokenHash []byte, userID, appID uint64, expiresAt time.Time) error
	RefreshToken(ctx context.Context, tokenHash []byte) (models.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenHash []byte) error
}

type Auth struct {
	log         *slog.Logger
	usrSaver    UserSaver
	usrProvider UserProvider
	appProvider AppProvider
	tokenSaver  TokenSaver
	tokenTTL    time.Duration
	refreshTTL  time.Duration
}

func New(
	log *slog.Logger,
	usrSaver UserSaver,
	usrProvider UserProvider,
	appProvider AppProvider,
	tokenSaver TokenSaver,
	tokenTTL time.Duration,
	refreshTTL time.Duration,
) *Auth {
	return &Auth{
		log:         log,
		usrSaver:    usrSaver,
		usrProvider: usrProvider,
		appProvider: appProvider,
		tokenSaver:  tokenSaver,
		tokenTTL:    tokenTTL,
		refreshTTL:  refreshTTL,
	}
}

func (a *Auth) RegisterNewUser(ctx context.Context, email, password string) (uint64, error) {
	const op = "auth.RegisterNewUser"
	log := a.log.With(slog.String("op", op), slog.String("email", email))

	passHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Error("failed to generate password hash", sl.Err(err))
		return 0, fmt.Errorf("%s: %w", op, err)
	}

	id, err := a.usrSaver.SaveUser(ctx, email, passHash)
	if err != nil {
		if errors.Is(err, storage.ErrUserExists) {
			log.Warn("user already exists")
			return 0, fmt.Errorf("%s: %w", op, ErrUserExists)
		}
		log.Error("failed to save user", sl.Err(err))
		return 0, fmt.Errorf("%s: %w", op, err)
	}

	return id, nil
}

func (a *Auth) Login(ctx context.Context, email, password string, appID uint64) (access, refresh string, err error) {
	const op = "auth.Login"
	log := a.log.With(slog.String("op", op), slog.String("email", email))

	user, err := a.usrProvider.User(ctx, email)
	if err != nil {
		if errors.Is(err, storage.ErrUserNotFound) {
			log.Warn("user not found")
			return "", "", fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
		}
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	if err := bcrypt.CompareHashAndPassword(user.PassHash, []byte(password)); err != nil {
		return "", "", fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
	}

	app, err := a.appProvider.App(ctx, appID)
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	return a.issueTokenPair(ctx, user, app)
}

func (a *Auth) IsAdmin(ctx context.Context, userID uint64) (bool, error) {
	const op = "auth.IsAdmin"

	isAdmin, err := a.usrProvider.IsAdmin(ctx, userID)
	if err != nil {
		if errors.Is(err, storage.ErrUserNotFound) {
			return false, fmt.Errorf("%s: %w", op, storage.ErrUserNotFound)
		}
		return false, fmt.Errorf("%s: %w", op, err)
	}

	return isAdmin, nil
}

func (a *Auth) RefreshTokens(ctx context.Context, refreshToken string, appID uint64) (access, refresh string, err error) {
	const op = "auth.RefreshTokens"
	log := a.log.With(slog.String("op", op))

	tokenHash := jwt.HashToken(refreshToken)

	stored, err := a.tokenSaver.RefreshToken(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, storage.ErrRefreshTokenInvalid) {
			return "", "", fmt.Errorf("%s: %w", op, ErrRefreshTokenInvalid)
		}
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	if stored.RevokedAt != nil || time.Now().After(stored.ExpiresAt) || stored.AppID != appID {
		log.Warn("refresh token invalid", slog.Uint64("user_id", stored.UserID))
		return "", "", fmt.Errorf("%s: %w", op, ErrRefreshTokenInvalid)
	}

	// Ротация: старый токен инвалидируем сразу, даже если что-то пойдёт не так дальше —
	// это защита от replay-атак
	if err := a.tokenSaver.RevokeRefreshToken(ctx, tokenHash); err != nil {
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	app, err := a.appProvider.App(ctx, appID)
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	user, err := a.usrProvider.UserByID(ctx, stored.UserID)
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	return a.issueTokenPair(ctx, user, app)
}

func (a *Auth) Logout(ctx context.Context, refreshToken string) error {
	const op = "auth.Logout"

	tokenHash := jwt.HashToken(refreshToken)

	if err := a.tokenSaver.RevokeRefreshToken(ctx, tokenHash); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (a *Auth) issueTokenPair(ctx context.Context, user models.User, app models.App) (access, refresh string, err error) {
	const op = "auth.issueTokenPair"

	access, err = jwt.NewAccessToken(user, app, a.tokenTTL)
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	refresh, err = jwt.NewRefreshToken()
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	expiresAt := time.Now().Add(a.refreshTTL)
	if err := a.tokenSaver.SaveRefreshToken(ctx, jwt.HashToken(refresh), user.ID, app.ID, expiresAt); err != nil {
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	return access, refresh, nil
}
