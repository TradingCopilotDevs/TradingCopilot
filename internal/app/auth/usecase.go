package auth

import (
	"context"
	"errors"
	domainauth "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/auth"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrAdminExists       = errors.New("admin already exists")
	ErrInvalidAdmin      = errors.New("invalid admin")
	ErrInvalidCredential = errors.New("invalid username or password")
)

type AdminRepository interface {
	CountAdmins(ctx context.Context) (int64, error)
	CreateAdmin(ctx context.Context, user *domainauth.AdminUser) error
	FindAdminByUsername(ctx context.Context, username string) (*domainauth.AdminUser, error)
}

type Transactor interface {
	WithTx(ctx context.Context, fn func(AdminRepository) error) error
}

type SecurityService interface {
	HashPassword(password string) (string, error)
	VerifyPassword(password string, hash string) bool
	CreateAccessToken(subject string, extra map[string]any) (string, error)
	DecodeAccessToken(token string) (jwt.MapClaims, error)
}

type Usecase struct {
	admins   AdminRepository
	security SecurityService
	tx       Transactor
}

func NewUsecase(admins AdminRepository, security SecurityService, tx Transactor) Usecase {
	return Usecase{admins: admins, security: security, tx: tx}
}

func (u Usecase) BootstrapRequired(ctx context.Context) (bool, error) {
	count, err := u.admins.CountAdmins(ctx)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

func (u Usecase) Bootstrap(ctx context.Context, username string, password string) (string, error) {
	if username == "" || password == "" {
		return "", ErrInvalidAdmin
	}
	count, err := u.admins.CountAdmins(ctx)
	if err != nil {
		return "", err
	}
	if count > 0 {
		return "", ErrAdminExists
	}
	hash, err := u.security.HashPassword(password)
	if err != nil {
		return "", err
	}
	user := domainauth.AdminUser{Username: username, PasswordHash: hash}
	if err := u.tx.WithTx(ctx, func(repo AdminRepository) error {
		count, err := repo.CountAdmins(ctx)
		if err != nil {
			return err
		}
		if count > 0 {
			return ErrAdminExists
		}
		return repo.CreateAdmin(ctx, &user)
	}); err != nil {
		return "", err
	}
	return u.security.CreateAccessToken(user.Username, nil)
}

func (u Usecase) Login(ctx context.Context, username string, password string) (string, error) {
	user, err := u.admins.FindAdminByUsername(ctx, username)
	if err != nil || !u.security.VerifyPassword(password, user.PasswordHash) {
		return "", ErrInvalidCredential
	}
	return u.security.CreateAccessToken(user.Username, nil)
}

func (u Usecase) Authenticate(ctx context.Context, token string) (string, error) {
	claims, err := u.security.DecodeAccessToken(token)
	if err != nil {
		return "", ErrInvalidCredential
	}
	username, _ := claims["sub"].(string)
	if username == "" {
		return "", ErrInvalidCredential
	}
	if _, err := u.admins.FindAdminByUsername(ctx, username); err != nil {
		return "", ErrInvalidCredential
	}
	return username, nil
}
