package auth

import "time"

type AdminUser struct {
	ID           uint
	Username     string
	DisplayName  string
	Role         string
	Active       bool
	PasswordHash string
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type AuthSession struct {
	ID           uint
	TokenID      string
	Username     string
	UserID       uint
	Role         string
	IP           string
	UserAgent    string
	ExpiresAt    time.Time
	RevokedAt    *time.Time
	RevokedBy    string
	RevokeReason string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type AuditEvent struct {
	ID           uint
	Actor        string
	Action       string
	ResourceType string
	ResourceID   string
	Outcome      string
	Detail       string
	IP           string
	UserAgent    string
	CreatedAt    time.Time
}
