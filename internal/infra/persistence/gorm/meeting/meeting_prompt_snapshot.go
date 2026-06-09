package meeting

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
)

const (
	managedMeetingPromptVersion = "managed-runtime-v1"
	legacyMeetingPromptVersion  = "legacy-role-tools-v1"
)

func rolePromptSnapshot(role domainai.AgentRole, modelName string, messages []map[string]string, label string, promptVersion string) map[string]any {
	providerID := providerIDForRole(role)
	providerName := ""
	defaultModel := ""
	if role.Provider != nil {
		providerName = role.Provider.Name
		defaultModel = role.Provider.DefaultModel
	}
	if modelName == "" {
		modelName = defaultModel
	}
	return map[string]any{
		"label":              strings.TrimSpace(label),
		"roleKey":            role.Key,
		"roleName":           role.Name,
		"providerId":         providerID,
		"providerName":       providerName,
		"model":              modelName,
		"promptVersion":      promptVersion,
		"promptHash":         hashJSON(messages),
		"systemPromptHash":   hashMessageRole(messages, "system"),
		"userPromptHash":     hashMessageRole(messages, "user"),
		"promptTemplateHash": hashText(role.PromptTemplate),
		"messageCount":       len(messages),
		"toolNames":          stringsFromJSON(role.ToolNames),
		"skillNames":         stringsFromJSON(role.SkillNames),
	}
}

func providerIDForRole(role domainai.AgentRole) any {
	if role.Provider != nil {
		return role.Provider.ID
	}
	if role.ProviderID != nil {
		return *role.ProviderID
	}
	return nil
}

func hashMessageRole(messages []map[string]string, role string) string {
	parts := []string{}
	for _, message := range messages {
		if strings.EqualFold(strings.TrimSpace(message["role"]), role) {
			parts = append(parts, message["content"])
		}
	}
	return hashText(strings.Join(parts, "\n\n"))
}

func hashJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return hashText("")
	}
	return hashText(string(raw))
}

func hashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
