package paper

import (
	domainpaper "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/paper"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"gorm.io/gorm"
)

func riskConfigModelToDomain(row persistmodel.RiskConfig) domainpaper.RiskConfig {
	return domainpaper.RiskConfig{
		ID:              row.ID,
		Name:            row.Name,
		InitialCash:     row.InitialCash,
		MaxPositionPct:  row.MaxPositionPct,
		MaxOrderPct:     row.MaxOrderPct,
		AllowShort:      row.AllowShort,
		AllowMargin:     row.AllowMargin,
		AllowSHMain:     row.AllowSHMain,
		AllowSZMain:     row.AllowSZMain,
		AllowBJ:         row.AllowBJ,
		AllowSTAR:       row.AllowSTAR,
		AllowChiNext:    row.AllowChiNext,
		AllowETFLOF:     row.AllowETFLOF,
		CommissionRate:  row.CommissionRate,
		MinCommission:   row.MinCommission,
		StampDutyRate:   row.StampDutyRate,
		TransferFeeRate: row.TransferFeeRate,
		Enabled:         row.Enabled,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func riskConfigDomainToModel(row domainpaper.RiskConfig) persistmodel.RiskConfig {
	return persistmodel.RiskConfig{
		ID:              row.ID,
		Name:            row.Name,
		InitialCash:     row.InitialCash,
		MaxPositionPct:  row.MaxPositionPct,
		MaxOrderPct:     row.MaxOrderPct,
		AllowShort:      row.AllowShort,
		AllowMargin:     row.AllowMargin,
		AllowSHMain:     row.AllowSHMain,
		AllowSZMain:     row.AllowSZMain,
		AllowBJ:         row.AllowBJ,
		AllowSTAR:       row.AllowSTAR,
		AllowChiNext:    row.AllowChiNext,
		AllowETFLOF:     row.AllowETFLOF,
		CommissionRate:  row.CommissionRate,
		MinCommission:   row.MinCommission,
		StampDutyRate:   row.StampDutyRate,
		TransferFeeRate: row.TransferFeeRate,
		Enabled:         row.Enabled,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func paperAccountModelToDomain(row persistmodel.PaperAccount) domainpaper.Account {
	return domainpaper.Account{
		ID:           row.ID,
		Name:         row.Name,
		InitialCash:  row.InitialCash,
		Cash:         row.Cash,
		RiskConfigID: row.RiskConfigID,
		Active:       row.Active,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

func paperAccountDomainToModel(row domainpaper.Account) persistmodel.PaperAccount {
	return persistmodel.PaperAccount{
		ID:           row.ID,
		Name:         row.Name,
		InitialCash:  row.InitialCash,
		Cash:         row.Cash,
		RiskConfigID: row.RiskConfigID,
		Active:       row.Active,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

func paperAccountsModelToDomain(rows []persistmodel.PaperAccount) []domainpaper.Account {
	out := make([]domainpaper.Account, 0, len(rows))
	for _, row := range rows {
		out = append(out, paperAccountModelToDomain(row))
	}
	return out
}

func paperOrderModelToDomain(row persistmodel.PaperOrder) domainpaper.Order {
	return domainpaper.Order{
		ID:                   row.ID,
		AccountID:            row.AccountID,
		MeetingID:            row.MeetingID,
		Code:                 row.Code,
		Side:                 row.Side,
		Quantity:             row.Quantity,
		Status:               row.Status,
		SuggestedPrice:       row.SuggestedPrice,
		FilledPrice:          row.FilledPrice,
		Reason:               row.Reason,
		SubmittedAt:          row.SubmittedAt,
		ExecuteAfter:         row.ExecuteAfter,
		ExpireAt:             row.ExpireAt,
		SourceMeetingEventID: row.SourceMeetingEventID,
		ExecutionNote:        row.ExecutionNote,
		Commission:           row.Commission,
		StampDuty:            row.StampDuty,
		TransferFee:          row.TransferFee,
		NetAmount:            row.NetAmount,
		CreatedAt:            row.CreatedAt,
		FilledAt:             row.FilledAt,
	}
}

func paperOrderDomainToModel(row domainpaper.Order) persistmodel.PaperOrder {
	return persistmodel.PaperOrder{
		ID:                   row.ID,
		AccountID:            row.AccountID,
		MeetingID:            row.MeetingID,
		Code:                 row.Code,
		Side:                 row.Side,
		Quantity:             row.Quantity,
		Status:               row.Status,
		SuggestedPrice:       row.SuggestedPrice,
		FilledPrice:          row.FilledPrice,
		Reason:               row.Reason,
		SubmittedAt:          row.SubmittedAt,
		ExecuteAfter:         row.ExecuteAfter,
		ExpireAt:             row.ExpireAt,
		SourceMeetingEventID: row.SourceMeetingEventID,
		ExecutionNote:        row.ExecutionNote,
		Commission:           row.Commission,
		StampDuty:            row.StampDuty,
		TransferFee:          row.TransferFee,
		NetAmount:            row.NetAmount,
		CreatedAt:            row.CreatedAt,
		FilledAt:             row.FilledAt,
	}
}

func paperOrdersModelToDomain(rows []persistmodel.PaperOrder) []domainpaper.Order {
	out := make([]domainpaper.Order, 0, len(rows))
	for _, row := range rows {
		out = append(out, paperOrderModelToDomain(row))
	}
	return out
}

func paperPositionModelToDomain(row persistmodel.PaperPosition) domainpaper.Position {
	return domainpaper.Position{
		ID:            row.ID,
		AccountID:     row.AccountID,
		Code:          row.Code,
		Quantity:      row.Quantity,
		AvgCost:       row.AvgCost,
		CostAmount:    row.CostAmount,
		LastPrice:     row.LastPrice,
		MarketValue:   row.MarketValue,
		UnrealizedPNL: row.UnrealizedPNL,
		RealizedPNL:   row.RealizedPNL,
		UpdatedAt:     row.UpdatedAt,
	}
}

func paperPositionDomainToModel(row domainpaper.Position) persistmodel.PaperPosition {
	return persistmodel.PaperPosition{
		ID:            row.ID,
		AccountID:     row.AccountID,
		Code:          row.Code,
		Quantity:      row.Quantity,
		AvgCost:       row.AvgCost,
		CostAmount:    row.CostAmount,
		LastPrice:     row.LastPrice,
		MarketValue:   row.MarketValue,
		UnrealizedPNL: row.UnrealizedPNL,
		RealizedPNL:   row.RealizedPNL,
		UpdatedAt:     row.UpdatedAt,
	}
}

func paperPositionsModelToDomain(rows []persistmodel.PaperPosition) []domainpaper.Position {
	out := make([]domainpaper.Position, 0, len(rows))
	for _, row := range rows {
		out = append(out, paperPositionModelToDomain(row))
	}
	return out
}

func paperFillDomainToModel(row domainpaper.Fill) persistmodel.PaperFill {
	return persistmodel.PaperFill{
		ID:          row.ID,
		OrderID:     row.OrderID,
		AccountID:   row.AccountID,
		Code:        row.Code,
		Side:        row.Side,
		Quantity:    row.Quantity,
		Price:       row.Price,
		GrossAmount: row.GrossAmount,
		Commission:  row.Commission,
		StampDuty:   row.StampDuty,
		TransferFee: row.TransferFee,
		NetAmount:   row.NetAmount,
		RealizedPNL: row.RealizedPNL,
		FilledAt:    row.FilledAt,
	}
}

func paperFillModelToDomain(row persistmodel.PaperFill) domainpaper.Fill {
	return domainpaper.Fill{
		ID:          row.ID,
		OrderID:     row.OrderID,
		AccountID:   row.AccountID,
		Code:        row.Code,
		Side:        row.Side,
		Quantity:    row.Quantity,
		Price:       row.Price,
		GrossAmount: row.GrossAmount,
		Commission:  row.Commission,
		StampDuty:   row.StampDuty,
		TransferFee: row.TransferFee,
		NetAmount:   row.NetAmount,
		RealizedPNL: row.RealizedPNL,
		FilledAt:    row.FilledAt,
	}
}

func paperFillsModelToDomain(rows []persistmodel.PaperFill) []domainpaper.Fill {
	out := make([]domainpaper.Fill, 0, len(rows))
	for _, row := range rows {
		out = append(out, paperFillModelToDomain(row))
	}
	return out
}

func paperEquitySnapshotModelToDomain(row persistmodel.PaperEquitySnapshot) domainpaper.EquitySnapshot {
	return domainpaper.EquitySnapshot{
		ID:            row.ID,
		AccountID:     row.AccountID,
		SnapshotTime:  row.SnapshotTime,
		Cash:          row.Cash,
		MarketValue:   row.MarketValue,
		TotalEquity:   row.TotalEquity,
		UnrealizedPNL: row.UnrealizedPNL,
		RealizedPNL:   row.RealizedPNL,
		DailyPNL:      row.DailyPNL,
	}
}

func paperEquitySnapshotsModelToDomain(rows []persistmodel.PaperEquitySnapshot) []domainpaper.EquitySnapshot {
	out := make([]domainpaper.EquitySnapshot, 0, len(rows))
	for _, row := range rows {
		out = append(out, paperEquitySnapshotModelToDomain(row))
	}
	return out
}

func savePaperAccountRecord(db *gorm.DB, account *domainpaper.Account) error {
	return gormrepo.NewPaperRepository(db).SaveAccount(dbContext(db), account)
}

func savePaperOrderRecord(db *gorm.DB, order *domainpaper.Order) error {
	return gormrepo.NewPaperRepository(db).SaveOrder(dbContext(db), order)
}
