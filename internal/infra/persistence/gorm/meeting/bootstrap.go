package meeting

import (
	"errors"
	domainai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/ai"
	domainauth "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/auth"

	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/security"
	"gorm.io/gorm"
)

func EnsureAdmin(db *gorm.DB, sec security.Service, username string, password string) (*domainauth.AdminUser, error) {
	ctx := dbContext(db)
	repo := gormrepo.NewAuthRepository(db)
	if user, err := repo.FindAdminByUsername(ctx, username); err == nil {
		return user, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	hash, err := sec.HashPassword(password)
	if err != nil {
		return nil, err
	}
	user := domainauth.AdminUser{Username: username, PasswordHash: hash}
	if err := repo.CreateAdmin(ctx, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func SeedDefaultRoles(db *gorm.DB, overwrite bool) error {
	ctx := dbContext(db)
	repo := gormrepo.NewAIRepository(db)
	for _, seed := range DefaultRoles {
		role, found, err := repo.FindRole(ctx, seed.Key)
		if err != nil {
			return err
		}
		if !found {
			role = &domainai.AgentRole{
				Key: seed.Key, Name: seed.Name, Responsibility: seed.Responsibility, PromptTemplate: seed.Prompt,
				ToolNames: JSONList(seed.Tools), SkillNames: JSONList(seed.Skills), Enabled: true, SortOrder: seed.SortOrder,
			}
			if err := repo.SaveRole(ctx, role); err != nil {
				return err
			}
			continue
		}
		applyRoleDefaults(role, seed, overwrite)
		if err := repo.SaveRole(ctx, role); err != nil {
			return err
		}
	}
	return nil
}

func applyRoleDefaults(role *domainai.AgentRole, seed RoleSeed, overwrite bool) {
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
		role.ToolNames = JSONList(seed.Tools)
	} else {
		role.ToolNames = JSONList(mergeCapabilityNames(stringsFromJSON(role.ToolNames), seed.Tools))
	}
	if overwrite || len(stringsFromJSON(role.SkillNames)) == 0 {
		role.SkillNames = JSONList(seed.Skills)
	} else {
		role.SkillNames = JSONList(mergeCapabilityNames(stringsFromJSON(role.SkillNames), seed.Skills))
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
