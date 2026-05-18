package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
)

var ErrProviderNotFound = errors.New("ai provider not found")

type Repository interface {
	ListProviders(ctx context.Context) ([]domainai.Provider, error)
	CreateProvider(ctx context.Context, provider *domainai.Provider) error
	FindProvider(ctx context.Context, id uint) (*domainai.Provider, bool, error)
	SaveProvider(ctx context.Context, provider *domainai.Provider) error
	ListProviderModels(ctx context.Context, providerID uint) ([]domainai.ProviderModel, error)
	FindProviderModel(ctx context.Context, providerID uint, modelID string) (*domainai.ProviderModel, bool, error)
	SaveProviderModel(ctx context.Context, model *domainai.ProviderModel) error
	ListRoles(ctx context.Context) ([]domainai.AgentRole, error)
	FindRole(ctx context.Context, key string) (*domainai.AgentRole, bool, error)
	SaveRole(ctx context.Context, role *domainai.AgentRole) error
	UpsertSecret(ctx context.Context, kind domainkernel.SecretKind, name string, encryptedValue string) (*domainsettings.Secret, error)
	SeedDefaultRoles(ctx context.Context, overwrite bool) (int, error)
}

type Transactor interface {
	WithTx(ctx context.Context, fn func(Repository) error) error
}

type SecurityService interface {
	EncryptSecret(value string) (string, error)
}

type ModelSyncer interface {
	ListModels(ctx context.Context, provider domainai.Provider) ([]map[string]any, error)
}

type Usecase struct {
	repo     Repository
	security SecurityService
	syncer   ModelSyncer
	tx       Transactor
}

func NewUsecase(repo Repository, security SecurityService, syncer ModelSyncer, tx Transactor) Usecase {
	return Usecase{repo: repo, security: security, syncer: syncer, tx: tx}
}

type ProviderInput struct {
	Name         string
	BaseURL      string
	APIKey       *string
	DefaultModel string
	Enabled      bool
}

type RoleInput struct {
	Attributes map[string]any
}

type CapabilityDefinition struct {
	Key         string
	Title       string
	Description string
	Category    string
}

func (u Usecase) ListProviders(ctx context.Context) ([]domainai.Provider, error) {
	return u.repo.ListProviders(ctx)
}

