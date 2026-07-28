package jwt

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"sso/internal/domain/models"

	"github.com/golang-jwt/jwt/v5"
)

func NewAccessToken(user models.User, app models.App, ttl time.Duration) (string, error) {
	const op = "lib.jwt.NewAccessToken"

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid":    user.ID,
		"email":  user.Email,
		"app_id": app.ID,
		"exp":    time.Now().Add(ttl).Unix(),
		"iat":    time.Now().Unix(),
	})

	signed, err := token.SignedString([]byte(app.Secret))
	if err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}

	return signed, nil
}

// NewRefreshToken генерирует непрозрачный opaque-токен (не JWT).
// Верификация делается по хэшу в БД, а не по подписи — это позволяет
// отзывать refresh-токены (revocation), чего JWT сам по себе не умеет.
func NewRefreshToken() (string, error) {
	const op = "lib.jwt.NewRefreshToken"

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
