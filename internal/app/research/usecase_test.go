package research

import (
	"context"
	"testing"

	appai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/ai"
	domainresearch "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/research"
)

func TestEnsureDefaultTeamCreatesTeamAndRolesOnlyWhenEmpty(t *testing.T) {
	ctx := context.Background()
	repo := newFakeResearchRepo()
	repo.paperAccounts[10] = true
	usecase := NewUsecase(repo, fakeResearchTx{repo: repo})

	team, created, err := usecase.EnsureDefaultTeam(ctx, 10)
	if err != nil {
		t.Fatalf("EnsureDefaultTeam: %v", err)
	}
	if !created || team == nil || team.Name != DefaultTeamName || team.PaperAccountID != 10 {
		t.Fatalf("unexpected default team result team=%+v created=%v", team, created)
	}
	if len(repo.roles[team.ID]) != len(appai.DefaultRoles) {
		t.Fatalf("default role count = %d, want %d", len(repo.roles[team.ID]), len(appai.DefaultRoles))
	}
	if _, ok := repo.roles[team.ID]["news_filter"]; ok {
		t.Fatal("message filter must not be created as a research-team role")
	}

	repo.roles[team.ID]["moderator"] = withRoleName(repo.roles[team.ID]["moderator"], "Custom Moderator")
	again, createdAgain, err := usecase.EnsureDefaultTeam(ctx, 10)
	if err != nil {
		t.Fatalf("second EnsureDefaultTeam: %v", err)
	}
	if createdAgain || again == nil || again.ID != team.ID {
		t.Fatalf("second ensure should return existing team without creation, team=%+v created=%v", again, createdAgain)
	}
	if repo.roles[team.ID]["moderator"].Name != "Custom Moderator" {
		t.Fatal("second ensure must not rewrite existing team roles")
	}
}

func TestApplyDefaultRolesDeletesAndRebuildsDefaultRoles(t *testing.T) {
	ctx := context.Background()
	repo := newFakeResearchRepo()
	repo.paperAccounts[10] = true
	team := domainresearch.Team{Name: "Team", PaperAccountID: 10, Active: true}
	if err := repo.CreateTeam(ctx, &team); err != nil {
		t.Fatal(err)
	}
	providerID := uint(99)
	model := "custom-model"
	repo.roles[team.ID] = map[string]domainresearch.TeamRole{
		"moderator": {
			ResearchTeamID: team.ID,
			Key:            "moderator",
			Name:           "Custom Moderator",
			Responsibility: "custom",
			PromptTemplate: "custom prompt",
			ProviderID:     &providerID,
			Model:          &model,
			Enabled:        true,
			SortOrder:      99,
		},
		"custom": {
			ResearchTeamID: team.ID,
			Key:            "custom",
			Name:           "Custom Role",
			Responsibility: "custom",
			PromptTemplate: "custom prompt",
			Enabled:        true,
			SortOrder:      100,
		},
	}
	usecase := NewUsecase(repo, fakeResearchTx{repo: repo})

	rebuilt, err := usecase.ApplyDefaultRoles(ctx, team.ID)
	if err != nil {
		t.Fatalf("ApplyDefaultRoles: %v", err)
	}
	if rebuilt != len(appai.DefaultRoles) {
		t.Fatalf("rebuilt roles = %d, want %d", rebuilt, len(appai.DefaultRoles))
	}
	if len(repo.roles[team.ID]) != len(appai.DefaultRoles) {
		t.Fatalf("role count after reset = %d, want %d", len(repo.roles[team.ID]), len(appai.DefaultRoles))
	}
	if _, ok := repo.roles[team.ID]["custom"]; ok {
		t.Fatal("custom role should be removed by default reset")
	}
	moderator := repo.roles[team.ID]["moderator"]
	if moderator.Name == "Custom Moderator" || moderator.PromptTemplate == "custom prompt" {
		t.Fatalf("moderator role was not reset: %+v", moderator)
	}
	if moderator.ProviderID != nil || moderator.Model != nil {
		t.Fatalf("default reset should clear provider/model: %+v", moderator)
	}
}

