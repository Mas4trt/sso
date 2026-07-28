package postgres_test

import (
	"context"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

func runMigrations(dsn string) error {
	m, err := migrate.New("file://../../../migrations", dsn)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}

// seedApp вставляет тестовое приложение напрямую в БД, в обход Storage —
// apps являются статическими доверенными клиентами и намеренно
// не создаются через публичный API сервиса.
func seedApp(ctx context.Context, dsn, name, secret string) (uint64, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return 0, fmt.Errorf("seedApp: connect: %w", err)
	}
	defer pool.Close()

	var id uint64
	err = pool.QueryRow(ctx,
		`INSERT INTO apps (name, secret) VALUES ($1, $2)
		 ON CONFLICT (name) DO UPDATE SET secret = EXCLUDED.secret
		 RETURNING id`,
		name, secret,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("seedApp: insert: %w", err)
	}

	return id, nil
}
