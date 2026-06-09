package meeting

import (
	"errors"

	appmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/app/meeting"
)

var errMeetingRunSuperseded = errors.New("meeting run is no longer active")

type activeMeetingFailure struct {
	reason string
}

func (e activeMeetingFailure) Error() string {
	return e.reason
}

type roleTurnResult = appmeeting.RoleTurnResult

type moderatorPlan = appmeeting.ModeratorPlan
