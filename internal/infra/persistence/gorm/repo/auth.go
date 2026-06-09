package repo

import (
	"context"
	"errors"
	domainauth "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/auth"
	"time"

	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"gorm.io/gorm"
)

type AuthRepository struct {
	db *gorm.DB
}

func NewAuthRepository(db *gorm.DB) AuthRepository {
	return AuthRepository{db: db}
}

func (r AuthRepository) CountAdmins(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&persistmodel.AdminUser{}).Count(&count).Error
	return count, err
}

func (r AuthRepository) CreateAdmin(ctx context.Context, user *domainauth.AdminUser) error {
	row := adminUserToModel(*user)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*user = adminUserFromModel(row)
	return nil
}

func (r AuthRepository) ListAdmins(ctx context.Context) ([]domainauth.AdminUser, error) {
	var rows []persistmodel.AdminUser
	if err := r.db.WithContext(ctx).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return adminUsersFromModel(rows), nil
}

func (r AuthRepository) FindAdminByID(ctx context.Context, id uint) (*domainauth.AdminUser, error) {
	var row persistmodel.AdminUser
	if err := r.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return nil, err
	}
	user := adminUserFromModel(row)
	return &user, nil
}

func (r AuthRepository) FindAdminByUsername(ctx context.Context, username string) (*domainauth.AdminUser, error) {
	var row persistmodel.AdminUser
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&row).Error; err != nil {
		return nil, err
	}
	user := adminUserFromModel(row)
	return &user, nil
}

func (r AuthRepository) UpdateAdmin(ctx context.Context, user *domainauth.AdminUser) error {
	row := adminUserToModel(*user)
	if err := r.db.WithContext(ctx).Save(&row).Error; err != nil {
		return err
	}
	*user = adminUserFromModel(row)
	return nil
}

func (r AuthRepository) CreateSession(ctx context.Context, session *domainauth.AuthSession) error {
	row := authSessionToModel(*session)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*session = authSessionFromModel(row)
	return nil
}

func (r AuthRepository) FindSessionByTokenID(ctx context.Context, tokenID string) (*domainauth.AuthSession, error) {
	var row persistmodel.AuthSession
	if err := r.db.WithContext(ctx).Where("token_id = ?", tokenID).First(&row).Error; err != nil {
		return nil, err
	}
	session := authSessionFromModel(row)
	return &session, nil
}

func (r AuthRepository) ListSessions(ctx context.Context, username string, includeRevoked bool, limit int) ([]domainauth.AuthSession, error) {
	var rows []persistmodel.AuthSession
	query := r.db.WithContext(ctx).Order("created_at desc, id desc")
	if username != "" {
		query = query.Where("username = ?", username)
	}
	if !includeRevoked {
		query = query.Where("revoked_at IS NULL")
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if err := query.Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return authSessionsFromModel(rows), nil
}

func (r AuthRepository) RevokeSession(ctx context.Context, id uint, revokedAt time.Time, revokedBy string, reason string) (*domainauth.AuthSession, bool, error) {
	var row persistmodel.AuthSession
	if err := r.db.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	row.RevokedAt = &revokedAt
	row.RevokedBy = revokedBy
	row.RevokeReason = reason
	if err := r.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, false, err
	}
	session := authSessionFromModel(row)
	return &session, true, nil
}

func (r AuthRepository) CreateAuditEvent(ctx context.Context, event *domainauth.AuditEvent) error {
	row := auditEventToModel(*event)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*event = auditEventFromModel(row)
	return nil
}

func (r AuthRepository) ListAuditEvents(ctx context.Context, limit int) ([]domainauth.AuditEvent, error) {
	var rows []persistmodel.AuditEvent
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if err := r.db.WithContext(ctx).Order("created_at desc, id desc").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return auditEventsFromModel(rows), nil
}

func adminUsersFromModel(rows []persistmodel.AdminUser) []domainauth.AdminUser {
	out := make([]domainauth.AdminUser, 0, len(rows))
	for _, row := range rows {
		out = append(out, adminUserFromModel(row))
	}
	return out
}

func adminUserFromModel(row persistmodel.AdminUser) domainauth.AdminUser {
	return domainauth.AdminUser{
		ID:           row.ID,
		Username:     row.Username,
		DisplayName:  row.DisplayName,
		Role:         row.Role,
		Active:       row.Active,
		PasswordHash: row.PasswordHash,
		LastLoginAt:  row.LastLoginAt,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

func adminUserToModel(row domainauth.AdminUser) persistmodel.AdminUser {
	return persistmodel.AdminUser{
		ID:           row.ID,
		Username:     row.Username,
		DisplayName:  row.DisplayName,
		Role:         row.Role,
		Active:       row.Active,
		PasswordHash: row.PasswordHash,
		LastLoginAt:  row.LastLoginAt,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

func authSessionsFromModel(rows []persistmodel.AuthSession) []domainauth.AuthSession {
	out := make([]domainauth.AuthSession, 0, len(rows))
	for _, row := range rows {
		out = append(out, authSessionFromModel(row))
	}
	return out
}

func authSessionFromModel(row persistmodel.AuthSession) domainauth.AuthSession {
	return domainauth.AuthSession{
		ID:           row.ID,
		TokenID:      row.TokenID,
		Username:     row.Username,
		UserID:       row.UserID,
		Role:         row.Role,
		IP:           row.IP,
		UserAgent:    row.UserAgent,
		ExpiresAt:    row.ExpiresAt,
		RevokedAt:    row.RevokedAt,
		RevokedBy:    row.RevokedBy,
		RevokeReason: row.RevokeReason,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

func authSessionToModel(row domainauth.AuthSession) persistmodel.AuthSession {
	return persistmodel.AuthSession{
		ID:           row.ID,
		TokenID:      row.TokenID,
		Username:     row.Username,
		UserID:       row.UserID,
		Role:         row.Role,
		IP:           row.IP,
		UserAgent:    row.UserAgent,
		ExpiresAt:    row.ExpiresAt,
		RevokedAt:    row.RevokedAt,
		RevokedBy:    row.RevokedBy,
		RevokeReason: row.RevokeReason,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

func auditEventsFromModel(rows []persistmodel.AuditEvent) []domainauth.AuditEvent {
	out := make([]domainauth.AuditEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, auditEventFromModel(row))
	}
	return out
}

func auditEventFromModel(row persistmodel.AuditEvent) domainauth.AuditEvent {
	return domainauth.AuditEvent{
		ID:           row.ID,
		Actor:        row.Actor,
		Action:       row.Action,
		ResourceType: row.ResourceType,
		ResourceID:   row.ResourceID,
		Outcome:      row.Outcome,
		Detail:       row.Detail,
		IP:           row.IP,
		UserAgent:    row.UserAgent,
		CreatedAt:    row.CreatedAt,
	}
}

func auditEventToModel(row domainauth.AuditEvent) persistmodel.AuditEvent {
	return persistmodel.AuditEvent{
		ID:           row.ID,
		Actor:        row.Actor,
		Action:       row.Action,
		ResourceType: row.ResourceType,
		ResourceID:   row.ResourceID,
		Outcome:      row.Outcome,
		Detail:       row.Detail,
		IP:           row.IP,
		UserAgent:    row.UserAgent,
		CreatedAt:    row.CreatedAt,
	}
}