func (u Usecase) CreateProvider(ctx context.Context, input ProviderInput) (*domainai.Provider, error) {
	row := domainai.Provider{Name: input.Name, BaseURL: input.BaseURL, DefaultModel: input.DefaultModel, Enabled: input.Enabled}
	if err := u.tx.WithTx(ctx, func(repo Repository) error {
		if err := repo.CreateProvider(ctx, &row); err != nil {
			return err
		}
		if input.APIKey != nil && *input.APIKey != "" {
			secret, err := u.saveProviderSecret(ctx, repo, row.ID, "", *input.APIKey)
			if err != nil {
				return err
			}
			row.APIKeySecretID = &secret.ID
			return repo.SaveProvider(ctx, &row)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &row, nil
}

func (u Usecase) UpdateProvider(ctx context.Context, id uint, input ProviderInput) (*domainai.Provider, error) {
	var saved *domainai.Provider
	if err := u.tx.WithTx(ctx, func(repo Repository) error {
		row, found, err := repo.FindProvider(ctx, id)
		if err != nil {
			return err
		}
		if !found {
			return ErrProviderNotFound
		}
		row.Name, row.BaseURL, row.DefaultModel, row.Enabled = input.Name, input.BaseURL, input.DefaultModel, input.Enabled
		if input.APIKey != nil && *input.APIKey != "" {
			name := ""
			if row.APIKeySecret != nil {
				name = row.APIKeySecret.Name
			}
			secret, err := u.saveProviderSecret(ctx, repo, row.ID, name, *input.APIKey)
			if err != nil {
				return err
			}
			row.APIKeySecretID = &secret.ID
		}
		if err := repo.SaveProvider(ctx, row); err != nil {
			return err
		}
		saved = row
		return nil
	}); err != nil {
		return nil, err
	}
	return saved, nil
}

func (u Usecase) ListProviderModels(ctx context.Context, providerID uint) ([]domainai.ProviderModel, error) {
	return u.repo.ListProviderModels(ctx, providerID)
}

func (u Usecase) SyncProviderModels(ctx context.Context, providerID uint) ([]domainai.ProviderModel, error) {
	provider, found, err := u.repo.FindProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrProviderNotFound
	}
	models, err := u.syncer.ListModels(ctx, *provider)
	if err != nil {
		return nil, err
	}
	synced := make([]domainai.ProviderModel, 0, len(models))
	if err := u.tx.WithTx(ctx, func(repo Repository) error {
		for _, item := range models {
			modelID := fmt.Sprint(firstNonEmpty(item["id"], item["model"]))
			if modelID == "" {
				continue
			}
			row, found, err := repo.FindProviderModel(ctx, provider.ID, modelID)
			if err != nil {
				return err
			}
			if !found {
				row = &domainai.ProviderModel{ProviderID: provider.ID, ModelID: modelID, DisplayName: modelID}
			}
			row.DisplayName = fmt.Sprint(firstNonEmpty(item["name"], modelID))
			row.Enabled = true
			row.Raw = jsonData(item)
			if err := repo.SaveProviderModel(ctx, row); err != nil {
				return err
			}
			synced = append(synced, *row)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return synced, nil
}

func (u Usecase) ToolDefinitions(context.Context) []CapabilityDefinition {
	return ToolDefinitions
}

func (u Usecase) SkillDefinitions(context.Context) []CapabilityDefinition {
	return SkillDefinitions
}

func (u Usecase) ListRoles(ctx context.Context) ([]domainai.AgentRole, error) {
	var rows []domainai.AgentRole
	if err := u.tx.WithTx(ctx, func(repo Repository) error {
		if _, err := repo.SeedDefaultRoles(ctx, false); err != nil {
			return err
		}
		var err error
		rows, err = repo.ListRoles(ctx)
		return err
	}); err != nil {
		return nil, err
	}
	return rows, nil
}

func (u Usecase) ApplyDefaultRoleCapabilities(ctx context.Context) (int, error) {
	count := 0
	if err := u.tx.WithTx(ctx, func(repo Repository) error {
		var err error
		count, err = repo.SeedDefaultRoles(ctx, true)
		return err
	}); err != nil {
		return 0, err
	}
	return count, nil
}

func (u Usecase) UpsertRole(ctx context.Context, key string, input RoleInput) (*domainai.AgentRole, error) {
	var row *domainai.AgentRole
	if err := u.tx.WithTx(ctx, func(repo Repository) error {
		foundRow, found, err := repo.FindRole(ctx, key)
		if err != nil {
			return err
		}
		if !found {
			foundRow = &domainai.AgentRole{Key: key}
		}
		ApplyRoleAttributes(foundRow, input.Attributes)
		if err := repo.SaveRole(ctx, foundRow); err != nil {
			return err
		}
		row = foundRow
		return nil
	}); err != nil {
		return nil, err
	}
	return row, nil
}

func (u Usecase) saveProviderSecret(ctx context.Context, repo Repository, providerID uint, existingName string, value string) (*domainsettings.Secret, error) {
	encrypted, err := u.security.EncryptSecret(value)
	if err != nil {
		return nil, err
	}
	name := existingName
	if name == "" {
		name = fmt.Sprintf("provider:%d:api_key", providerID)
	}
	return repo.UpsertSecret(ctx, domainkernel.SecretKindAIProvider, name, encrypted)
}

func ApplyRoleAttributes(role *domainai.AgentRole, payload map[string]any) {
	if v, ok := stringAttrValue(payload, "name"); ok {
		role.Name = v
	}
	if v, ok := stringAttrValue(payload, "responsibility"); ok {
		role.Responsibility = v
	}
	if v, ok := stringAttrValue(payload, "promptTemplate", "prompt_template"); ok {
		role.PromptTemplate = v
	}
	if v, ok := attrValue(payload, "providerId", "provider_id"); ok {
		role.ProviderID = uintPtrFromAny(v)
	}
	if v, ok := attrValue(payload, "model"); ok {
		if v == nil || fmt.Sprint(v) == "" {
			role.Model = nil
		} else {
			model := fmt.Sprint(v)
			role.Model = &model
		}
	}
	if v, ok := attrValue(payload, "toolNames", "tool_names"); ok {
		role.ToolNames = jsonData(v)
	}
	if v, ok := attrValue(payload, "skillNames", "skill_names"); ok {
		role.SkillNames = jsonData(v)
	}
	if v, ok := payload["enabled"].(bool); ok {
		role.Enabled = v
	}
	if v, ok := numberAttrValue(payload, "sortOrder", "sort_order"); ok {
		role.SortOrder = int(v)
	}
}

func firstNonEmpty(values ...any) any {
	for _, value := range values {
		if value == nil {
			continue
		}
		if text := fmt.Sprint(value); text != "" && text != "<nil>" {
			return value
		}
	}
	return ""
}

func jsonData(value any) domainkernel.JSON {
	raw, _ := json.Marshal(value)
	return domainkernel.JSON(raw)
}
