package runtimeproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

const (
	SecretNameProxyURL = "outbound_proxy_url"

	SettingEnabledAI       = "OUTBOUND_PROXY_ENABLED_AI"
	SettingEnabledTelegram = "OUTBOUND_PROXY_ENABLED_TELEGRAM"
	SettingEnabledMarket   = "OUTBOUND_PROXY_ENABLED_MARKET"
	SettingEnabledWeb      = "OUTBOUND_PROXY_ENABLED_WEB"
	SettingNoProxy         = "OUTBOUND_PROXY_NO_PROXY"
	SettingRevision        = "OUTBOUND_PROXY_REVISION"

	ModuleAI       = "ai"
	ModuleTelegram = "telegram"
	ModuleMarket   = "market"
	ModuleWeb      = "web"
)

var DefaultNoProxy = []string{"localhost", "127.0.0.1"}

type Config struct {
	ProxyURL        string    `json:"proxy_url"`
	EnabledAI       bool      `json:"enabled_ai"`
	EnabledTelegram bool      `json:"enabled_telegram"`
	EnabledMarket   bool      `json:"enabled_market"`
	EnabledWeb      bool      `json:"enabled_web"`
	NoProxy         []string  `json:"no_proxy"`
	Revision        string    `json:"revision"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func HTTPClientForConfig(cfg Config, module string, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if cfg.ProxyURL == "" || !cfg.Enabled(module) {
		return &http.Client{Timeout: timeout, Transport: transport}
	}
	parsed, err := url.Parse(cfg.ProxyURL)
	if err != nil {
		return &http.Client{Timeout: timeout, Transport: transport}
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		transport.Proxy = func(req *http.Request) (*url.URL, error) {
			if ShouldBypass(req.URL.Hostname(), cfg.NoProxy) {
				return nil, nil
			}
			return parsed, nil
		}
	case "socks5", "socks5h":
		dialer, ok := socks5Dialer(parsed)
		if ok {
			direct := &net.Dialer{}
			transport.DialContext = func(ctx context.Context, network string, addr string) (net.Conn, error) {
				host, _, _ := net.SplitHostPort(addr)
				if ShouldBypass(host, cfg.NoProxy) {
					return direct.DialContext(ctx, network, addr)
				}
				return dialer.DialContext(ctx, network, addr)
			}
		}
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

func (c Config) Enabled(module string) bool {
	switch module {
	case ModuleAI:
		return c.EnabledAI
	case ModuleTelegram:
		return c.EnabledTelegram
	case ModuleMarket:
		return c.EnabledMarket
	case ModuleWeb:
		return c.EnabledWeb
	default:
		return false
	}
}

func ValidateProxyURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return fmt.Errorf("unsupported proxy scheme: %s", parsed.Scheme)
	}
	if parsed.Hostname() == "" {
		return errors.New("proxy hostname is required")
	}
	return nil
}

func ShouldBypass(host string, rules []string) bool {
	host = strings.ToLower(strings.Trim(host, "[] "))
	if host == "" {
		return false
	}
	allRules := normalizeNoProxy(rules)
	ip := net.ParseIP(host)
	for _, rule := range allRules {
		rule = strings.ToLower(strings.TrimSpace(rule))
		if rule == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(rule); err == nil && ip != nil {
			if network.Contains(ip) {
				return true
			}
			continue
		}
		if ruleIP := net.ParseIP(rule); ruleIP != nil && ip != nil && ruleIP.Equal(ip) {
			return true
		}
		if rule == host || strings.HasPrefix(rule, ".") && strings.HasSuffix(host, rule) || strings.HasSuffix(host, "."+rule) {
			return true
		}
	}
	return false
}

func NormalizeNoProxy(values []string) []string {
	return normalizeNoProxy(values)
}

func normalizeNoProxy(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range append(DefaultNoProxy, values...) {
		for _, part := range strings.Split(value, ",") {
			item := strings.ToLower(strings.TrimSpace(part))
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			out = append(out, item)
		}
	}
	return out
}

type contextDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

func socks5Dialer(parsed *url.URL) (contextDialer, bool) {
	var auth *proxy.Auth
	if parsed.User != nil {
		auth = &proxy.Auth{User: parsed.User.Username()}
		auth.Password, _ = parsed.User.Password()
	}
	port := parsed.Port()
	if port == "" {
		port = strconv.Itoa(1080)
	}
	dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort(parsed.Hostname(), port), auth, proxy.Direct)
	if err != nil {
		return nil, false
	}
	ctxDialer, ok := dialer.(contextDialer)
	return ctxDialer, ok
}
