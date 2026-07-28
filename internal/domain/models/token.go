package models

import "time"

type RefreshToken struct {
	UserID    uint64
	AppID     uint64
	ExpiresAt time.Time
	RevokedAt *time.Time
}
