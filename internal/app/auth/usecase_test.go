package auth

import (
	"context"
	"errors"
	domainauth "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/auth"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestUsecaseBootstrapCreatesAdminInTransactionAndLogin(t *testing.T) {
	repo := &fakeAdminRepo{}
	tx := &fakeAdminTransactor{repo: repo}
	uc := NewUsecase(repo, fakeSecurity{}, tx)

	required, err := uc.BootstrapRequired(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !required {
		t.Fatal("expected bootstrap to be required")
	}

	token, err := uc.Bootstrap(context.Background(), "admin", "password")
	if err != nil {
		t.Fatal(err)
	}
	if token != "token:admin" {
		t.Fatalf("unexpected token %q", token)
	}
	if tx.txCount != 1 {
		t.Fatalf("expected one explicit transaction, got %d", tx.txCount)
	}

	token, err = uc.Login(context.Background(), "admin", "password")
	if err != nil {
		t.Fatal(err)
	}
	if token != "token:admin" {
		t.Fatalf("unexpected login token %q", token)
	}
}

func TestUsecaseRejectsExistingAdminAndBadCredentials(t *testing.T) {
	repo := &fakeAdminRepo{users: map[string]domainauth.AdminUser{
		"admin": {Username: "admin", PasswordHash: "hash:password"},
	}}
	uc := NewUsecase(repo, fakeSecurity{}, &fakeAdminTransactor{repo: repo})

	if _, err := uc.Bootstrap(context.Background(), "admin2", "password"); !errors.Is(err, ErrAdminExists) {
		t.Fatalf("expected ErrAdminExists, got %v", err)
	}
	if _, err := uc.Login(context.Background(), "admin", "bad"); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("expected ErrInvalidCredential, got %v", err)
	}
}

type fakeAdminRepo struct {
	users map[string]domainauth.AdminUser
}

func (r *fakeAdminRepo) CountAdmins(context.Context) (int64, error) {
	return int64(len(r.users)), nil
}

func (r *fakeAdminRepo) CreateAdmin(_ context.Context, user *domainauth.AdminUser) error {
	if r.users == nil {
		r.users = map[string]domainauth.AdminUser{}
	}
	r.users[user.Username] = *user
	return nil
}

func (r *fakeAdminRepo) FindAdminByUsername(_ context.Context, username string) (*domainauth.AdminUser, error) {
	user, ok := r.users[username]
	if !ok {
		return nil, errors.New("not found")
	}
	return &user, nil
}

type fakeAdminTransactor struct {
	repo    *fakeAdminRepo
	txCount int
}

func (t *fakeAdminTransactor) WithTx(ctx context.Context, fn func(AdminRepository) error) error {
	t.txCount++
	return fn(t.repo)
}

type fakeSecurity struct{}

func (fakeSecurity) HashPassword(password string) (string, error) {
	return "hash:" + password, nil
}

func (fakeSecurity) VerifyPassword(password string, hash string) bool {
	return hash == "hash:"+password
}

func (fakeSecurity) CreateAccessToken(subject string, _ map[string]any) (string, error) {
	return "token:" + subject, nil
}

func (fakeSecurity) DecodeAccessToken(token string) (jwt.MapClaims, error) {
	if token == "token:admin" {
		return jwt.MapClaims{"sub": "admin"}, nil
	}
	return nil, errors.New("invalid token")
}
