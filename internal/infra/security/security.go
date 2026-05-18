package security

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	"github.com/alexedwards/argon2id"
	"github.com/fernet/fernet-go"
	"github.com/golang-jwt/jwt/v5"
)

type Service struct {
	settings config.Settings
}

func New(settings config.Settings) Service {
	return Service{settings: settings}
}

func (s Service) EncryptSecret(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	keys, err := fernet.DecodeKeys(fernetKey(s.settings.AppSecretKey))
	if err != nil {
		return "", err
	}
	token, err := fernet.EncryptAndSign([]byte(value), keys[0])
	if err != nil {
		return "", err
	}
	return string(token), nil
}

func (s Service) DecryptSecret(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	keys, err := fernet.DecodeKeys(fernetKey(s.settings.AppSecretKey))
	if err != nil {
		return "", err
	}
	plain := fernet.VerifyAndDecrypt([]byte(value), 0, keys)
	if plain == nil {
		return "", errors.New("secret cannot be decrypted with the configured APP_SECRET_KEY")
	}
	return string(plain), nil
}

func (s Service) HashPassword(password string) (string, error) {
	return argon2id.CreateHash(password, argon2id.DefaultParams)
}

func (s Service) VerifyPassword(password string, hash string) bool {
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	return err == nil && match
}

func (s Service) CreateAccessToken(subject string, extra map[string]any) (string, error) {
	claims := jwt.MapClaims{
		"sub": subject,
		"exp": time.Now().Add(time.Duration(s.settings.AccessTokenExpireMinutes) * time.Minute).Unix(),
	}
	for key, value := range extra {
		claims[key] = value
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.settings.JWTSecretKey))
}

func (s Service) DecodeAccessToken(token string) (jwt.MapClaims, error) {
	parsed, err := jwt.Parse(token, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected jwt signing method")
		}
		return []byte(s.settings.JWTSecretKey), nil
	})
	if err != nil || !parsed.Valid {
		return nil, errors.New("invalid access token")
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid access token")
	}
	return claims, nil
}

func fernetKey(master string) string {
	sum := sha256.Sum256([]byte(master))
	return base64.URLEncoding.EncodeToString(sum[:])
}
