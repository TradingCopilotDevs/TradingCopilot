package logging

import (
	"strings"

	"go.uber.org/zap/zapcore"
)

type defaultFieldsCore struct {
	zapcore.Core
	context []zapcore.Field
}

func newDefaultFieldsCore(core zapcore.Core) zapcore.Core {
	return defaultFieldsCore{Core: core}
}

func (c defaultFieldsCore) With(fields []zapcore.Field) zapcore.Core {
	context := append([]zapcore.Field{}, c.context...)
	context = append(context, fields...)
	return defaultFieldsCore{Core: c.Core.With(fields), context: context}
}

func (c defaultFieldsCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return checked.AddCore(entry, c)
	}
	return checked
}

func (c defaultFieldsCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	fields = appendLogDefaults(hasLogFields(c.context, fields), fields)
	return c.Core.Write(entry, fields)
}

type logFieldSet struct {
	event    bool
	group    bool
	method   bool
	status   bool
	duration bool
}

func hasLogFields(context []zapcore.Field, fields []zapcore.Field) logFieldSet {
	var set logFieldSet
	for _, field := range context {
		set.mark(field.Key)
	}
	for _, field := range fields {
		set.mark(field.Key)
	}
	return set
}

func (s *logFieldSet) mark(key string) {
	switch strings.TrimSpace(key) {
	case FieldEvent:
		s.event = true
	case FieldGroup:
		s.group = true
	case FieldMethod:
		s.method = true
	case FieldStatus:
		s.status = true
	case FieldDuration:
		s.duration = true
	}
}

func appendLogDefaults(set logFieldSet, fields []zapcore.Field) []zapcore.Field {
	if !set.event {
		fields = append(fields, zapcore.Field{Key: FieldEvent, Type: zapcore.StringType, String: EventNotApplicable})
	}
	if !set.group {
		fields = append(fields, zapcore.Field{Key: FieldGroup, Type: zapcore.StringType, String: GroupNotApplicable})
	}
	if !set.method {
		fields = append(fields, zapcore.Field{Key: FieldMethod, Type: zapcore.StringType, String: MethodNotApplicable})
	}
	if !set.status {
		fields = append(fields, zapcore.Field{Key: FieldStatus, Type: zapcore.StringType, String: StatusNotApplicable})
	}
	if !set.duration {
		fields = append(fields, zapcore.Field{Key: FieldDuration, Type: zapcore.ReflectType, Interface: nil})
	}
	return fields
}
