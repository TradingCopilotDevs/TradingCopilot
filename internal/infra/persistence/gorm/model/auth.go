package model

import "time"

type AdminUser struct {
	ID           uint   `gorm:"primaryKey"`
	Username     string `gorm:"size:64;uniqueIndex"`
	PasswordHash string `gorm:"size:255"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (AdminUser) TableName() string { return "admin_users" }
