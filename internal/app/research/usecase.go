package research

import (
	"context"
	"errors"
	"strings"

	appai "github.com/TradingCopilotDevs/TradingCopilot/internal/app/ai"
	domainresearch "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/research"
)

const (
	DefaultTeamName        = "默认投研团队"
	DefaultTeamDescription = "系统初始化创建的默认投研会议团队。"

	AssetClassAShare           = "a_share"
	AssetClassPredictionMarket = "prediction_market"
	AssetClassMixed            = "mixed"

	DefaultPredictionTeamName        = "默认预测市场投研团队"
	DefaultPredictionTeamDescription = "系统初始化创建的 Polymarket 预测市场投研会议团队。"

	predictionRolePromptBoundaryMarker = "预测市场角色边界"
	predictionRolePromptBoundary       = `预测市场角色边界：本角色只能研究 Polymarket 等预测市场的公开事件、市场问题、结算规则、赔率/盘口、流动性和证据缺口。不得输出 A 股行业、A 股个股、股票代码、模拟盘订单、证券自选池或真实交易动作；如果原始角色说明与本边界冲突，以本边界为准。`
)

var (
	ErrTeamNotFound         = errors.New("research team not found")
	ErrPaperAccountRequired = errors.New("paper account is required")
	ErrPaperAccountNotFound = errors.New("paper account not found")
	ErrPaperAccountBound    = errors.New("paper account is already bound to another research team")
	ErrSubscriptionBound    = errors.New("research team still has message subscription bindings")
	ErrTeamNameRequired     = errors.New("research team name is required")
	ErrRoleKeyRequired      = errors.New("research team role key is required")
	ErrRoleNameRequired     = errors.New("research team role name is required")
	ErrRolePromptRequired   = errors.New("research team role prompt template is required")
)

var predictionRoleAllowedSkills = map[string]struct{}{
	"news-source-verification":   {},
	"web-research":               {},
	"meeting-moderation":         {},
	"cross-examination":          {},
	"evidence-synthesis":         {},
	"prediction-market-research": {},
	"odds-market-analysis":       {},
	"market-resolution-risk":     {},
	"wake-plan-design":           {},
}

type Usecase struct {
	repo Repository
	tx   Transactor
}

func NewUsecase(repo Repository, tx Transactor) Usecase {
	return Usecase{repo: repo, tx: tx}
}

type Repository interface {
	ListTeams(ctx context.Context) ([]domainresearch.Team, error)
	FindTeam(ctx context.Context, id uint) (*domainresearch.Team, bool, error)
	FindTeamByPaperAccount(ctx context.Context, paperAccountID uint) (*domainresearch.Team, bool, error)
	PaperAccountExists(ctx context.Context, id uint) (bool, error)
	CreateTeam(ctx context.Context, row *domainresearch.Team) error
	SaveTeam(ctx context.Context, row *domainresearch.Team) error
	DeleteTeamGraph(ctx context.Context, id uint) error
	CountSubscriptionBindingsByTeam(ctx context.Context, teamID uint) (int64, error)
	ListRoles(ctx context.Context, teamID uint) ([]domainresearch.TeamRole, error)
	FindRole(ctx context.Context, teamID uint, key string) (*domainresearch.TeamRole, bool, error)
	CreateRole(ctx context.Context, role *domainresearch.TeamRole) error
	SaveRole(ctx context.Context, role *domainresearch.TeamRole) error
	DeleteRole(ctx context.Context, teamID uint, key string) error
	DeleteRoles(ctx context.Context, teamID uint) error
}

type Transactor interface {
	WithTx(ctx context.Context, fn func(Repository) error) error
}

type TeamInput struct {
	Name                string
	Description         string
	PaperAccountID      uint
	AssetClass          string
	Active              bool
	CopyRolesFromTeamID *uint
}

type RoleInput struct {
	Key            string
	Name           string
	Responsibility string
	PromptTemplate string
	ProviderID     *uint
	Model          *string
	ToolNames      []string
	SkillNames     []string
	Enabled        bool
	SortOrder      int
}

func (u Usecase) ListTeams(ctx context.Context) ([]domainresearch.Team, error) {
	return u.repo.ListTeams(ctx)
}

