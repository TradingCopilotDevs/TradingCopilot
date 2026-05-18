package meeting

import (
	"context"

	domainai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/ai"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/meeting"
	domainpaper "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/paper"
	domainresearch "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/research"
	persistmodel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/model"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func CreateMeeting(db *gorm.DB, topic string, triggerSource string) (*domainmeeting.Meeting, error) {
	if triggerSource == "" {
		triggerSource = "manual"
	}
	repo := gormrepo.NewMeetingRepository(db)
	teamID := ensureTestResearchTeam(db)
	meeting := domainmeeting.Meeting{ResearchTeamID: teamID, Topic: topic, TriggerSource: triggerSource, Status: domainkernel.MeetingQueued, Tags: JSONList(nil)}
	if err := repo.Create(dbContext(db), &meeting); err != nil {
		return nil, err
	}
	_, err := AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, "Meeting submitted for execution.", map[string]any{"status": "queued"})
	return &meeting, err
}

func ensureTestResearchTeam(db *gorm.DB) uint {
	db = db.Session(&gorm.Session{NewDB: true})
	var team persistmodel.ResearchTeam
	if err := db.First(&team).Error; err == nil && team.ID != 0 {
		return team.ID
	}
	ctx := dbContext(db)
	var account persistmodel.PaperAccount
	if err := db.First(&account).Error; err != nil || account.ID == 0 {
		domainAccount := domainpaper.Account{
			Name:        "Test research team account",
			InitialCash: decimal.RequireFromString("1000000"),
			Cash:        decimal.RequireFromString("1000000"),
			Active:      true,
		}
		_ = gormrepo.NewPaperRepository(db).SaveAccount(ctx, &domainAccount)
		account.ID = domainAccount.ID
	}
	domainTeam := domainresearch.Team{Name: "Test research team", PaperAccountID: account.ID, Active: true}
	_ = gormrepo.NewResearchRepository(db).CreateTeam(ctx, &domainTeam)
	mirrorExistingAgentRoles(ctx, db, domainTeam.ID)
	return domainTeam.ID
}

func installAgentRoleMirror(db *gorm.DB) {
	_ = db.Callback().Create().After("gorm:create").Register("meeting_tests:mirror_agent_role_to_team_role", func(tx *gorm.DB) {
		role, ok := tx.Statement.Dest.(*domainai.AgentRole)
		if !ok || role == nil || role.Key == "" {
			return
		}
		session := tx.Session(&gorm.Session{NewDB: true})
		teamID := ensureTestResearchTeam(session)
		_ = saveMirroredResearchRole(context.Background(), session, teamID, *role)
	})
}

func mirrorExistingAgentRoles(ctx context.Context, db *gorm.DB, teamID uint) {
	var roles []domainai.AgentRole
	if err := db.WithContext(ctx).Find(&roles).Error; err != nil {
		return
	}
	for _, role := range roles {
		_ = saveMirroredResearchRole(ctx, db, teamID, role)
	}
}

func saveMirroredResearchRole(ctx context.Context, db *gorm.DB, teamID uint, role domainai.AgentRole) error {
	repo := gormrepo.NewResearchRepository(db)
	row, found, err := repo.FindRole(ctx, teamID, role.Key)
	if err != nil {
		return err
	}
	if !found || row == nil {
		row = &domainresearch.TeamRole{ResearchTeamID: teamID, Key: role.Key}
	}
	row.Name = role.Name
	row.Responsibility = role.Responsibility
	row.PromptTemplate = role.PromptTemplate
	row.ProviderID = role.ProviderID
	row.Model = role.Model
	row.ToolNames = role.ToolNames
	row.SkillNames = role.SkillNames
	row.Enabled = role.Enabled
	row.SortOrder = role.SortOrder
	if found {
		return repo.SaveRole(ctx, row)
	}
	return repo.CreateRole(ctx, row)
}

func saveTestAgentRole(db *gorm.DB, teamID uint, role *domainai.AgentRole) error {
	if teamID == 0 {
		teamID = ensureTestResearchTeam(db)
	}
	if err := db.Create(role).Error; err != nil {
		return err
	}
	return saveMirroredResearchRole(dbContext(db), db, teamID, *role)
}
