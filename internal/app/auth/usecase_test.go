package auth

import (
	"context"
	"errors"
	domainauth "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/auth"
	"testing"
	"time"

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
		"admin": {ID: 1, Username: "admin", Role: "owner", Active: true, PasswordHash: "hash:password"},
	}}
	uc := NewUsecase(repo, fakeSecurity{}, &fakeAdminTransactor{repo: repo})

	if _, err := uc.Bootstrap(context.Background(), "admin2", "password"); !errors.Is(err, ErrAdminExists) {
		t.Fatalf("expected ErrAdminExists, got %v", err)
	}
	if _, err := uc.Login(context.Background(), "admin", "bad"); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("expected ErrInvalidCredential, got %v", err)
	}
}

func TestUsecaseLogoutRevokesCurrentSession(t *testing.T) {
	session := domainauth.AuthSession{ID: 7, TokenID: "token-id", Username: "admin", UserID: 1, Role: "owner", ExpiresAt: time.Now().Add(time.Hour)}
	repo := &fakeAdminRepo{
		users:    map[string]domainauth.AdminUser{"admin": {ID: 1, Username: "admin", Role: "owner", Active: true, PasswordHash: "hash:password"}},
		sessions: map[string]domainauth.AuthSession{"token-id": session},
	}
	uc := NewUsecase(repo, fakeSecurity{}, &fakeAdminTransactor{repo: repo})

	row, err := uc.Logout(context.Background(), Principal{Username: "admin", UserID: 1, Role: "owner", SessionID: 7}, RequestMeta{Actor: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if row.RevokedAt == nil || row.RevokedBy != "admin" || row.RevokeReason != "self logout" {
		t.Fatalf("session was not revoked as logout: %+v", row)
	}
	if len(repo.audits) != 1 || repo.audits[0].Action != "auth.logout" {
		t.Fatalf("missing logout audit event: %+v", repo.audits)
	}
}

type fakeAdminRepo struct {
	users    map[string]domainauth.AdminUser
	sessions map[string]domainauth.AuthSession
	audits   []domainauth.AuditEvent
	nextID   uint
}

func (r *fakeAdminRepo) CountAdmins(context.Context) (int64, error) {
	return int64(len(r.users)), nil
}

func (r *fakeAdminRepo) CreateAdmin(_ context.Context, user *domainauth.AdminUser) error {
	if r.users == nil {
		r.users = map[string]domainauth.AdminUser{}
	}
	r.nextID++
	if user.ID == 0 {
		user.ID = r.nextID
	}
	r.users[user.Username] = *user
	return nil
}

func (r *fakeAdminRepo) ListAdmins(context.Context) ([]domainauth.AdminUser, error) {
	out := make([]domainauth.AdminUser, 0, len(r.users))
	for _, user := range r.users {
		out = append(out, user)
	}
	return out, nil
}

func (r *fakeAdminRepo) FindAdminByID(_ context.Context, id uint) (*domainauth.AdminUser, error) {
	for _, user := range r.users {
		if user.ID == id {
			return &user, nil
		}
	}
	return nil, errors.New("not found")
}

func (r *fakeAdminRepo) FindAdminByUsername(_ context.Context, username string) (*domainauth.AdminUser, error) {
	user, ok := r.users[username]
	if !ok {
		return nil, errors.New("not found")
	}
	return &user, nil
}

func (r *fakeAdminRepo) UpdateAdmin(_ context.Context, user *domainauth.AdminUser) error {
	if r.users == nil {
		r.users = map[string]domainauth.AdminUser{}
	}
	r.users[user.Username] = *user
	return nil
}

func (r *fakeAdminRepo) CreateSession(_ context.Context, session *domainauth.AuthSession) error {
	if r.sessions == nil {
		r.sessions = map[string]domainauth.AuthSession{}
	}
	r.nextID++
	session.ID = r.nextID
	r.sessions[session.TokenID] = *session
	return nil
}

func (r *fakeAdminRepo) FindSessionByTokenID(_ context.Context, tokenID string) (*domainauth.AuthSession, error) {
	session, ok := r.sessions[tokenID]
	if !ok {
		return nil, errors.New("not found")
	}
	return &session, nil
}

func (r *fakeAdminRepo) ListSessions(context.Context, string, bool, int) ([]domainauth.AuthSession, error) {
	out := make([]domainauth.AuthSession, 0, len(r.sessions))
	for _, session := range r.sessions {
		out = append(out, session)
	}
	return out, nil
}

func (r *fakeAdminRepo) RevokeSession(_ context.Context, id uint, revokedAt time.Time, revokedBy string, reason string) (*domainauth.AuthSession, bool, error) {
	for tokenID, session := range r.sessions {
		if session.ID != id {
			continue
		}
		session.RevokedAt = &revokedAt
		session.RevokedBy = revokedBy
		session.RevokeReason = reason
		r.sessions[tokenID] = session
		return &session, true, nil
	}
	return nil, false, nil
}

func (r *fakeAdminRepo) CreateAuditEvent(_ context.Context, event *domainauth.AuditEvent) error {
	r.nextID++
	event.ID = r.nextID
	r.audits = append(r.audits, *event)
	return nil
}

func (r *fakeAdminRepo) ListAuditEvents(context.Context, int) ([]domainauth.AuditEvent, error) {
	return r.audits, nil
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