func (u Usecase) EnsureDefaultTeam(ctx context.Context, paperAccountID uint) (*domainresearch.Team, bool, error) {
	var row *domainresearch.Team
	created := false
	if err := u.withTx(ctx, func(repo Repository) error {
		teams, err := repo.ListTeams(ctx)
		if err != nil {
			return err
		}
		if len(teams) > 0 {
			existing := teams[0]
			row = &existing
			return nil
		}
		if err := validatePaperAccountAvailable(ctx, repo, paperAccountID, 0); err != nil {
			return err
		}
		team := domainresearch.Team{
			Name:           DefaultTeamName,
			Description:    DefaultTeamDescription,
			PaperAccountID: paperAccountID,
			AssetClass:     AssetClassAShare,
			Active:         true,
		}
		if err := repo.CreateTeam(ctx, &team); err != nil {
			return err
		}
		for _, seed := range appai.DefaultRoles {
			if err := createDefaultRole(ctx, repo, team.ID, seed); err != nil {
				return err
			}
		}
		row = &team
		created = true
		return nil
	}); err != nil {
		return nil, false, err
	}
	return row, created, nil
}

func (u Usecase) EnsureDefaultPredictionTeam(ctx context.Context) (*domainresearch.Team, bool, error) {
	var row *domainresearch.Team
	created := false
	if err := u.withTx(ctx, func(repo Repository) error {
		teams, err := repo.ListTeams(ctx)
		if err != nil {
			return err
		}
		for _, team := range teams {
			if team.Name != DefaultPredictionTeamName {
				continue
			}
			existing := team
			if existing.AssetClass != AssetClassPredictionMarket || existing.PaperAccountID != 0 {
				existing.AssetClass = AssetClassPredictionMarket
				existing.PaperAccountID = 0
				if err := repo.SaveTeam(ctx, &existing); err != nil {
					return err
				}
			}
			if err := ensurePredictionDefaultRoles(ctx, repo, existing.ID); err != nil {
				return err
			}
			row = &existing
			return nil
		}
		for _, team := range teams {
			if team.AssetClass == AssetClassPredictionMarket {
				existing := team
				if err := sanitizePredictionTeamRoles(ctx, repo, existing.ID); err != nil {
					return err
				}
				row = &existing
				return nil
			}
		}
		team := domainresearch.Team{
			Name:        DefaultPredictionTeamName,
			Description: DefaultPredictionTeamDescription,
			AssetClass:  AssetClassPredictionMarket,
			Active:      true,
		}
		if err := repo.CreateTeam(ctx, &team); err != nil {
			return err
		}
		for _, seed := range PredictionMarketDefaultRoles {
			if err := createDefaultRole(ctx, repo, team.ID, seed); err != nil {
				return err
			}
		}
		row = &team
		created = true
		return nil
	}); err != nil {
		return nil, false, err
	}
	return row, created, nil
}

