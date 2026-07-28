CREATE TABLE IF NOT EXISTS users (
    id        BIGSERIAL PRIMARY KEY,
    email     TEXT NOT NULL UNIQUE,
    pass_hash BYTEA NOT NULL,
    is_admin  BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);