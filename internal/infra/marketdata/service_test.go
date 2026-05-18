package marketdata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	applogging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/logging"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	infralogging "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/logging"
	runtimeproxy "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/proxy/runtime"
	"github.com/shopspring/decimal"
)

func TestRefreshQuotesSkipsEmptyNormalizedCodeList(t *testing.T) {
	settings := testLogSettings(t.TempDir())
	shutdown, err := infralogging.Setup(settings, "marketdata-test")
	if err != nil {
		t.Fatal(err)
	}

	store := &failingRuntimeStore{}
	quotes, err := NewService(config.Settings{}, store).RefreshQuotes(context.Background(), []string{"", "not-a-code"})
	if err != nil {
		t.Fatal(err)
	}
	if len(quotes) != 0 {
		t.Fatalf("expected empty quote map, got %+v", quotes)
	}
	if store.runtimeConfigCalls != 0 || store.proxyConfigCalls != 0 {
		t.Fatalf("empty quote refresh should not read runtime config or proxy config, got runtime=%d proxy=%d", store.runtimeConfigCalls, store.proxyConfigCalls)
	}
	if err := shutdown(); err != nil {
		t.Fatal(err)
	}

	result, err := infralogging.NewReader(settings).Query(context.Background(), applogging.Query{
		Event: "market.quotes",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("expected one market.quotes log entry, got %+v", result.Entries)
	}
	entry := result.Entries[0]
	if entry.Level != "info" || entry.Status != infralogging.StatusSkipped {
		t.Fatalf("expected info/skipped market.quotes log, got level=%s status=%s entry=%+v", entry.Level, entry.Status, entry)
	}
	if entry.Fields["skipReason"] != "no_valid_a_share_codes" {
		t.Fatalf("expected skip reason in log fields, got %+v", entry.Fields)
	}
}

func TestRefreshQuotesUsesCompatProviderForMissingETFQuotes(t *testing.T) {
	oldAdata := FetchAdataRealtimeQuotes
	FetchAdataRealtimeQuotes = func([]string) ([]*Quote, error) {
		return []*Quote{{Code: "600519", Provider: ProviderAdata, Price: decimal.NewFromInt(1688), QuoteTime: time.Now()}}, nil
	}
	t.Cleanup(func() { FetchAdataRealtimeQuotes = oldAdata })

	oldTencentBase := TencentRealtimeBaseURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/q=s_sh510300" {
			t.Fatalf("unexpected Tencent compatibility path %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`v_s_sh510300="51~沪深300ETF~510300~3.900~0.010~0.26~100~390";`))
	}))
	defer srv.Close()
	TencentRealtimeBaseURL = srv.URL
	t.Cleanup(func() { TencentRealtimeBaseURL = oldTencentBase })

	store := &runtimeStore{
		cfg: RuntimeConfig{DefaultProvider: ProviderAdata, RealtimeProvider: ProviderAdata, RealtimeCompatProvider: ProviderTencentCompat, RealtimeCacheTTL: time.Second},
	}
	quotes, err := NewService(config.Settings{}, store).RefreshQuotes(context.Background(), []string{"600519", "510300"})
	if err != nil {
		t.Fatal(err)
	}
	if quotes["600519"] == nil || quotes["600519"].Provider != ProviderAdata {
		t.Fatalf("expected adata stock quote, got %+v", quotes["600519"])
	}
	if quotes["510300"] == nil || quotes["510300"].Provider != ProviderTencentCompat || !quotes["510300"].Price.Equal(decimal.RequireFromString("3.900")) {
		t.Fatalf("expected Tencent ETF quote, got %+v", quotes["510300"])
	}
}

func TestParseSinaRealtimeQuotesUsesSimpleSymbolCode(t *testing.T) {
	quotes, err := ParseSinaRealtimeQuotes(`var hq_str_s_sh510300="CSI 300 ETF,3.900,0.010,0.26,100,390";`)
	if err != nil {
		t.Fatal(err)
	}
	if len(quotes) != 1 || quotes[0].Code != "510300" || quotes[0].Provider != ProviderSinaCompat || !quotes[0].Price.Equal(decimal.RequireFromString("3.900")) {
		t.Fatalf("unexpected Sina quote parse result: %+v", quotes)
	}
}

func testLogSettings(dir string) config.Settings {
	return config.Settings{
		LogDir:                 dir,
		LogLevel:               "info",
		LogRotationMode:        infralogging.RotationModeSize,
		LogRotationSizeMB:      5,
		LogRotationTotalSizeMB: 100,
		LogRotationMaxAgeDays:  7,
	}
}

type failingRuntimeStore struct {
	runtimeConfigCalls int
	proxyConfigCalls   int
}

func (s *failingRuntimeStore) RuntimeConfig(context.Context) (RuntimeConfig, error) {
	s.runtimeConfigCalls++
	return RuntimeConfig{}, errors.New("runtime config should not be loaded")
}

func (s *failingRuntimeStore) MarketSecret(context.Context, string) (string, error) {
	return "", errors.New("market secret should not be loaded")
}

func (s *failingRuntimeStore) ProxyConfig(context.Context) (runtimeproxy.Config, error) {
	s.proxyConfigCalls++
	return runtimeproxy.Config{}, errors.New("proxy config should not be loaded")
}

type runtimeStore struct {
	cfg RuntimeConfig
}

func (s *runtimeStore) RuntimeConfig(context.Context) (RuntimeConfig, error) {
	return s.cfg, nil
}

func (s *runtimeStore) MarketSecret(context.Context, string) (string, error) {
	return "", errors.New("market secret should not be loaded")
}

func (s *runtimeStore) ProxyConfig(context.Context) (runtimeproxy.Config, error) {
	return runtimeproxy.Config{}, nil
}
