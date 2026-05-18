package model

import (
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	"time"

	"gorm.io/datatypes"
)

type TelegramChannel struct {
	ID            uint   `gorm:"primaryKey"`
	Title         string `gorm:"size:160"`
	ChannelRef    string `gorm:"size:255;uniqueIndex"`
	Enabled       bool   `gorm:"default:true"`
	BackfillLimit int    `gorm:"default:0"`
	CollectFrom   time.Time
	CreatedAt     time.Time
}

func (TelegramChannel) TableName() string { return "telegram_channels" }

type TelegramMessage struct {
	ID                 uint                       `gorm:"primaryKey"`
	ChannelID          uint                       `gorm:"index;uniqueIndex:uq_tg_channel_message"`
	Channel            *TelegramChannel           `gorm:"foreignKey:ChannelID;constraint:OnDelete:CASCADE"`
	MessageID          int64                      `gorm:"uniqueIndex:uq_tg_channel_message"`
	MessageTime        time.Time                  `gorm:"index"`
	Text               string                     `gorm:"type:text"`
	Raw                datatypes.JSON             `gorm:"type:json;default:'{}'"`
	FilterDecision     *domainkernel.NewsDecision `gorm:"size:32"`
	FilterReason       *string                    `gorm:"type:text"`
	RelatedSymbols     datatypes.JSON             `gorm:"type:json;default:'[]'"`
	FilteredAt         *time.Time
	FilterModelRoleKey *string `gorm:"size:64"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (TelegramMessage) TableName() string { return "telegram_messages" }
