package migrate

import (
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"gorm.io/gorm"
)

func AutoMigrate(db *gorm.DB) error {
	db.Config.NamingStrategy = model.NewNamingStrategy(db.Config.NamingStrategy)
	if err := db.AutoMigrate(model.Models()...); err != nil {
		return err
	}
	if err := clearEmptyMessageSubscriptionCollectFrom(db); err != nil {
		return err
	}
	if err := normalizeIngestedMessageFilterStatus(db); err != nil {
		return err
	}
	if err := normalizeResearchTeamAssetClass(db); err != nil {
		return err
	}
	if err := cancelInvalidActiveWakePlans(db); err != nil {
		return err
	}
	return SetupTimescaleDB(db)
}

func normalizeIngestedMessageFilterStatus(db *gorm.DB) error {
	if !db.Migrator().HasColumn(&model.IngestedMessage{}, "FilterStatus") {
		return nil
	}
	if err := db.Exec(`
		UPDATE ingested_messages
		SET filter_status = 'filtered'
		WHERE (filter_status IS NULL OR filter_status = '')
			AND filtered_at IS NOT NULL
			AND filter_decision IS NOT NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE ingested_messages
		SET filter_status = 'failed'
		WHERE (filter_status IS NULL OR filter_status = '')
			AND filtered_at IS NOT NULL
			AND filter_decision IS NULL
	`).Error; err != nil {
		return err
	}
	return db.Exec(`
		UPDATE ingested_messages
		SET filter_status = 'unfiltered'
		WHERE filter_status IS NULL OR filter_status = ''
	`).Error
}

func clearEmptyMessageSubscriptionCollectFrom(db *gorm.DB) error {
	zero := time.Time{}
	return db.Exec(`
		UPDATE message_subscriptions
		SET collect_from = ?
		WHERE collect_from <> ?
			AND NOT EXISTS (
				SELECT 1
				FROM ingested_messages
				WHERE ingested_messages.subscription_id = message_subscriptions.id
			)
		`, zero, zero).Error
}

func normalizeResearchTeamAssetClass(db *gorm.DB) error {
	if !db.Migrator().HasColumn(&model.ResearchTeam{}, "AssetClass") {
		return nil
	}
	return db.Exec(`
		UPDATE research_teams
		SET asset_class = 'a_share'
		WHERE asset_class IS NULL OR asset_class = ''
	`).Error
}
