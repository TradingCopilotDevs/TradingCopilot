package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
	domaintelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/telegram"
)

func (u Usecase) applyMessageFilterInput(row *domaintelegram.Message, input MessageInput) {
	if input.HasDecision {
		if input.FilterDecision == nil {
			row.FilterDecision = nil
		} else {
			decision := domainkernel.NewsDecision(fmt.Sprint(input.FilterDecision))
			row.FilterDecision = &decision
		}
	}
	if input.HasReason {
		if input.FilterReason == nil {
			row.FilterReason = nil
		} else {
			reason := fmt.Sprint(input.FilterReason)
			row.FilterReason = &reason
		}
	}
	if input.HasSymbols {
		row.RelatedSymbols = u.service.JSON(input.RelatedSymbols)
	}
}

func (u Usecase) ensureMeetingForMessage(ctx context.Context, service Service, row *domaintelegram.Message, triggerSource string) []*domainmeeting.Meeting {
	if row.FilterDecision == nil || *row.FilterDecision != domainkernel.NewsMeeting {
		return nil
	}
	meeting, created, err := service.EnsureMeetingForMessage(ctx, row, triggerSource)
	if err == nil && created && meeting != nil {
		return []*domainmeeting.Meeting{meeting}
	}
	return nil
}

func (u Usecase) hydrateMessages(ctx context.Context, messages []domaintelegram.Message) []MessageRow {
	out := make([]MessageRow, 0, len(messages))
	for _, message := range messages {
		out = append(out, u.hydrateMessage(ctx, message))
	}
	return out
}

func (u Usecase) hydrateMessage(ctx context.Context, message domaintelegram.Message) MessageRow {
	channel, _, _ := u.repo.FindChannelForMessage(ctx, message.ChannelID)
	if channel == nil {
		channel = &domaintelegram.Channel{}
	}
	return MessageRow{Message: message, Channel: *channel}
}

func (u Usecase) upsertEncryptedSecret(ctx context.Context, repo Repository, kind domainkernel.SecretKind, name string, value string) error {
	encrypted, err := u.security.EncryptSecret(value)
	if err != nil {
		return err
	}
	row, found, err := repo.FindSecret(ctx, kind, name)
	if err != nil {
		return err
	}
	if !found {
		row = &domainsettings.Secret{Kind: kind, Name: name, EncryptedValue: encrypted}
		return repo.CreateSecret(ctx, row)
	}
	row.EncryptedValue = encrypted
	return repo.SaveSecret(ctx, row)
}

func (u Usecase) hasSecret(ctx context.Context, kind domainkernel.SecretKind, name string) bool {
	_, ok := u.secret(ctx, kind, name)
	return ok
}

func (u Usecase) withTx(ctx context.Context, fn func(Repository) error) error {
	if u.tx != nil {
		return u.tx.WithTx(ctx, fn)
	}
	return errors.New("telegram unit of work is not configured")
}

func (u Usecase) secret(ctx context.Context, kind domainkernel.SecretKind, name string) (domainsettings.Secret, bool) {
	row, ok, _ := u.repo.FindSecret(ctx, kind, name)
	if !ok || row == nil {
		return domainsettings.Secret{}, false
	}
	return *row, true
}

func botLeaseClaimable(setting domainsettings.AppSetting, owner string, now time.Time, ttl time.Duration) bool {
	description := "telegram bot listener lease: " + owner
	if setting.Description != nil && *setting.Description == description {
		return true
	}
	var payload struct {
		Owner     string    `json:"owner"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(setting.Value, &payload); err == nil {
		if strings.TrimSpace(payload.Owner) == owner {
			return true
		}
		if !payload.ExpiresAt.IsZero() && !payload.ExpiresAt.After(now) {
			return true
		}
	}
	if setting.UpdatedAt.IsZero() {
		return true
	}
	return !setting.UpdatedAt.After(now.Add(-ttl))
}
