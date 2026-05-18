package marketdata

import (
	"net/http"
	"strings"
	"time"
)

type RuntimeConfig struct {
	DefaultProvider        string
	RealtimeProvider       string
	RealtimeCompatProvider string
	RealtimeCacheTTL       time.Duration
}

type HistoryProviderFactory struct {
	AdataProvider  HistoryProvider
	TushareToken   func() (string, error)
	TushareBaseURL string
	HTTPClient     *http.Client
}

func ResolveHistoryProvider(cfg RuntimeConfig, factory HistoryProviderFactory) (HistoryProvider, error) {
	name := NormalizeProviderName(cfg.DefaultProvider, ProviderAdata)
	switch name {
	case ProviderAdata:
		return factory.AdataProvider, nil
	case ProviderTushare:
		token := ""
		var err error
		if factory.TushareToken != nil {
			token, err = factory.TushareToken()
			if err != nil {
				return nil, err
			}
		}
		return &HTTPHistoryProvider{ProviderName: ProviderTushare, Token: token, BaseURL: factory.TushareBaseURL, HTTPClient: factory.HTTPClient}, nil
	default:
		return nil, UnsupportedProviderError(name)
	}
}

func NormalizeProviderName(value string, fallback string) string {
	name := strings.ToLower(strings.TrimSpace(value))
	if name == "" {
		return fallback
	}
	return name
}
