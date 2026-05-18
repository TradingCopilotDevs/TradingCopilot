package telegram

import (
	"context"

	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
)

func (u Usecase) DeleteMessage(ctx context.Context, id uint) (bool, error) {
	row, found, err := u.repo.FindMessage(ctx, id)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	if err := u.withTx(ctx, func(repo Repository) error {
		externalRef := u.service.MessageExternalRef(*row)
		return repo.DeleteMessage(ctx, row, externalRef)
	}); err != nil {
		return true, err
	}
	return true, nil
}

func (u Usecase) RefilterMessages(ctx context.Context, input RefilterInput) (RefilterResult, error) {
	if input.Limit <= 0 {
		input.Limit = 100
	}
	rows, err := u.repo.ListMessagesForRefilter(ctx, input.IDs, input.OnlyUnfiltered, input.Limit)
	if err != nil {
		return RefilterResult{}, err
	}
	var created []*domainmeeting.Meeting
	if err := u.withTx(ctx, func(_ Repository) error {
		for i := range rows {
			if err := u.service.ApplyFilter(ctx, &rows[i], u.security); err != nil {
				return err
			}
			created = append(created, u.ensureMeetingForMessage(ctx, u.service, &rows[i], "telegram_refilter")...)
		}
		return nil
	}); err != nil {
		return RefilterResult{}, err
	}
	return RefilterResult{Rows: u.hydrateMessages(ctx, rows), CreatedMeetings: created}, nil
}

func (u Usecase) FilterMessage(ctx context.Context, id uint) (*MessageMutation, bool, error) {
	row, found, err := u.repo.FindMessage(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	var created []*domainmeeting.Meeting
	if err := u.withTx(ctx, func(_ Repository) error {
		if err := u.service.ApplyFilter(ctx, row, u.security); err != nil {
			return err
		}
		created = u.ensureMeetingForMessage(ctx, u.service, row, "telegram_refilter")
		return nil
	}); err != nil {
		return nil, true, err
	}
	return &MessageMutation{Row: u.hydrateMessage(ctx, *row), CreatedMeetings: created}, true, nil
}
