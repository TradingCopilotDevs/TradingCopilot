package repo

import (
	"context"
	"errors"

	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainresearch "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/research"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type ResearchRepository struct {
	db *gorm.DB
}

func NewResearchRepository(db *gorm.DB) ResearchRepository {
	return ResearchRepository{db: db}
}

func (r ResearchRepository) ListTeams(ctx context.Context) ([]domainresearch.Team, error) {
	var rows []persistmodel.ResearchTeam
	if err := r.db.WithContext(ctx).Order("active desc, name, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return researchTeamsToDomain(rows), nil
}

func (r ResearchRepository) FindTeam(ctx context.Context, id uint) (*domainresearch.Team, bool, error) {
	var row persistmodel.ResearchTeam
	err := r.db.WithContext(ctx).First(&row, id).Error
	if err == nil {
		out := researchTeamFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r ResearchRepository) FindTeamByPaperAccount(ctx context.Context, paperAccountID uint) (*domainresearch.Team, bool, error) {
	if paperAccountID == 0 {
		return nil, false, nil
	}
	var row persistmodel.ResearchTeam
	err := r.db.WithContext(ctx).Where("paper_account_id = ?", paperAccountID).First(&row).Error
	if err == nil {
		out := researchTeamFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r ResearchRepository) PaperAccountExists(ctx context.Context, id uint) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&persistmodel.PaperAccount{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r ResearchRepository) CreateTeam(ctx context.Context, row *domainresearch.Team) error {
	modelRow := researchTeamToModel(*row)
	if err := r.db.WithContext(ctx).Create(&modelRow).Error; err != nil {
		return err
	}
	*row = researchTeamFromModel(modelRow)
	return nil
}

func (r ResearchRepository) SaveTeam(ctx context.Context, row *domainresearch.Team) error {
	modelRow := researchTeamToModel(*row)
	if err := r.db.WithContext(ctx).Save(&modelRow).Error; err != nil {
		return err
	}
	*row = researchTeamFromModel(modelRow)
	return nil
}

func (r ResearchRepository) DeleteTeamGraph(ctx context.Context, id uint) error {
	db := r.db.WithContext(ctx)
	var meetingIDs []uint
	if err := db.Model(&persistmodel.Meeting{}).Where("research_team_id = ?", id).Pluck("id", &meetingIDs).Error; err != nil {
		return err
	}
	if len(meetingIDs) > 0 {
		if err := db.Where("meeting_id IN ?", meetingIDs).Delete(&persistmodel.WakePlan{}).Error; err != nil {
			return err
		}
		if err := db.Where("meeting_id IN ?", meetingIDs).Delete(&persistmodel.ToolCallLog{}).Error; err != nil {
			return err
		}
		if err := db.Where("source_meeting_id IN ?", meetingIDs).Delete(&persistmodel.MeetingReference{}).Error; err != nil {
			return err
		}
		if err := db.Model(&persistmodel.MeetingReference{}).Where("target_meeting_id IN ?", meetingIDs).Updates(map[string]any{"target_meeting_id": nil, "target_deleted": true}).Error; err != nil {
			return err
		}
		if err := db.Model(&persistmodel.PaperOrder{}).Where("meeting_id IN ?", meetingIDs).Updates(map[string]any{"meeting_id": nil, "source_meeting_event_id": nil}).Error; err != nil {
			return err
		}
		if err := db.Where("meeting_id IN ?", meetingIDs).Delete(&persistmodel.MeetingEvent{}).Error; err != nil {
			return err
		}
	}
	if err := db.Where("research_team_id = ?", id).Delete(&persistmodel.WakePlan{}).Error; err != nil {
		return err
	}
	if err := db.Where("research_team_id = ?", id).Delete(&persistmodel.WatchlistItem{}).Error; err != nil {
		return err
	}
	if err := db.Where("research_team_id = ?", id).Delete(&persistmodel.ResearchTeamRole{}).Error; err != nil {
		return err
	}
	if len(meetingIDs) > 0 {
		if err := db.Delete(&persistmodel.Meeting{}, meetingIDs).Error; err != nil {
			return err
		}
	}
	return db.Delete(&persistmodel.ResearchTeam{}, id).Error
}

func (r ResearchRepository) CountSubscriptionBindingsByTeam(ctx context.Context, teamID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&persistmodel.MessageSubscriptionResearchTeam{}).
		Where("research_team_id = ?", teamID).
		Count(&count).Error
	return count, err
}

func (r ResearchRepository) ListRoles(ctx context.Context, teamID uint) ([]domainresearch.TeamRole, error) {
	var rows []persistmodel.ResearchTeamRole
	if err := r.db.WithContext(ctx).Where("research_team_id = ?", teamID).Order("sort_order, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return researchTeamRolesToDomain(rows), nil
}

func (r ResearchRepository) FindRole(ctx context.Context, teamID uint, key string) (*domainresearch.TeamRole, bool, error) {
	var row persistmodel.ResearchTeamRole
	err := r.db.WithContext(ctx).Where("research_team_id = ? AND key = ?", teamID, key).First(&row).Error
	if err == nil {
		out := researchTeamRoleFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r ResearchRepository) CreateRole(ctx context.Context, role *domainresearch.TeamRole) error {
	row := researchTeamRoleToModel(*role)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*role = researchTeamRoleFromModel(row)
	return nil
}

func (r ResearchRepository) SaveRole(ctx context.Context, role *domainresearch.TeamRole) error {
	row := researchTeamRoleToModel(*role)
	if err := r.db.WithContext(ctx).Save(&row).Error; err != nil {
		return err
	}
	*role = researchTeamRoleFromModel(row)
	return nil
}

func (r ResearchRepository) DeleteRole(ctx context.Context, teamID uint, key string) error {
	return r.db.WithContext(ctx).Delete(&persistmodel.ResearchTeamRole{}, "research_team_id = ? AND key = ?", teamID, key).Error
}

func (r ResearchRepository) DeleteRoles(ctx context.Context, teamID uint) error {
	return r.db.WithContext(ctx).Delete(&persistmodel.ResearchTeamRole{}, "research_team_id = ?", teamID).Error
}

func researchTeamsToDomain(rows []persistmodel.ResearchTeam) []domainresearch.Team {
	out := make([]domainresearch.Team, 0, len(rows))
	for _, row := range rows {
		out = append(out, researchTeamFromModel(row))
	}
	return out
}

func researchTeamFromModel(row persistmodel.ResearchTeam) domainresearch.Team {
	paperAccountID := uint(0)
	if row.PaperAccountID != nil {
		paperAccountID = *row.PaperAccountID
	}
	assetClass := row.AssetClass
	if assetClass == "" {
		assetClass = "a_share"
	}
	return domainresearch.Team{
		ID:             row.ID,
		Name:           row.Name,
		Description:    row.Description,
		PaperAccountID: paperAccountID,
		AssetClass:     assetClass,
		Active:         row.Active,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func researchTeamToModel(row domainresearch.Team) persistmodel.ResearchTeam {
	var paperAccountID *uint
	if row.PaperAccountID != 0 {
		value := row.PaperAccountID
		paperAccountID = &value
	}
	assetClass := row.AssetClass
	if assetClass == "" {
		assetClass = "a_share"
	}
	return persistmodel.ResearchTeam{
		ID:             row.ID,
		Name:           row.Name,
		Description:    row.Description,
		PaperAccountID: paperAccountID,
		AssetClass:     assetClass,
		Active:         row.Active,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func researchTeamRolesToDomain(rows []persistmodel.ResearchTeamRole) []domainresearch.TeamRole {
	out := make([]domainresearch.TeamRole, 0, len(rows))
	for _, row := range rows {
		out = append(out, researchTeamRoleFromModel(row))
	}
	return out
}

func researchTeamRoleFromModel(row persistmodel.ResearchTeamRole) domainresearch.TeamRole {
	return domainresearch.TeamRole{
		ID:             row.ID,
		ResearchTeamID: row.ResearchTeamID,
		Key:            row.Key,
		Name:           row.Name,
		Responsibility: row.Responsibility,
		PromptTemplate: row.PromptTemplate,
		ProviderID:     row.ProviderID,
		Model:          row.Model,
		ToolNames:      domainkernel.JSON(row.ToolNames),
		SkillNames:     domainkernel.JSON(row.SkillNames),
		Enabled:        row.Enabled,
		SortOrder:      row.SortOrder,
	}
}

func researchTeamRoleToModel(row domainresearch.TeamRole) persistmodel.ResearchTeamRole {
	return persistmodel.ResearchTeamRole{
		ID:             row.ID,
		ResearchTeamID: row.ResearchTeamID,
		Key:            row.Key,
		Name:           row.Name,
		Responsibility: row.Responsibility,
		PromptTemplate: row.PromptTemplate,
		ProviderID:     row.ProviderID,
		Model:          row.Model,
		ToolNames:      datatypes.JSON(row.ToolNames),
		SkillNames:     datatypes.JSON(row.SkillNames),
		Enabled:        row.Enabled,
		SortOrder:      row.SortOrder,
	}
}