func (u Usecase) CreateTeam(ctx context.Context, input TeamInput) (*domainresearch.Team, error) {
	row := domainresearch.Team{
		Name:           strings.TrimSpace(input.Name),
		Description:    strings.TrimSpace(input.Description),
		PaperAccountID: input.PaperAccountID,
		AssetClass:     normalizeAssetClass(input.AssetClass),
		Active:         input.Active,
	}
	if row.Name == "" {
		return nil, ErrTeamNameRequired
	}
	if requiresPaperAccount(row.AssetClass) && row.PaperAccountID == 0 {
		return nil, ErrPaperAccountRequired
	}
	if err := u.withTx(ctx, func(repo Repository) error {
		if requiresPaperAccount(row.AssetClass) {
			if err := validatePaperAccountAvailable(ctx, repo, row.PaperAccountID, 0); err != nil {
				return err
			}
		} else if row.PaperAccountID != 0 {
			if err := validatePaperAccountAvailable(ctx, repo, row.PaperAccountID, 0); err != nil {
				return err
			}
		}
		if !validAssetClass(row.AssetClass) {
			return errors.New("asset class must be a_share, prediction_market, or mixed")
		}
		if row.AssetClass == AssetClassPredictionMarket {
			row.PaperAccountID = 0
		}
		if err := repo.CreateTeam(ctx, &row); err != nil {
			return err
		}
		if input.CopyRolesFromTeamID != nil && *input.CopyRolesFromTeamID != 0 {
			sourceRoles, err := repo.ListRoles(ctx, *input.CopyRolesFromTeamID)
			if err != nil {
				return err
			}
			for _, source := range sourceRoles {
				clone := source
				clone.ID = 0
				clone.ResearchTeamID = row.ID
				if row.AssetClass == AssetClassPredictionMarket {
					sanitizePredictionRoleCapabilities(&clone)
				}
				if err := repo.CreateRole(ctx, &clone); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &row, nil
}

func (u Usecase) UpdateTeam(ctx context.Context, id uint, input TeamInput, fields map[string]bool) (*domainresearch.Team, bool, error) {
	var row *domainresearch.Team
	found := false
	if err := u.withTx(ctx, func(repo Repository) error {
		var err error
		row, found, err = repo.FindTeam(ctx, id)
		if err != nil || !found {
			return err
		}
		if fields["name"] {
			row.Name = strings.TrimSpace(input.Name)
		}
		if fields["description"] {
			row.Description = strings.TrimSpace(input.Description)
		}
		if fields["paperAccountId"] {
			if requiresPaperAccount(firstNonEmpty(input.AssetClass, row.AssetClass)) || input.PaperAccountID != 0 {
				if err := validatePaperAccountAvailable(ctx, repo, input.PaperAccountID, id); err != nil {
					return err
				}
			}
			row.PaperAccountID = input.PaperAccountID
		}
		if fields["assetClass"] {
			row.AssetClass = normalizeAssetClass(input.AssetClass)
			if !validAssetClass(row.AssetClass) {
				return errors.New("asset class must be a_share, prediction_market, or mixed")
			}
			if row.AssetClass == AssetClassPredictionMarket {
				row.PaperAccountID = 0
			}
			if requiresPaperAccount(row.AssetClass) && row.PaperAccountID == 0 {
				return ErrPaperAccountRequired
			}
			if row.PaperAccountID != 0 {
				if err := validatePaperAccountAvailable(ctx, repo, row.PaperAccountID, id); err != nil {
					return err
				}
			}
		} else if requiresPaperAccount(row.AssetClass) && row.PaperAccountID == 0 {
			return ErrPaperAccountRequired
		}
		if fields["paperAccountId"] && row.PaperAccountID != 0 {
			if err := validatePaperAccountAvailable(ctx, repo, row.PaperAccountID, id); err != nil {
				return err
			}
		}
		if fields["active"] {
			row.Active = input.Active
		}
		if row.Name == "" {
			return ErrTeamNameRequired
		}
		if err := repo.SaveTeam(ctx, row); err != nil {
			return err
		}
		if row.AssetClass == AssetClassPredictionMarket {
			return sanitizePredictionTeamRoles(ctx, repo, row.ID)
		}
		return nil
	}); err != nil {
		return nil, found, err
	}
	return row, found, nil
}

func (u Usecase) DeleteTeam(ctx context.Context, id uint) (bool, error) {
	found := false
	if err := u.withTx(ctx, func(repo Repository) error {
		_, ok, err := repo.FindTeam(ctx, id)
		if err != nil || !ok {
			found = ok
			return err
		}
		found = true
		count, err := repo.CountSubscriptionBindingsByTeam(ctx, id)
		if err != nil {
			return err
		}
		if count > 0 {
			return ErrSubscriptionBound
		}
		return repo.DeleteTeamGraph(ctx, id)
	}); err != nil {
		return found, err
	}
	return found, nil
}

func (u Usecase) ListRoles(ctx context.Context, teamID uint) ([]domainresearch.TeamRole, error) {
	return u.repo.ListRoles(ctx, teamID)
}

func (u Usecase) UpsertRole(ctx context.Context, teamID uint, key string, input RoleInput) (*domainresearch.TeamRole, error) {
	key = strings.TrimSpace(firstNonEmpty(key, input.Key))
	if key == "" {
		return nil, ErrRoleKeyRequired
	}
	var row *domainresearch.TeamRole
	if err := u.withTx(ctx, func(repo Repository) error {
		team, found, err := repo.FindTeam(ctx, teamID)
		if err != nil || !found {
			if err != nil {
				return err
			}
			return ErrTeamNotFound
		}
		foundRole, found, err := repo.FindRole(ctx, teamID, key)
		if err != nil {
			return err
		}
		if found {
			row = foundRole
		} else {
			row = &domainresearch.TeamRole{ResearchTeamID: teamID, Key: key}
		}
		applyRoleInput(row, input)
		if team.AssetClass == AssetClassPredictionMarket {
			sanitizePredictionRoleCapabilities(row)
		}
		if err := validateRole(row); err != nil {
			return err
		}
		if found {
			return repo.SaveRole(ctx, row)
		}
		return repo.CreateRole(ctx, row)
	}); err != nil {
		return nil, err
	}
	return row, nil
}

func (u Usecase) DeleteRole(ctx context.Context, teamID uint, key string) error {
	return u.withTx(ctx, func(repo Repository) error {
		return repo.DeleteRole(ctx, teamID, key)
	})
}

func (u Usecase) ApplyDefaultRoles(ctx context.Context, teamID uint) (int, error) {
	updated := 0
	if err := u.withTx(ctx, func(repo Repository) error {
		team, found, err := repo.FindTeam(ctx, teamID)
		if err != nil || !found {
			if err != nil {
				return err
			}
			return ErrTeamNotFound
		}
		if err := repo.DeleteRoles(ctx, teamID); err != nil {
			return err
		}
		seeds := appai.DefaultRoles
		if team != nil && team.AssetClass == AssetClassPredictionMarket {
			seeds = PredictionMarketDefaultRoles
		}
		for _, seed := range seeds {
			if err := createDefaultRole(ctx, repo, teamID, seed); err != nil {
				return err
			}
			updated++
		}
		return nil
	}); err != nil {
		return 0, err
	}
	return updated, nil
}

func normalizeAssetClass(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return AssetClassAShare
	}
	return value
}

func validAssetClass(value string) bool {
	switch normalizeAssetClass(value) {
	case AssetClassAShare, AssetClassPredictionMarket, AssetClassMixed:
		return true
	default:
		return false
	}
}

func requiresPaperAccount(assetClass string) bool {
	switch normalizeAssetClass(assetClass) {
	case AssetClassPredictionMarket:
		return false
	default:
		return true
	}
}

func createDefaultRole(ctx context.Context, repo Repository, teamID uint, seed appai.RoleSeed) error {
	role := domainresearch.TeamRole{
		ResearchTeamID: teamID,
		Key:            seed.Key,
		Name:           seed.Name,
		Responsibility: seed.Responsibility,
		PromptTemplate: seed.Prompt,
		ToolNames:      jsonList(seed.Tools),
		SkillNames:     jsonList(seed.Skills),
		Enabled:        true,
		SortOrder:      seed.SortOrder,
	}
	return repo.CreateRole(ctx, &role)
}

func ensurePredictionDefaultRoles(ctx context.Context, repo Repository, teamID uint) error {
	roles, err := repo.ListRoles(ctx, teamID)
	if err != nil {
		return err
	}
	byKey := map[string]domainresearch.TeamRole{}
	for _, role := range roles {
		byKey[role.Key] = role
	}
	for _, seed := range PredictionMarketDefaultRoles {
		role, ok := byKey[seed.Key]
		if !ok {
			if err := createDefaultRole(ctx, repo, teamID, seed); err != nil {
				return err
			}
			continue
		}
		applyPredictionDefaultSeed(&role, seed)
		if err := repo.SaveRole(ctx, &role); err != nil {
			return err
		}
	}
	for _, role := range roles {
		if _, ok := predictionDefaultRoleSeed(role.Key); ok {
			continue
		}
		sanitizePredictionRoleCapabilities(&role)
		if err := repo.SaveRole(ctx, &role); err != nil {
			return err
		}
	}
	return nil
}

func applyPredictionDefaultSeed(role *domainresearch.TeamRole, seed appai.RoleSeed) {
	role.Name = seed.Name
	role.Responsibility = seed.Responsibility
	role.PromptTemplate = seed.Prompt
	role.ToolNames = jsonList(seed.Tools)
	role.SkillNames = jsonList(seed.Skills)
	role.SortOrder = seed.SortOrder
}

func predictionDefaultRoleSeed(key string) (appai.RoleSeed, bool) {
	for _, seed := range PredictionMarketDefaultRoles {
		if seed.Key == key {
			return seed, true
		}
	}
	return appai.RoleSeed{}, false
}

func sanitizePredictionTeamRoles(ctx context.Context, repo Repository, teamID uint) error {
	roles, err := repo.ListRoles(ctx, teamID)
	if err != nil {
		return err
	}
	for _, role := range roles {
		sanitizePredictionRoleCapabilities(&role)
		if err := repo.SaveRole(ctx, &role); err != nil {
			return err
		}
	}
	return nil
}

func sanitizePredictionRoleCapabilities(role *domainresearch.TeamRole) {
	role.PromptTemplate = sanitizePredictionRolePrompt(role.PromptTemplate)
	role.ToolNames = jsonList(filterRoleCapabilities(jsonStringList(role.ToolNames), predictionRoleToolAllowed))
	role.SkillNames = jsonList(filterRoleCapabilities(jsonStringList(role.SkillNames), predictionRoleSkillAllowed))
}

func sanitizePredictionRolePrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if strings.Contains(prompt, predictionRolePromptBoundaryMarker) {
		return prompt
	}
	if prompt == "" {
		return predictionRolePromptBoundary
	}
	return predictionRolePromptBoundary + "\n\n原始角色补充说明（仅在不违反上述边界时适用）：\n" + prompt
}

func filterRoleCapabilities(values []string, allowed func(string) bool) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || !allowed(value) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func predictionRoleToolAllowed(name string) bool {
	name = strings.TrimSpace(name)
	return name == "web.search" ||
		name == "meeting.references" ||
		name == "meeting.transcript" ||
		strings.HasPrefix(name, "prediction.")
}

func predictionRoleSkillAllowed(name string) bool {
	_, ok := predictionRoleAllowedSkills[strings.TrimSpace(name)]
	return ok
}

func validatePaperAccountAvailable(ctx context.Context, repo Repository, paperAccountID uint, currentTeamID uint) error {
	if paperAccountID == 0 {
		return ErrPaperAccountRequired
	}
	exists, err := repo.PaperAccountExists(ctx, paperAccountID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrPaperAccountNotFound
	}
	team, found, err := repo.FindTeamByPaperAccount(ctx, paperAccountID)
	if err != nil {
		return err
	}
	if found && team != nil && team.ID != currentTeamID {
		return ErrPaperAccountBound
	}
	return nil
}

func applyRoleInput(role *domainresearch.TeamRole, input RoleInput) {
	role.Name = strings.TrimSpace(input.Name)
	role.Responsibility = strings.TrimSpace(input.Responsibility)
	role.PromptTemplate = strings.TrimSpace(input.PromptTemplate)
	role.ProviderID = input.ProviderID
	role.Model = cleanStringPtr(input.Model)
	role.ToolNames = jsonList(input.ToolNames)
	role.SkillNames = jsonList(input.SkillNames)
	role.Enabled = input.Enabled
	role.SortOrder = input.SortOrder
	if role.SortOrder == 0 {
		role.SortOrder = 100
	}
}

func validateRole(role *domainresearch.TeamRole) error {
	if strings.TrimSpace(role.Key) == "" {
		return ErrRoleKeyRequired
	}
	if strings.TrimSpace(role.Name) == "" {
		return ErrRoleNameRequired
	}
	if strings.TrimSpace(role.PromptTemplate) == "" {
		return ErrRolePromptRequired
	}
	return nil
}

func (u Usecase) withTx(ctx context.Context, fn func(Repository) error) error {
	if u.tx == nil {
		return errors.New("research unit of work is not configured")
	}
	return u.tx.WithTx(ctx, fn)
}

func cleanStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(*value)
	if text == "" {
		return nil
	}
	return &text
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
