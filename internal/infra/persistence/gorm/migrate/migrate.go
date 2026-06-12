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
	if err := migrateMessageSubscriptionAssignments(db); err != nil {
		return err
	}
	if err := clearEmptyMessageSubscriptionCollectFrom(db); err != nil {
		return err
	}
	if err := normalizeIngestedMessageFilterStatus(db); err != nil {
		return err
	}
	if err := migrateIngestedMessageFilterResults(db); err != nil {
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

func migrateMessageSubscriptionAssignments(db *gorm.DB) error {
	if !db.Migrator().HasTable("message_subscription_assignments") {
		return nil
	}
	if db.Migrator().HasTable("message_subscription_research_teams") {
		if err := db.Exec(`
			INSERT INTO message_subscription_assignments
				(subscription_id, filter_id, research_team_id, enabled, created_at, updated_at)
			SELECT
				message_subscription_research_teams.message_subscription_id,
				message_subscriptions.filter_id,
				message_subscription_research_teams.research_team_id,
				true,
				COALESCE(message_subscription_research_teams.created_at, CURRENT_TIMESTAMP),
				CURRENT_TIMESTAMP
			FROM message_subscription_research_teams
			JOIN message_subscriptions ON message_subscriptions.id = message_subscription_research_teams.message_subscription_id
			WHERE message_subscriptions.filter_id IS NOT NULL
				AND message_subscriptions.filter_id <> 0
				AND NOT EXISTS (
					SELECT 1
					FROM message_subscription_assignments existing
					WHERE existing.subscription_id = message_subscription_research_teams.message_subscription_id
						AND existing.filter_id = message_subscriptions.filter_id
						AND existing.research_team_id = message_subscription_research_teams.research_team_id
				)
		`).Error; err != nil {
			return err
		}
		if err := db.Migrator().DropTable("message_subscription_research_teams"); err != nil {
			return err
		}
	}
	return db.Exec(`
		INSERT INTO message_subscription_assignments
			(subscription_id, filter_id, research_team_id, enabled, created_at, updated_at)
		SELECT message_subscriptions.id, message_subscriptions.filter_id, research_teams.id, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM message_subscriptions
		JOIN research_teams ON research_teams.id = (
			SELECT id FROM research_teams WHERE active = true ORDER BY id LIMIT 1
		)
		WHERE message_subscriptions.enabled = true
			AND message_subscriptions.filter_id IS NOT NULL
			AND message_subscriptions.filter_id <> 0
			AND NOT EXISTS (
				SELECT 1
				FROM message_subscription_assignments existing
				WHERE existing.subscription_id = message_subscriptions.id
			)
	`).Error
}

func migrateIngestedMessageFilterResults(db *gorm.DB) error {
	if !db.Migrator().HasTable("ingested_message_filter_results") {
		return nil
	}
	return db.Exec(`
		INSERT INTO ingested_message_filter_results
			(message_id, subscription_id, assignment_id, filter_id, research_team_id, filter_decision, filter_reason, filter_status, related_symbols, filtered_at, created_at, updated_at)
		SELECT
			ingested_messages.id,
			ingested_messages.subscription_id,
			message_subscription_assignments.id,
			message_subscription_assignments.filter_id,
			message_subscription_assignments.research_team_id,
			ingested_messages.filter_decision,
			ingested_messages.filter_reason,
			COALESCE(NULLIF(ingested_messages.filter_status, ''), 'unfiltered'),
			COALESCE(ingested_messages.related_symbols, '[]'),
			ingested_messages.filtered_at,
			COALESCE(ingested_messages.created_at, CURRENT_TIMESTAMP),
			COALESCE(ingested_messages.updated_at, CURRENT_TIMESTAMP)
		FROM ingested_messages
		JOIN message_subscription_assignments ON message_subscription_assignments.subscription_id = ingested_messages.subscription_id
		WHERE message_subscription_assignments.enabled = true
			AND NOT EXISTS (
			SELECT 1
			FROM ingested_message_filter_results existing
			WHERE existing.message_id = ingested_messages.id
				AND existing.filter_id = message_subscription_assignments.filter_id
				AND existing.research_team_id = message_subscription_assignments.research_team_id
		)
	`).Error
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
