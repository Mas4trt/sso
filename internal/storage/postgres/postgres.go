package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sso/internal/domain/models"
	"sso/internal/storage"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Storage struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*Storage, error) {
	const op = "storage.postgres.New"

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("%s: ping: %w", op, err)
	}

	return &Storage{pool: pool}, nil
}

func (s *Storage) Close() {
	s.pool.Close()
}

func (s *Storage) SaveUser(ctx context.Context, email string, passHash []byte) (uint64, error) {
	const op = "storage.postgres.SaveUser"

	var id uint64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (email, pass_hash) VALUES ($1, $2) RETURNING id`,
		email, passHash,
	).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return 0, fmt.Errorf("%s: %w", op, storage.ErrUserExists)
		}
		return 0, fmt.Errorf("%s: %w", op, err)
	}

	return id, nil
}
func (s *Storage) User(ctx context.Context, email string) (models.User, error) {
	const op = "storage.postgres.User"

	var u models.User
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, pass_hash, is_admin FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Email, &u.PassHash, &u.IsAdmin)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.User{}, fmt.Errorf("%s: %w", op, storage.ErrUserNotFound)
		}
		return models.User{}, fmt.Errorf("%s: %w", op, err)
	}

	return u, nil
}

func (s *Storage) UserByID(ctx context.Context, userID uint64) (models.User, error) {
	const op = "storage.postgres.UserByID"

	var u models.User
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, pass_hash, is_admin FROM users WHERE id = $1`, userID,
	).Scan(&u.ID, &u.Email, &u.PassHash, &u.IsAdmin)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.User{}, fmt.Errorf("%s: %w", op, storage.ErrUserNotFound)
		}
		return models.User{}, fmt.Errorf("%s: %w", op, err)
	}

	return u, nil
}

func (s *Storage) IsAdmin(ctx context.Context, userID uint64) (bool, error) {
	const op = "storage.postgres.IsAdmin"

	var isAdmin bool
	err := s.pool.QueryRow(ctx,
		`SELECT is_admin FROM users WHERE id = $1`, userID,
	).Scan(&isAdmin)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("%s: %w", op, storage.ErrUserNotFound)
		}
		return false, fmt.Errorf("%s: %w", op, err)
	}

	return isAdmin, nil
}

func (s *Storage) App(ctx context.Context, appID uint64) (models.App, error) {
	const op = "storage.postgres.App"

	var app models.App
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, secret FROM apps WHERE id = $1`, appID,
	).Scan(&app.ID, &app.Name, &app.Secret)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.App{}, fmt.Errorf("%s: %w", op, storage.ErrAppNotFound)
		}
		return models.App{}, fmt.Errorf("%s: %w", op, err)
	}

	return app, nil
}

func (s *Storage) SaveRefreshToken(ctx context.Context, tokenHash []byte, userID, appID uint64, expiresAt time.Time) error {
	const op = "storage.postgres.SaveRefreshToken"

	_, err := s.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (token_hash, user_id, app_id, expires_at) VALUES ($1, $2, $3, $4)`,
		tokenHash, userID, appID, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *Storage) RefreshToken(ctx context.Context, tokenHash []byte) (models.RefreshToken, error) {
	const op = "storage.postgres.RefreshToken"

	var rt models.RefreshToken
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, app_id, expires_at, revoked_at FROM refresh_tokens WHERE token_hash = $1`,
		tokenHash,
	).Scan(&rt.UserID, &rt.AppID, &rt.ExpiresAt, &rt.RevokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.RefreshToken{}, fmt.Errorf("%s: %w", op, storage.ErrRefreshTokenInvalid)
		}
		return models.RefreshToken{}, fmt.Errorf("%s: %w", op, err)
	}

	return rt, nil
}

// RevokeRefreshToken делает revoke сразу и по конкретному токену (logout),
// используется также внутри RotateRefreshToken перед выдачей новой пары.
func (s *Storage) RevokeRefreshToken(ctx context.Context, tokenHash []byte) error {
	const op = "storage.postgres.RevokeRefreshToken"

	_, err := s.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`,
		tokenHash,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

// RevokeAllUserTokens — на случай компрометации аккаунта ("выйти со всех устройств").
func (s *Storage) RevokeAllUserTokens(ctx context.Context, userID uint64) error {
	const op = "storage.postgres.RevokeAllUserTokens"

	_, err := s.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}
