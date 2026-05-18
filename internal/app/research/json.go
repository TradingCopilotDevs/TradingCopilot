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
