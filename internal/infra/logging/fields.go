package logging

import (
	"time"

	"go.uber.org/zap"
)

const (
	Placeholder = "not_applicable"

	FieldEvent    = "event"
	FieldGroup    = "group"
	FieldMethod   = "method"
	FieldStatus   = "status"
	FieldDuration = "durationMs"

	StatusOK            = "ok"
	StatusError         = "error"
	StatusWarning       = "warning"
	StatusSlow          = "slow"
	StatusSkipped       = "skipped"
	StatusNotApplicable = Placeholder
	MethodNotApplicable = Placeholder
	GroupNotApplicable  = Placeholder
	EventNotApplicable  = Placeholder
)

func Event(value string) zap.Field {
	return zap.String(FieldEvent, defaultText(value, EventNotApplicable))
}

func Group(value string) zap.Field {
	return zap.String(FieldGroup, defaultText(value, GroupNotApplicable))
}

func Method(value string) zap.Field {
	return zap.String(FieldMethod, defaultText(value, MethodNotApplicable))
}

func Status(value string) zap.Field {
	return zap.String(FieldStatus, defaultText(value, StatusNotApplicable))
}

func DurationMS(value int64) zap.Field {
	if value < 0 {
		return zap.Any(FieldDuration, nil)
	}
	return zap.Int64(FieldDuration, value)
}

func DurationSince(start time.Time) zap.Field {
	if start.IsZero() {
		return zap.Any(FieldDuration, nil)
	}
	elapsed := time.Since(start)
	ms := elapsed.Milliseconds()
	if elapsed > 0 && ms == 0 {
		ms = 1
	}
	return DurationMS(ms)
}

func OperationFields(event string, group string, method string, status string, start time.Time, fields ...zap.Field) []zap.Field {
	out := []zap.Field{
		Event(event),
		Group(group),
		Method(method),
		Status(status),
		DurationSince(start),
	}
	out = append(out, fields...)
	return out
}

func LogOperation(start time.Time, event string, group string, method string, message string, err error, fields ...zap.Field) {
	status := StatusOK
	if err != nil {
		status = StatusError
		fields = append(fields, zap.Error(err))
		Logger().Error(message, OperationFields(event, group, method, status, start, fields...)...)
		return
	}
	Logger().Info(message, OperationFields(event, group, method, status, start, fields...)...)
}

func defaultText(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
