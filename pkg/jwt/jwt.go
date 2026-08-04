package jwt

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"sso/internal/domain/models"

	"github.com/golang-jwt/jwt/v5"
)

var ErrInvalidToken = errors.New("invalid access token")

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

func ParseUnverifiedAppID(accessToken string) (uint64, error) {
	const op = "lib.jwt.ParseUnverifiedAppID"

	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(accessToken, claims); err != nil {
		return 0, fmt.Errorf("%s: %w", op, ErrInvalidToken)
	}

	appID, ok := claims["app_id"].(float64) // JSON numbers decode as float64
	if !ok || appID <= 0 {
		return 0, fmt.Errorf("%s: %w", op, ErrInvalidToken)
	}
	return uint64(appID), nil
}

func VerifyAccessToken(accessToken, secret string) (uint64, error) {
	const op = "lib.jwt.VerifyAccessToken"

	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(accessToken, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return 0, fmt.Errorf("%s: %w", op, ErrInvalidToken)
	}

	uid, ok := claims["uid"].(float64)
	if !ok || uid <= 0 {
		return 0, fmt.Errorf("%s: %w", op, ErrInvalidToken)
	}
	return uint64(uid), nil
}
