package research

import (
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
)

type Team struct {
	ID             uint
	Name           string
	Description    string
	PaperAccountID uint
	AssetClass     string
	Active         bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type TeamRole struct {
	ID             uint
	ResearchTeamID uint
	Key            string
	Name           string
	Responsibility string
	PromptTemplate string
	ProviderID     *uint
	Model          *string
	ToolNames      kernel.JSON
	SkillNames     kernel.JSON
	Enabled        bool
	SortOrder      int
}
