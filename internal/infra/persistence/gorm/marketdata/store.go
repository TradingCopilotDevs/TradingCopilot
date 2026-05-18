package marketdata

import (
	"context"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	inframarketdata "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/marketdata"
	gormruntimeproxy "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	runtimeproxy "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/proxy/runtime"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"gorm.io/gorm"
)

type Store struct {
	db       *gorm.DB
	settings config.Settings
	security security.Service
}

func NewStore(db *gorm.DB, settings config.Settings, sec security.Service) Store {
	return Store{db: db, settings: settings, security: sec}
}

func (s Store) RuntimeConfig(ctx context.Context) (inframarketdata.RuntimeConfig, error) {
	return RuntimeConfigFromDB(s.db.WithContext(ctx), s.settings), nil
}

func (s Store) MarketSecret(ctx context.Context, name string) (string, error) {
	return marketSecret(s.db.WithContext(ctx), s.settings, name)
}

func (s Store) ProxyConfig(ctx context.Context) (runtimeproxy.Config, error) {
	return gormruntimeproxy.LoadWithSecurity(s.db.WithContext(ctx), s.security), nil
}
