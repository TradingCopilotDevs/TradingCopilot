package migrate

import (
	"fmt"

	"gorm.io/gorm"
)

func TimescaleSetupSQL() []string {
	return []string{
		"CREATE EXTENSION IF NOT EXISTS timescaledb",
		"ALTER TABLE daily_bars DROP CONSTRAINT IF EXISTS daily_bars_pkey",
		"ALTER TABLE realtime_quotes DROP CONSTRAINT IF EXISTS realtime_quotes_pkey",
		"SELECT create_hypertable('daily_bars', 'trade_date', if_not_exists => TRUE, migrate_data => TRUE)",
		"SELECT create_hypertable('realtime_quotes', 'quote_time', if_not_exists => TRUE, migrate_data => TRUE)",
		"CREATE INDEX IF NOT EXISTS idx_daily_bars_code_trade_date ON daily_bars(code, trade_date DESC)",
		"CREATE INDEX IF NOT EXISTS idx_realtime_quotes_code_quote_time ON realtime_quotes(code, quote_time DESC)",
	}
}

func SetupTimescaleDB(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("nil db")
	}
	if db.Dialector == nil || db.Dialector.Name() != "postgres" {
		return nil
	}
	for _, stmt := range TimescaleSetupSQL() {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("timescale setup SQL failed: %s: %w", stmt, err)
		}
	}
	return nil
}
