package system

import (
	"context"
	"encoding/json"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/settings"
	"os"
	"time"
)

const (
	HeartbeatIntervalSeconds   = 10
	HeartbeatStaleAfterSeconds = 30
	HeartbeatKeyPrefix         = "system_heartbeat_"
)

type Heartbeat struct {
	Service   string    `json:"service"`
	Mode      string    `json:"mode"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	PID       int       `json:"pid"`
	Version   string    `json:"version"`
	Error     string    `json:"error,omitempty"`
}

type Repository interface {
	FindAppSetting(ctx context.Context, key string) (*domainsettings.AppSetting, bool, error)
	SaveAppSetting(ctx context.Context, setting *domainsettings.AppSetting) error
	DeleteAppSetting(ctx context.Context, key string) error
}

type Transactor interface {
	WithTx(ctx context.Context, fn func(Repository) error) error
}

type Usecase struct {
	repo Repository
	tx   Transactor
}

func NewUsecase(repo Repository, tx Transactor) Usecase {
	return Usecase{repo: repo, tx: tx}
}

func HeartbeatKey(service string) string {
	return HeartbeatKeyPrefix + service
}

func (u Usecase) RecordHeartbeat(ctx context.Context, serviceName string, mode string, errText string) error {
	return u.writeHeartbeat(ctx, serviceName, mode, "running", errText)
}

func (u Usecase) MarkStopped(ctx context.Context, serviceName string, mode string, errText string) error {
	return u.writeHeartbeat(ctx, serviceName, mode, "stopped", errText)
}

func (u Usecase) LoadHeartbeat(ctx context.Context, serviceName string) (*Heartbeat, error) {
	setting, found, err := u.repo.FindAppSetting(ctx, HeartbeatKey(serviceName))
	if err != nil || !found {
		return nil, err
	}
	var heartbeat Heartbeat
	if err := json.Unmarshal(setting.Value, &heartbeat); err != nil {
		return nil, err
	}
	return &heartbeat, nil
}

func (u Usecase) DeleteInvalidHeartbeat(ctx context.Context, serviceName string) (bool, error) {
	setting, found, err := u.repo.FindAppSetting(ctx, HeartbeatKey(serviceName))
	if err != nil || !found {
		return false, err
	}
	var payload struct {
		Timestamp time.Time `json:"timestamp"`
	}
	if err := json.Unmarshal(setting.Value, &payload); err == nil && !payload.Timestamp.IsZero() {
		return false, nil
	}
	if err := u.repo.DeleteAppSetting(ctx, setting.Key); err != nil {
		return false, err
	}
	return true, nil
}

func HeartbeatAgeSeconds(heartbeat *Heartbeat, now time.Time) *int {
	if heartbeat == nil || heartbeat.Timestamp.IsZero() {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	age := int(now.Sub(heartbeat.Timestamp).Seconds())
	if age < 0 {
		age = 0
	}
	return &age
}

func (u Usecase) writeHeartbeat(ctx context.Context, serviceName string, mode string, status string, errText string) error {
	payload := Heartbeat{
		Service:   serviceName,
		Mode:      mode,
		Status:    status,
		Timestamp: time.Now(),
		PID:       os.Getpid(),
		Version:   "0.1.0",
	}
	if errText != "" {
		if len(errText) > 300 {
			errText = errText[:300]
		}
		payload.Error = errText
	}
	key := HeartbeatKey(serviceName)
	description := "runtime heartbeat for " + serviceName
	row := domainsettings.AppSetting{Key: key, Value: domainkernel.NewJSON(payload), Description: &description, UpdatedAt: time.Now()}
	return u.tx.WithTx(ctx, func(repo Repository) error {
		return repo.SaveAppSetting(ctx, &row)
	})
}
