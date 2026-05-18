package database

import (
	gormmigrate "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/migrate"
	"gorm.io/gorm"
)

func TimescaleSetupSQL() []string {
	return gormmigrate.TimescaleSetupSQL()
}

func SetupTimescaleDB(db *gorm.DB) error {
	return gormmigrate.SetupTimescaleDB(db)
}
