package meeting

import "errors"

var errMeetingRunSuperseded = errors.New("meeting run is no longer active")

type activeMeetingFailure struct {
	reason string
}

func (e activeMeetingFailure) Error() string {
	return e.reason
}

type roleTurnResult struct {
	Content    string
	Raw        string
	Questions  []map[string]string
	Mentions   []string
	Citations  []string
	Confidence string
}

type moderatorPlan struct {
	Content            string
	Raw                string
	ContinueDiscussion bool
	FocusRoles         []string
	Questions          []map[string]string
}
