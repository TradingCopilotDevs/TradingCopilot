package logging

import (
	"context"
	"strings"
	"time"
)

type Entry struct {
	ID         string
	Time       time.Time
	Level      string
	Role       string
	Source     string
	Event      string
	Message    string
	Caller     string
	Stacktrace string
	File       string
	Group      string
	Method     string
	Path       string
	Status     string
	DurationMS *int
	Noise      bool
	Fields     map[string]any
	Raw        map[string]any
}

type File struct {
	Name      string
	Path      string
	SizeBytes int64
	Modified  time.Time
	Active    bool
	Role      string
}

type ConfigSummary struct {
	Dir                 string
	Level               string
	RotationMode        string
	RotationSizeMB      int
	RotationTotalSizeMB int
	RotationMaxAgeDays  int
}

type Query struct {
	From         *time.Time
	To           *time.Time
	Level        string
	Role         string
	Source       string
	Event        string
	Group        string
	Method       string
	Path         string
	Status       string
	SlowOnly     bool
	IncludeNoise bool
	Q            string
	File         string
	Limit        int
	Cursor       string
}

type Result struct {
	Entries    []Entry
	NextCursor string
}

type Reader interface {
	ListFiles(ctx context.Context) ([]File, ConfigSummary, error)
	Query(ctx context.Context, query Query) (Result, error)
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}

func (u Usecase) ListFiles(ctx context.Context) ([]File, ConfigSummary, error) {
	if u.reader == nil {
		return nil, ConfigSummary{}, nil
	}
	return u.reader.ListFiles(ctx)
}

func (u Usecase) Query(ctx context.Context, query Query) (Result, error) {
	if u.reader == nil {
		return Result{}, nil
	}
	query.Level = strings.TrimSpace(query.Level)
	query.Role = strings.TrimSpace(query.Role)
	query.Source = strings.TrimSpace(query.Source)
	query.Event = strings.TrimSpace(query.Event)
	query.Group = strings.TrimSpace(query.Group)
	query.Method = strings.TrimSpace(query.Method)
	query.Path = strings.TrimSpace(query.Path)
	query.Status = strings.TrimSpace(query.Status)
	query.Q = strings.TrimSpace(query.Q)
	query.File = strings.TrimSpace(query.File)
	if query.Limit <= 0 {
		query.Limit = 100
	}
	if query.Limit > 500 {
		query.Limit = 500
	}
	return u.reader.Query(ctx, query)
}
