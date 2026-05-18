package migrate

import (
	appwake "github.com/TradingCopilotDevs/TradingCopilot/internal/app/wake"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainwake "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/wake"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"gorm.io/gorm"
)

func cancelInvalidActiveWakePlans(db *gorm.DB) error {
	var rows []persistmodel.WakePlan
	if err := db.Where("status = ?", domainkernel.WakeActive).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		plan := domainwake.Plan{
			ID:                   row.ID,
			MeetingID:            row.MeetingID,
			TriggerType:          row.TriggerType,
			TriggerConfig:        domainkernel.JSON(row.TriggerConfig),
			Reason:               row.Reason,
			SourceMeetingEventID: row.SourceMeetingEventID,
			SourceRoleKey:        row.SourceRoleKey,
			Status:               row.Status,
			NextCheckAt:          row.NextCheckAt,
			FiredAt:              row.FiredAt,
			LastRunAt:            row.LastRunAt,
			ResultSummary:        row.ResultSummary,
			CreatedAt:            row.CreatedAt,
		}
		if err := appwake.ValidatePlan(&plan); err == nil {
			continue
		} else {
			summary := "Invalid wake plan cancelled: " + err.Error()
			if err := db.Model(&persistmodel.WakePlan{}).Where("id = ? AND status = ?", row.ID, domainkernel.WakeActive).Updates(map[string]any{
				"status":         domainkernel.WakeCancelled,
				"result_summary": summary,
			}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
