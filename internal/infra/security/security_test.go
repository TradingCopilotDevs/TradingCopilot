package security

import (
	"testing"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
)

func TestSecretRoundTrip(t *testing.T) {
	svc := New(config.Settings{AppSecretKey: "local-dev-secret-change-before-real-use-32-bytes", JWTSecretKey: "jwt"})
	encrypted, err := svc.EncryptSecret("sensitive-value")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "sensitive-value" {
		t.Fatal("secret was not encrypted")
	}
	decrypted, err := svc.DecryptSecret(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "sensitive-value" {
		t.Fatalf("unexpected decrypted value %q", decrypted)
	}
}

func TestPasswordHashRoundTrip(t *testing.T) {
	svc := New(config.Settings{AppSecretKey: "secret", JWTSecretKey: "jwt"})
	hash, err := svc.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !svc.VerifyPassword("correct horse battery staple", hash) {
		t.Fatal("password did not verify")
	}
	if svc.VerifyPassword("wrong", hash) {
		t.Fatal("wrong password verified")
	}
}
