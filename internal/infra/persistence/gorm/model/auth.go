package model

import "time"

type AdminUser struct {
	ID           uint   `gorm:"primaryKey"`
	Username     string `gorm:"size:64;uniqueIndex"`
	DisplayName  string `gorm:"size:128"`
	Role         string `gorm:"size:32;index;default:admin"`
	Active       bool   `gorm:"not null;default:true"`
	PasswordHash string `gorm:"size:255"`
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (AdminUser) TableName() string { return "admin_users" }

type AuthSession struct {
	ID           uint   `gorm:"primaryKey"`
	TokenID      string `gorm:"size:64;uniqueIndex"`
	Username     string `gorm:"size:64;index"`
	UserID       uint   `gorm:"index"`
	Role         string `gorm:"size:32;index"`
	IP           string `gorm:"size:128"`
	UserAgent    string `gorm:"size:512"`
	ExpiresAt    time.Time
	RevokedAt    *time.Time
	RevokedBy    string `gorm:"size:64"`
	RevokeReason string `gorm:"size:255"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (AuthSession) TableName() string { return "auth_sessions" }

type AuditEvent struct {
	ID           uint   `gorm:"primaryKey"`
	Actor        string `gorm:"size:64;index"`
	Action       string `gorm:"size:128;index"`
	ResourceType string `gorm:"size:64;index"`
	ResourceID   string `gorm:"size:128;index"`
	Outcome      string `gorm:"size:32;index"`
	Detail       string `gorm:"type:text"`
	IP           string `gorm:"size:128"`
	UserAgent    string `gorm:"size:512"`
	CreatedAt    time.Time
}

func (AuditEvent) TableName() string { return "audit_events" }
