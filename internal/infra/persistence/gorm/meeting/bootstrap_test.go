package meeting

import (
	domainai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/ai"
	"strings"
	"testing"
)

func TestDefaultRolesMatchOriginalPromptShape(t *testing.T) {
	byKey := map[string]RoleSeed{}
	for _, role := range DefaultRoles {
		byKey[role.Key] = role
	}
	for _, key := range []string{"moderator", "news_analyst", "macro", "sector", "fundamental", "technical", "risk", "portfolio"} {
		if byKey[key].Key == "" {
			t.Fatalf("missing default role %s", key)
		}
		if len([]rune(byKey[key].Prompt)) < 80 {
			t.Fatalf("default role %s prompt is unexpectedly short: %q", key, byKey[key].Prompt)
		}
	}
	if len(byKey["moderator"].Prompt) == 0 || !containsString(byKey["moderator"].Tools, "market.upsert_watchlist") {
		t.Fatalf("moderator defaults are not aligned with original: %+v", byKey["moderator"])
	}
	if !strings.Contains(byKey["portfolio"].Prompt, "position_pct") || !containsString(byKey["portfolio"].Tools, "wake.create_plan") {
		t.Fatalf("portfolio defaults are not aligned with original: %+v", byKey["portfolio"])
	}
}

func TestSeedDefaultRolesFillsMissingTextAndMergesCapabilities(t *testing.T) {
	db := newMeetingTestDB(t)
	role := domainai.AgentRole{
		Key:            "moderator",
		Name:           "Custom Moderator",
		Responsibility: "custom responsibility",
		PromptTemplate: "custom prompt",
		ToolNames:      JSONList([]string{"custom.tool"}),
		SkillNames:     JSONList([]string{"custom-skill"}),
		Enabled:        true,
		SortOrder:      100,
	}
	mustMeeting(t, db.Create(&role).Error)
	mustMeeting(t, SeedDefaultRoles(db, false))
	mustMeeting(t, db.First(&role, "key = ?", "moderator").Error)
	if role.Name != "Custom Moderator" || role.PromptTemplate != "custom prompt" {
		t.Fatalf("non-overwrite seed should preserve custom text: %+v", role)
	}
	tools := stringsFromJSON(role.ToolNames)
	if !containsString(tools, "custom.tool") || !containsString(tools, "market.upsert_watchlist") {
		t.Fatalf("non-overwrite seed should merge tools, got %v", tools)
	}
	if role.SortOrder != 10 {
		t.Fatalf("default sort order should fill legacy 100, got %d", role.SortOrder)
	}
}

func TestSeedDefaultRolesOverwriteRestoresOriginalPrompt(t *testing.T) {
	db := newMeetingTestDB(t)
	role := domainai.AgentRole{Key: "risk", Name: "Custom", Responsibility: "custom", PromptTemplate: "custom", ToolNames: JSONList([]string{"custom.tool"}), Enabled: true, SortOrder: 99}
	mustMeeting(t, db.Create(&role).Error)
	mustMeeting(t, SeedDefaultRoles(db, true))
	mustMeeting(t, db.First(&role, "key = ?", "risk").Error)
	if role.Name == "Custom" || !strings.Contains(role.PromptTemplate, "\u5ba1\u614e") {
		t.Fatalf("overwrite seed did not restore original role text: %+v", role)
	}
	tools := stringsFromJSON(role.ToolNames)
	if containsString(tools, "custom.tool") || !containsString(tools, "paper.risk_configs") {
		t.Fatalf("overwrite seed should replace tools with defaults, got %v", tools)
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