type fakeResearchRepo struct {
	teams         map[uint]domainresearch.Team
	roles         map[uint]map[string]domainresearch.TeamRole
	paperAccounts map[uint]bool
	nextTeamID    uint
	nextRoleID    uint
}

func newFakeResearchRepo() *fakeResearchRepo {
	return &fakeResearchRepo{
		teams:         map[uint]domainresearch.Team{},
		roles:         map[uint]map[string]domainresearch.TeamRole{},
		paperAccounts: map[uint]bool{},
		nextTeamID:    1,
		nextRoleID:    1,
	}
}

func (r *fakeResearchRepo) ListTeams(context.Context) ([]domainresearch.Team, error) {
	out := make([]domainresearch.Team, 0, len(r.teams))
	for _, row := range r.teams {
		out = append(out, row)
	}
	return out, nil
}

func (r *fakeResearchRepo) FindTeam(_ context.Context, id uint) (*domainresearch.Team, bool, error) {
	row, ok := r.teams[id]
	return &row, ok, nil
}

func (r *fakeResearchRepo) FindTeamByPaperAccount(_ context.Context, paperAccountID uint) (*domainresearch.Team, bool, error) {
	for _, row := range r.teams {
		if row.PaperAccountID == paperAccountID {
			found := row
			return &found, true, nil
		}
	}
	return nil, false, nil
}

func (r *fakeResearchRepo) PaperAccountExists(_ context.Context, id uint) (bool, error) {
	return r.paperAccounts[id], nil
}

func (r *fakeResearchRepo) CreateTeam(_ context.Context, row *domainresearch.Team) error {
	if row.ID == 0 {
		row.ID = r.nextTeamID
		r.nextTeamID++
	}
	r.teams[row.ID] = *row
	return nil
}

func (r *fakeResearchRepo) SaveTeam(_ context.Context, row *domainresearch.Team) error {
	r.teams[row.ID] = *row
	return nil
}

func (r *fakeResearchRepo) DeleteTeamGraph(_ context.Context, id uint) error {
	delete(r.teams, id)
	delete(r.roles, id)
	return nil
}

func (r *fakeResearchRepo) CountSubscriptionBindingsByTeam(context.Context, uint) (int64, error) {
	return 0, nil
}

func (r *fakeResearchRepo) ListRoles(_ context.Context, teamID uint) ([]domainresearch.TeamRole, error) {
	out := []domainresearch.TeamRole{}
	for _, row := range r.roles[teamID] {
		out = append(out, row)
	}
	return out, nil
}

func (r *fakeResearchRepo) FindRole(_ context.Context, teamID uint, key string) (*domainresearch.TeamRole, bool, error) {
	row, ok := r.roles[teamID][key]
	return &row, ok, nil
}

func (r *fakeResearchRepo) CreateRole(_ context.Context, role *domainresearch.TeamRole) error {
	if r.roles[role.ResearchTeamID] == nil {
		r.roles[role.ResearchTeamID] = map[string]domainresearch.TeamRole{}
	}
	if role.ID == 0 {
		role.ID = r.nextRoleID
		r.nextRoleID++
	}
	r.roles[role.ResearchTeamID][role.Key] = *role
	return nil
}

func (r *fakeResearchRepo) SaveRole(_ context.Context, role *domainresearch.TeamRole) error {
	if r.roles[role.ResearchTeamID] == nil {
		r.roles[role.ResearchTeamID] = map[string]domainresearch.TeamRole{}
	}
	r.roles[role.ResearchTeamID][role.Key] = *role
	return nil
}

func (r *fakeResearchRepo) DeleteRole(_ context.Context, teamID uint, key string) error {
	delete(r.roles[teamID], key)
	return nil
}

func (r *fakeResearchRepo) DeleteRoles(_ context.Context, teamID uint) error {
	delete(r.roles, teamID)
	return nil
}

type fakeResearchTx struct {
	repo *fakeResearchRepo
}

func (tx fakeResearchTx) WithTx(ctx context.Context, fn func(Repository) error) error {
	return fn(tx.repo)
}

func withRoleName(role domainresearch.TeamRole, name string) domainresearch.TeamRole {
	role.Name = name
	return role
}
