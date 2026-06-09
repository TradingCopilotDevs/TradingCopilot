package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	domainauth "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/auth"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrAdminExists          = errors.New("admin already exists")
	ErrInvalidAdmin         = errors.New("invalid admin")
	ErrInvalidCredential    = errors.New("invalid username or password")
	ErrInvalidUser          = errors.New("invalid user")
	ErrUserExists           = errors.New("user already exists")
	ErrUserNotFound         = errors.New("user not found")
	ErrSessionNotFound      = errors.New("session not found")
	ErrConfirmationRequired = errors.New("confirmation required")
)

type AdminRepository interface {
	CountAdmins(ctx context.Context) (int64, error)
	CreateAdmin(ctx context.Context, user *domainauth.AdminUser) error
	ListAdmins(ctx context.Context) ([]domainauth.AdminUser, error)
	FindAdminByID(ctx context.Context, id uint) (*domainauth.AdminUser, error)
	FindAdminByUsername(ctx context.Context, username string) (*domainauth.AdminUser, error)
	UpdateAdmin(ctx context.Context, user *domainauth.AdminUser) error
	CreateSession(ctx context.Context, session *domainauth.AuthSession) error
	FindSessionByTokenID(ctx context.Context, tokenID string) (*domainauth.AuthSession, error)
	ListSessions(ctx context.Context, username string, includeRevoked bool, limit int) ([]domainauth.AuthSession, error)
	RevokeSession(ctx context.Context, id uint, revokedAt time.Time, revokedBy string, reason string) (*domainauth.AuthSession, bool, error)
	CreateAuditEvent(ctx context.Context, event *domainauth.AuditEvent) error
	ListAuditEvents(ctx context.Context, limit int) ([]domainauth.AuditEvent, error)
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

type Principal struct {
	Username  string
	UserID    uint
	Role      string
	SessionID uint
}

type RequestMeta struct {
	Actor     string
	IP        string
	UserAgent string
}

type CreateUserInput struct {
	Username    string
	DisplayName string
	Role        string
	Password    string
	Active      *bool
}

type UpdateUserInput struct {
	DisplayName *string
	Role        *string
	Active      *bool
}

type ResetPasswordInput struct {
	Password string
	Confirm  bool
}

type RevokeSessionInput struct {
	Reason  string
	Confirm bool
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
	user := domainauth.AdminUser{Username: strings.TrimSpace(username), DisplayName: strings.TrimSpace(username), Role: "owner", Active: true, PasswordHash: hash}
	var token string
	if err := u.tx.WithTx(ctx, func(repo AdminRepository) error {
		count, err := repo.CountAdmins(ctx)
		if err != nil {
			return err
		}
		if count > 0 {
			return ErrAdminExists
		}
		if err := repo.CreateAdmin(ctx, &user); err != nil {
			return err
		}
		createdToken, session, err := u.createTokenAndSession(ctx, repo, user, RequestMeta{Actor: user.Username})
		if err != nil {
			return err
		}
		token = createdToken
		return repo.CreateAuditEvent(ctx, &domainauth.AuditEvent{
			Actor: user.Username, Action: "auth.bootstrap", ResourceType: "admin-users", ResourceID: strconv.FormatUint(uint64(user.ID), 10),
			Outcome: "ok", Detail: fmt.Sprintf("initial owner session %d created", session.ID), CreatedAt: time.Now(),
		})
	}); err != nil {
		return "", err
	}
	return token, nil
}

func (u Usecase) Login(ctx context.Context, username string, password string) (string, error) {
	user, err := u.admins.FindAdminByUsername(ctx, username)
	if err != nil || user == nil || !user.Active || !u.security.VerifyPassword(password, user.PasswordHash) {
		return "", ErrInvalidCredential
	}
	var token string
	if err := u.tx.WithTx(ctx, func(repo AdminRepository) error {
		now := time.Now()
		user.LastLoginAt = &now
		if err := repo.UpdateAdmin(ctx, user); err != nil {
			return err
		}
		createdToken, session, err := u.createTokenAndSession(ctx, repo, *user, RequestMeta{Actor: user.Username})
		if err != nil {
			return err
		}
		token = createdToken
		return repo.CreateAuditEvent(ctx, &domainauth.AuditEvent{
			Actor: user.Username, Action: "auth.login", ResourceType: "auth-sessions", ResourceID: strconv.FormatUint(uint64(session.ID), 10),
			Outcome: "ok", Detail: "login succeeded", CreatedAt: time.Now(),
		})
	}); err != nil {
		return "", err
	}
	return token, nil
}

func (u Usecase) Authenticate(ctx context.Context, token string) (string, error) {
	principal, err := u.AuthenticatePrincipal(ctx, token)
	if err != nil {
		return "", err
	}
	return principal.Username, nil
}

func (u Usecase) AuthenticatePrincipal(ctx context.Context, token string) (Principal, error) {
	claims, err := u.security.DecodeAccessToken(token)
	if err != nil {
		return Principal{}, ErrInvalidCredential
	}
	username, _ := claims["sub"].(string)
	if username == "" {
		return Principal{}, ErrInvalidCredential
	}
	user, err := u.admins.FindAdminByUsername(ctx, username)
	if err != nil || user == nil || !user.Active {
		return Principal{}, ErrInvalidCredential
	}
	principal := Principal{Username: user.Username, UserID: user.ID, Role: normalizedRole(user.Role)}
	tokenID, _ := claims["jti"].(string)
	if tokenID == "" {
		return principal, nil
	}
	session, err := u.admins.FindSessionByTokenID(ctx, tokenID)
	if err != nil || session == nil || session.RevokedAt != nil || time.Now().After(session.ExpiresAt) {
		return Principal{}, ErrInvalidCredential
	}
	principal.SessionID = session.ID
	return principal, nil
}

func (u Usecase) Logout(ctx context.Context, principal Principal, meta RequestMeta) (*domainauth.AuthSession, error) {
	if principal.SessionID == 0 {
		return nil, ErrSessionNotFound
	}
	if meta.Actor == "" {
		meta.Actor = principal.Username
	}
	var out *domainauth.AuthSession
	if err := u.tx.WithTx(ctx, func(repo AdminRepository) error {
		session, found, err := repo.RevokeSession(ctx, principal.SessionID, time.Now(), meta.Actor, "self logout")
		if err != nil {
			return err
		}
		if !found {
			return ErrSessionNotFound
		}
		out = session
		return repo.CreateAuditEvent(ctx, auditEvent(meta, "auth.logout", "auth-sessions", strconv.FormatUint(uint64(session.ID), 10), "ok", "session logged out by current user"))
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func (u Usecase) ListUsers(ctx context.Context) ([]domainauth.AdminUser, error) {
	return u.admins.ListAdmins(ctx)
}

func (u Usecase) CreateUser(ctx context.Context, input CreateUserInput, meta RequestMeta) (*domainauth.AdminUser, error) {
	username := strings.TrimSpace(input.Username)
	password := strings.TrimSpace(input.Password)
	role := normalizedRole(input.Role)
	if username == "" || password == "" || !validRole(role) {
		return nil, ErrInvalidUser
	}
	if _, err := u.admins.FindAdminByUsername(ctx, username); err == nil {
		return nil, ErrUserExists
	}
	hash, err := u.security.HashPassword(password)
	if err != nil {
		return nil, err
	}
	active := true
	if input.Active != nil {
		active = *input.Active
	}
	user := domainauth.AdminUser{
		Username: username, DisplayName: strings.TrimSpace(input.DisplayName), Role: role, Active: active, PasswordHash: hash,
	}
	if user.DisplayName == "" {
		user.DisplayName = username
	}
	if err := u.tx.WithTx(ctx, func(repo AdminRepository) error {
		if err := repo.CreateAdmin(ctx, &user); err != nil {
			return err
		}
		return repo.CreateAuditEvent(ctx, auditEvent(meta, "admin.users.create", "admin-users", strconv.FormatUint(uint64(user.ID), 10), "ok", "user created"))
	}); err != nil {
		return nil, err
	}
	return &user, nil
}

func (u Usecase) UpdateUser(ctx context.Context, id uint, input UpdateUserInput, meta RequestMeta) (*domainauth.AdminUser, error) {
	user, err := u.admins.FindAdminByID(ctx, id)
	if err != nil || user == nil {
		return nil, ErrUserNotFound
	}
	if input.DisplayName != nil {
		user.DisplayName = strings.TrimSpace(*input.DisplayName)
	}
	if input.Role != nil {
		role := normalizedRole(*input.Role)
		if !validRole(role) {
			return nil, ErrInvalidUser
		}
		user.Role = role
	}
	if input.Active != nil {
		user.Active = *input.Active
	}
	if user.DisplayName == "" {
		user.DisplayName = user.Username
	}
	if err := u.tx.WithTx(ctx, func(repo AdminRepository) error {
		if err := repo.UpdateAdmin(ctx, user); err != nil {
			return err
		}
		return repo.CreateAuditEvent(ctx, auditEvent(meta, "admin.users.update", "admin-users", strconv.FormatUint(uint64(user.ID), 10), "ok", "user updated"))
	}); err != nil {
		return nil, err
	}
	return user, nil
}

func (u Usecase) ResetPassword(ctx context.Context, id uint, input ResetPasswordInput, meta RequestMeta) (*domainauth.AdminUser, error) {
	if !input.Confirm {
		return nil, ErrConfirmationRequired
	}
	password := strings.TrimSpace(input.Password)
	if password == "" {
		return nil, ErrInvalidUser
	}
	user, err := u.admins.FindAdminByID(ctx, id)
	if err != nil || user == nil {
		return nil, ErrUserNotFound
	}
	hash, err := u.security.HashPassword(password)
	if err != nil {
		return nil, err
	}
	user.PasswordHash = hash
	if err := u.tx.WithTx(ctx, func(repo AdminRepository) error {
		if err := repo.UpdateAdmin(ctx, user); err != nil {
			return err
		}
		return repo.CreateAuditEvent(ctx, auditEvent(meta, "admin.users.reset_password", "admin-users", strconv.FormatUint(uint64(user.ID), 10), "ok", "password reset"))
	}); err != nil {
		return nil, err
	}
	return user, nil
}

func (u Usecase) ListSessions(ctx context.Context, username string, includeRevoked bool, limit int) ([]domainauth.AuthSession, error) {
	return u.admins.ListSessions(ctx, strings.TrimSpace(username), includeRevoked, limit)
}

func (u Usecase) RevokeSession(ctx context.Context, id uint, input RevokeSessionInput, meta RequestMeta) (*domainauth.AuthSession, error) {
	if !input.Confirm {
		return nil, ErrConfirmationRequired
	}
	var out *domainauth.AuthSession
	if err := u.tx.WithTx(ctx, func(repo AdminRepository) error {
		session, found, err := repo.RevokeSession(ctx, id, time.Now(), meta.Actor, strings.TrimSpace(input.Reason))
		if err != nil {
			return err
		}
		if !found {
			return ErrSessionNotFound
		}
		out = session
		return repo.CreateAuditEvent(ctx, auditEvent(meta, "admin.sessions.revoke", "auth-sessions", strconv.FormatUint(uint64(id), 10), "ok", firstNonEmpty(input.Reason, "session revoked")))
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func (u Usecase) ListAuditEvents(ctx context.Context, limit int) ([]domainauth.AuditEvent, error) {
	return u.admins.ListAuditEvents(ctx, limit)
}

func (u Usecase) RecordAudit(ctx context.Context, event domainauth.AuditEvent) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	if event.Outcome == "" {
		event.Outcome = "ok"
	}
	return u.admins.CreateAuditEvent(ctx, &event)
}

func (u Usecase) createTokenAndSession(ctx context.Context, repo AdminRepository, user domainauth.AdminUser, meta RequestMeta) (string, *domainauth.AuthSession, error) {
	tokenID, err := randomTokenID()
	if err != nil {
		return "", nil, err
	}
	role := normalizedRole(user.Role)
	token, err := u.security.CreateAccessToken(user.Username, map[string]any{"jti": tokenID, "role": role, "uid": user.ID})
	if err != nil {
		return "", nil, err
	}
	expiresAt := time.Now()
	if claims, err := u.security.DecodeAccessToken(token); err == nil {
		expiresAt = claimExpiry(claims)
	}
	session := domainauth.AuthSession{
		TokenID: tokenID, Username: user.Username, UserID: user.ID, Role: role, IP: meta.IP, UserAgent: meta.UserAgent, ExpiresAt: expiresAt,
	}
	if err := repo.CreateSession(ctx, &session); err != nil {
		return "", nil, err
	}
	return token, &session, nil
}

func randomTokenID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func claimExpiry(claims jwt.MapClaims) time.Time {
	switch value := claims["exp"].(type) {
	case float64:
		return time.Unix(int64(value), 0)
	case int64:
		return time.Unix(value, 0)
	case json.Number:
		parsed, _ := value.Int64()
		return time.Unix(parsed, 0)
	default:
		return time.Now().Add(time.Hour)
	}
}

func normalizedRole(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		return "admin"
	}
	return role
}

func validRole(role string) bool {
	switch normalizedRole(role) {
	case "owner", "admin", "operator", "viewer":
		return true
	default:
		return false
	}
}

func auditEvent(meta RequestMeta, action string, resourceType string, resourceID string, outcome string, detail string) *domainauth.AuditEvent {
	return &domainauth.AuditEvent{
		Actor: meta.Actor, Action: action, ResourceType: resourceType, ResourceID: resourceID, Outcome: outcome,
		Detail: detail, IP: meta.IP, UserAgent: meta.UserAgent, CreatedAt: time.Now(),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
