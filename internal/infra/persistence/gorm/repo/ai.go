package repo

import (
	"context"
	"encoding/json"
	"errors"
	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"

	appai "github.com/TradingCopilotDevs/TradingCopilot/internal/app/ai"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type AIRepository struct {
	db *gorm.DB
}

func NewAIRepository(db *gorm.DB) AIRepository {
	return AIRepository{db: db}
}

func (r AIRepository) ListProviders(ctx context.Context) ([]domainai.Provider, error) {
	var rows []persistmodel.AiProvider
	if err := r.db.WithContext(ctx).Preload("APIKeySecret").Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	return aiProvidersToDomain(rows), nil
}

func (r AIRepository) CreateProvider(ctx context.Context, provider *domainai.Provider) error {
	row := aiProviderToModel(*provider)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*provider = aiProviderFromModel(row)
	return nil
}

func (r AIRepository) FindProvider(ctx context.Context, id uint) (*domainai.Provider, bool, error) {
	var row persistmodel.AiProvider
	err := r.db.WithContext(ctx).Preload("APIKeySecret").First(&row, id).Error
	if err == nil {
		out := aiProviderFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r AIRepository) SaveProvider(ctx context.Context, provider *domainai.Provider) error {
	row := aiProviderToModel(*provider)
	if err := r.db.WithContext(ctx).Save(&row).Error; err != nil {
		return err
	}
	*provider = aiProviderFromModel(row)
	return nil
}

func (r AIRepository) ListProviderModels(ctx context.Context, providerID uint) ([]domainai.ProviderModel, error) {
	var rows []persistmodel.AiProviderModel
	if err := r.db.WithContext(ctx).Where("provider_id = ? AND enabled = ?", providerID, true).Order("model_id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return aiProviderModelsToDomain(rows), nil
}

func (r AIRepository) FindProviderModel(ctx context.Context, providerID uint, modelID string) (*domainai.ProviderModel, bool, error) {
	var row persistmodel.AiProviderModel
	err := r.db.WithContext(ctx).Where("provider_id = ? AND model_id = ?", providerID, modelID).First(&row).Error
	if err == nil {
		out := aiProviderModelFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r AIRepository) SaveProviderModel(ctx context.Context, model *domainai.ProviderModel) error {
	row := aiProviderModelToModel(*model)
	if err := r.db.WithContext(ctx).Save(&row).Error; err != nil {
		return err
	}
	*model = aiProviderModelFromModel(row)
	return nil
}

func (r AIRepository) ListRoles(ctx context.Context) ([]domainai.AgentRole, error) {
	var rows []persistmodel.AgentRole
	if err := r.db.WithContext(ctx).Order("sort_order, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return agentRolesToDomain(rows), nil
}

func (r AIRepository) FindRole(ctx context.Context, key string) (*domainai.AgentRole, bool, error) {
	var row persistmodel.AgentRole
	err := r.db.WithContext(ctx).Where("key = ?", key).First(&row).Error
	if err == nil {
		out := agentRoleFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r AIRepository) SaveRole(ctx context.Context, role *domainai.AgentRole) error {
	row := agentRoleToModel(*role)
	if err := r.db.WithContext(ctx).Save(&row).Error; err != nil {
		return err
	}
	*role = agentRoleFromModel(row)
	return nil
}

func (r AIRepository) UpsertSecret(ctx context.Context, kind domainkernel.SecretKind, name string, encryptedValue string) (*domainsettings.Secret, error) {
	var row persistmodel.Secret
	result := r.db.WithContext(ctx).Where("kind = ? AND name = ?", kind, name).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		row = persistmodel.Secret{Kind: kind, Name: name, EncryptedValue: encryptedValue}
		if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
			return nil, err
		}
		out := secretFromModel(row)
		return &out, nil
	}
	row.EncryptedValue = encryptedValue
	if err := r.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	out := secretFromModel(row)
	return &out, nil
}

func (r AIRepository) SeedDefaultRoles(ctx context.Context, overwrite bool) (int, error) {
	if err := r.seedDefaultRoles(ctx, overwrite); err != nil {
		return 0, err
	}
	return len(appai.DefaultRoles), nil
}

func (r AIRepository) seedDefaultRoles(ctx context.Context, overwrite bool) error {
	db := r.db.WithContext(ctx)
	for _, seed := range appai.DefaultRoles {
		var row persistmodel.AgentRole
		err := db.Where("key = ?", seed.Key).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row = persistmodel.AgentRole{
				Key:            seed.Key,
				Name:           seed.Name,
				Responsibility: seed.Responsibility,
				PromptTemplate: seed.Prompt,
				ToolNames:      jsonList(seed.Tools),
				SkillNames:     jsonList(seed.Skills),
				Enabled:        true,
				SortOrder:      seed.SortOrder,
			}
			if err := db.Create(&row).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		applyRoleSeed(&row, seed, overwrite)
		if err := db.Save(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func applyRoleSeed(role *persistmodel.AgentRole, seed appai.RoleSeed, overwrite bool) {
	if overwrite || role.Name == "" {
		role.Name = seed.Name
	}
	if overwrite || role.Responsibility == "" {
		role.Responsibility = seed.Responsibility
	}
	if overwrite || role.PromptTemplate == "" {
		role.PromptTemplate = seed.Prompt
	}
	if overwrite || len(stringsFromJSON(role.ToolNames)) == 0 {
		role.ToolNames = jsonList(seed.Tools)
	} else {
		role.ToolNames = jsonList(mergeCapabilityNames(stringsFromJSON(role.ToolNames), seed.Tools))
	}
	if overwrite || len(stringsFromJSON(role.SkillNames)) == 0 {
		role.SkillNames = jsonList(seed.Skills)
	} else {
		role.SkillNames = jsonList(mergeCapabilityNames(stringsFromJSON(role.SkillNames), seed.Skills))
	}
	if overwrite || role.SortOrder == 100 {
		role.SortOrder = seed.SortOrder
	}
}

func mergeCapabilityNames(existing []string, defaults []string) []string {
	merged := make([]string, 0, len(existing)+len(defaults))
	seen := map[string]struct{}{}
	for _, group := range [][]string{existing, defaults} {
		for _, name := range group {
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			merged = append(merged, name)
		}
	}
	return merged
}

func stringsFromJSON(raw []byte) []string {
	var out []string
	_ = json.Unmarshal(raw, &out)
	return out
}

func jsonList(values []string) datatypes.JSON {
	raw, _ := json.Marshal(values)
	return datatypes.JSON(raw)
}

func aiProvidersToDomain(rows []persistmodel.AiProvider) []domainai.Provider {
	out := make([]domainai.Provider, 0, len(rows))
	for _, row := range rows {
		out = append(out, aiProviderFromModel(row))
	}
	return out
}

func aiProviderFromModel(row persistmodel.AiProvider) domainai.Provider {
	out := domainai.Provider{
		ID:             row.ID,
		Name:           row.Name,
		BaseURL:        row.BaseURL,
		APIKeySecretID: row.APIKeySecretID,
		DefaultModel:   row.DefaultModel,
		Enabled:        row.Enabled,
		CreatedAt:      row.CreatedAt,
	}
	if row.APIKeySecret != nil {
		secret := secretFromModel(*row.APIKeySecret)
		out.APIKeySecret = &secret
	}
	return out
}

func aiProviderToModel(row domainai.Provider) persistmodel.AiProvider {
	return persistmodel.AiProvider{
		ID:             row.ID,
		Name:           row.Name,
		BaseURL:        row.BaseURL,
		APIKeySecretID: row.APIKeySecretID,
		DefaultModel:   row.DefaultModel,
		Enabled:        row.Enabled,
		CreatedAt:      row.CreatedAt,
	}
}

func aiProviderModelsToDomain(rows []persistmodel.AiProviderModel) []domainai.ProviderModel {
	out := make([]domainai.ProviderModel, 0, len(rows))
	for _, row := range rows {
		out = append(out, aiProviderModelFromModel(row))
	}
	return out
}

func aiProviderModelFromModel(row persistmodel.AiProviderModel) domainai.ProviderModel {
	return domainai.ProviderModel{
		ID:          row.ID,
		ProviderID:  row.ProviderID,
		ModelID:     row.ModelID,
		DisplayName: row.DisplayName,
		OwnedBy:     row.OwnedBy,
		Enabled:     row.Enabled,
		Raw:         domainkernel.JSON(row.Raw),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func aiProviderModelToModel(row domainai.ProviderModel) persistmodel.AiProviderModel {
	return persistmodel.AiProviderModel{
		ID:          row.ID,
		ProviderID:  row.ProviderID,
		ModelID:     row.ModelID,
		DisplayName: row.DisplayName,
		OwnedBy:     row.OwnedBy,
		Enabled:     row.Enabled,
		Raw:         datatypes.JSON(row.Raw),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func agentRolesToDomain(rows []persistmodel.AgentRole) []domainai.AgentRole {
	out := make([]domainai.AgentRole, 0, len(rows))
	for _, row := range rows {
		out = append(out, agentRoleFromModel(row))
	}
	return out
}

func agentRoleFromModel(row persistmodel.AgentRole) domainai.AgentRole {
	return domainai.AgentRole{
		ID:             row.ID,
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

func agentRoleToModel(row domainai.AgentRole) persistmodel.AgentRole {
	return persistmodel.AgentRole{
		ID:             row.ID,
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
