package auth

import "time"

type AdminUser struct {
	ID           uint
	Username     string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
