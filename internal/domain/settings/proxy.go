package settings

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

const (
	SecretNameProxyURL = "outbound_proxy_url"

	SettingEnabledAI       = "OUTBOUND_PROXY_ENABLED_AI"
	SettingEnabledTelegram = "OUTBOUND_PROXY_ENABLED_TELEGRAM"
	SettingEnabledMarket   = "OUTBOUND_PROXY_ENABLED_MARKET"
	SettingEnabledWeb      = "OUTBOUND_PROXY_ENABLED_WEB"
	SettingNoProxy         = "OUTBOUND_PROXY_NO_PROXY"
	SettingRevision        = "OUTBOUND_PROXY_REVISION"
)

var DefaultNoProxy = []string{"localhost", "127.0.0.1"}

type ProxyConfig struct {
	ProxyURL        string
	EnabledAI       bool
	EnabledTelegram bool
	EnabledMarket   bool
	EnabledWeb      bool
	NoProxy         []string
	Revision        string
	UpdatedAt       time.Time
}

type ProxyTestResult struct {
	Status     string
	Detail     string
	DurationMS int64
}

func (c ProxyConfig) AnyModuleEnabled() bool {
	return c.EnabledAI || c.EnabledTelegram || c.EnabledMarket || c.EnabledWeb
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

func NormalizeNoProxy(values []string) []string {
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

func ShouldBypass(host string, rules []string) bool {
	host = strings.ToLower(strings.Trim(host, "[] "))
	if host == "" {
		return false
	}
	allRules := NormalizeNoProxy(rules)
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
