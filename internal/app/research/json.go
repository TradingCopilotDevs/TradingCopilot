package research

import (
	"encoding/json"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
)

func jsonList(values []string) kernel.JSON {
	if values == nil {
		values = []string{}
	}
	raw, _ := json.Marshal(values)
	return kernel.JSON(raw)
}

func jsonStringList(raw kernel.JSON) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return []string{}
	}
	return values
}
